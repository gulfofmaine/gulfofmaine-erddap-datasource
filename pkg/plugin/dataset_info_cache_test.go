package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gulfofmaine/erddap/pkg/models"
)

func TestInfoCache(t *testing.T) {
	t.Run("miss for unknown dataset", func(t *testing.T) {
		c := newInfoCache()
		if _, ok := c.get("unknown"); ok {
			t.Fatal("expected a cache miss")
		}
	})

	t.Run("hit after set", func(t *testing.T) {
		c := newInfoCache()
		want := &datasetInfo{Variables: []erddapVariable{{Name: "temperature", Type: "double"}}}
		c.set("foo", want)

		got, ok := c.get("foo")
		if !ok {
			t.Fatal("expected a cache hit")
		}
		if len(got.Variables) != 1 || got.Variables[0].Name != "temperature" {
			t.Errorf("unexpected cached info: %+v", got)
		}
	})

	t.Run("expired entry is a miss", func(t *testing.T) {
		c := newInfoCache()
		c.entries["foo"] = infoCacheEntry{
			info:      &datasetInfo{},
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
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a non-200 response, got %+v", mappings)
		}
	})

	t.Run("malformed body fails soft", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a malformed body, got %+v", mappings)
		}
	})

	// Regression guard for the datasetInfoFor/flagMappingsFor split: the
	// error-returning variant must never let a metadata failure reach a
	// query. Whatever datasetInfoFor reports, flagMappingsFor swallows it.
	t.Run("fails soft when datasetInfoFor errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`Error {
    code=500;
    message="Internal Server Error: something broke";
}`))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}

		if _, err := ds.datasetInfoFor(context.Background(), "foo"); err == nil {
			t.Fatal("expected datasetInfoFor to surface the upstream failure")
		}

		if mappings := ds.flagMappingsFor(context.Background(), "foo"); mappings != nil {
			t.Errorf("expected flagMappingsFor to fail soft, got %+v", mappings)
		}
	})
}

func TestDatasetInfoFor(t *testing.T) {
	t.Run("surfaces the upstream status and message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Currently unknown datasetID=nope";
}`))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}

		_, err := ds.datasetInfoFor(context.Background(), "nope")
		if err == nil {
			t.Fatal("expected an error for an unknown dataset")
		}

		status, ok := erddapStatusOf(err)
		if !ok {
			t.Fatalf("expected an erddapStatusError, got %T: %v", err, err)
		}
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
		if err.Error() != "Not Found: Currently unknown datasetID=nope" {
			t.Errorf("message = %q, want the upstream ERDDAP message", err.Error())
		}
	})

	t.Run("returns the parsed variables", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(datasetInfoBody))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}

		info, err := ds.datasetInfoFor(context.Background(), "foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(info.Variables) != 3 {
			t.Errorf("expected 3 variables, got %+v", info.Variables)
		}
	})
}
