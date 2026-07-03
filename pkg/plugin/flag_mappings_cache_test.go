package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/gulfofmaine/erddap/pkg/models"
)

func TestFlagMappingsCache(t *testing.T) {
	t.Run("miss for unknown dataset", func(t *testing.T) {
		c := newFlagMappingsCache()
		if _, ok := c.get("unknown"); ok {
			t.Fatal("expected a cache miss")
		}
	})

	t.Run("hit after set", func(t *testing.T) {
		c := newFlagMappingsCache()
		want := map[string]data.ValueMappings{"flag": {data.ValueMapper{"1": {Text: "GOOD"}}}}
		c.set("foo", want)

		got, ok := c.get("foo")
		if !ok {
			t.Fatal("expected a cache hit")
		}
		if got["flag"][0].(data.ValueMapper)["1"].Text != "GOOD" {
			t.Errorf("unexpected cached mappings: %+v", got)
		}
	})

	t.Run("expired entry is a miss", func(t *testing.T) {
		c := newFlagMappingsCache()
		c.entries["foo"] = flagMappingsCacheEntry{
			mappings:  map[string]data.ValueMappings{},
			expiresAt: time.Now().Add(-time.Second),
		}

		if _, ok := c.get("foo"); ok {
			t.Fatal("expected an expired entry to be a cache miss")
		}
	})
}

func TestFlagMappingsFor(t *testing.T) {
	t.Run("success caches the result", func(t *testing.T) {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.URL.Path != "/info/foo/index.json" {
				t.Errorf("expected request to /info/foo/index.json, got %q", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"table": {
					"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
					"rows": [
						["attribute", "flag", "flag_meanings", "String", "GOOD FAIL"],
						["attribute", "flag", "flag_values", "long", "1, 2"]
					]
				}
			}`))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings["flag"] == nil {
			t.Fatalf("expected a mapping for 'flag', got %+v", mappings)
		}

		// Second call should be served from cache, not a second request.
		ds.flagMappingsFor(context.Background(), "foo")
		if requests != 1 {
			t.Errorf("expected 1 request (second call served from cache), got %d", requests)
		}
	})

	t.Run("non-200 fails soft", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a non-200 response, got %+v", mappings)
		}
	})

	t.Run("malformed body fails soft", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a malformed body, got %+v", mappings)
		}
	})
}
