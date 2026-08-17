package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gulfofmaine/erddap/pkg/models"
)

func TestBuildSearchURL(t *testing.T) {
	// page and itemsPerPage are not optional: ERDDAP answers a
	// /search/advanced.json request missing either one with a 302 to the HTML
	// search form, so the plugin silently gets HTML instead of JSON.
	t.Run("always sets page and itemsPerPage", func(t *testing.T) {
		for _, searchFor := range []string{"", "temperature"} {
			raw, err := buildSearchURL("https://data.neracoos.org/erddap", searchFor, 25)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			q := u.Query()

			if q.Get("page") != "1" {
				t.Errorf("searchFor=%q: page = %q, want 1 (%s)", searchFor, q.Get("page"), raw)
			}
			if q.Get("itemsPerPage") != "25" {
				t.Errorf("searchFor=%q: itemsPerPage = %q, want 25 (%s)", searchFor, q.Get("itemsPerPage"), raw)
			}
		}
	})

	t.Run("preserves an existing base path for a search", func(t *testing.T) {
		raw, err := buildSearchURL("https://data.neracoos.org/erddap", "temperature", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Path != "/erddap/search/advanced.json" {
			t.Errorf("path = %q, want /erddap/search/advanced.json", u.Path)
		}
	})

	t.Run("preserves an existing base path for a browse", func(t *testing.T) {
		raw, err := buildSearchURL("https://data.neracoos.org/erddap", "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Path != "/erddap/info/index.json" {
			t.Errorf("path = %q, want /erddap/info/index.json", u.Path)
		}
	})

	// /search/advanced.json 400s on a request with zero real criteria, which is
	// exactly what an empty searchFor would be. /info/index.json lists every
	// dataset unconditionally and returns the identical table shape, so a
	// browse (empty searchFor) must hit that endpoint instead.
	t.Run("browse hits info/index.json, not search/advanced.json", func(t *testing.T) {
		raw, err := buildSearchURL("https://example.org/erddap", "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Path != "/erddap/info/index.json" {
			t.Errorf("path = %q, want /erddap/info/index.json", u.Path)
		}
		if strings.Contains(raw, "searchFor") {
			t.Errorf("expected no searchFor parameter, got %s", raw)
		}
	})

	t.Run("a non-empty search still hits search/advanced.json", func(t *testing.T) {
		raw, err := buildSearchURL("https://example.org/erddap", "temperature", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Path != "/erddap/search/advanced.json" {
			t.Errorf("path = %q, want /erddap/search/advanced.json", u.Path)
		}
	})

	t.Run("encodes searchFor", func(t *testing.T) {
		raw, err := buildSearchURL("https://example.org/erddap", `sea water & "salinity"`, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := u.Query().Get("searchFor"); got != `sea water & "salinity"` {
			t.Errorf("searchFor round-tripped as %q", got)
		}
		if strings.Contains(u.RawQuery, `"`) {
			t.Errorf("expected the quote to be percent-encoded, got %s", u.RawQuery)
		}
	})

	t.Run("rejects an unparseable base URL", func(t *testing.T) {
		if _, err := buildSearchURL("://nope", "", 10); err == nil {
			t.Fatal("expected an error for an unparseable base URL")
		}
	})
}

// searchBody renders a /search/advanced.json response with the given column
// order and rows, so tests can prove columns are resolved by name.
func searchBody(columns string, rows string) string {
	return `{"table": {"columnNames": ` + columns + `, "rows": [` + rows + `]}}`
}

func TestParseSearchJSON(t *testing.T) {
	t.Run("tolerates a reordered columnNames array", func(t *testing.T) {
		natural := searchBody(
			`["griddap", "tabledap", "Title", "Summary", "Institution", "Dataset ID"]`,
			`["", "https://e.org/tabledap/A", "Buoy A", "A summary", "NERACOOS", "A01"]`,
		)
		shuffled := searchBody(
			`["Dataset ID", "Institution", "Summary", "Title", "tabledap", "griddap"]`,
			`["A01", "NERACOOS", "A summary", "Buoy A", "https://e.org/tabledap/A", ""]`,
		)

		want := erddapDataset{
			ID:                "A01",
			Title:             "Buoy A",
			Institution:       "NERACOOS",
			Summary:           "A summary",
			TabledapSupported: true,
		}

		for name, body := range map[string]string{"natural": natural, "shuffled": shuffled} {
			got, err := parseSearchJSON(strings.NewReader(body))
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", name, err)
			}
			if len(got) != 1 {
				t.Fatalf("%s: expected 1 dataset, got %+v", name, got)
			}
			if got[0] != want {
				t.Errorf("%s: got %+v, want %+v", name, got[0], want)
			}
		}
	})

	t.Run("drops allDatasets and blank IDs", func(t *testing.T) {
		body := searchBody(
			`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
			`["All Datasets", "", "", "https://e.org/tabledap/allDatasets", "allDatasets"],`+
				`["Nameless", "", "", "", ""],`+
				`["Buoy A", "", "", "https://e.org/tabledap/A", "A01"]`,
		)

		got, err := parseSearchJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "A01" {
			t.Errorf("expected only A01, got %+v", got)
		}
	})

	t.Run("griddap-only datasets are kept but flagged", func(t *testing.T) {
		body := searchBody(
			`["Title", "Summary", "Institution", "tabledap", "griddap", "Dataset ID"]`,
			`["Grid Only", "", "", "", "https://e.org/griddap/G", "G01"]`,
		)

		got, err := parseSearchJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected the griddap-only dataset to be returned, got %+v", got)
		}
		if got[0].TabledapSupported {
			t.Error("expected TabledapSupported = false for an empty tabledap column")
		}
	})

	t.Run("truncates a long summary", func(t *testing.T) {
		long := strings.Repeat("x", maxSummaryLen*2)
		body := searchBody(
			`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
			`["Buoy A", "`+long+`", "", "", "A01"]`,
		)

		got, err := parseSearchJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got[0].Summary) > maxSummaryLen {
			t.Errorf("summary is %d chars, want at most %d", len(got[0].Summary), maxSummaryLen)
		}
	})

	t.Run("missing Dataset ID column is an error", func(t *testing.T) {
		body := searchBody(`["Title", "Summary"]`, ``)
		if _, err := parseSearchJSON(strings.NewReader(body)); err == nil {
			t.Fatal("expected an error for a missing Dataset ID column")
		}
	})

	t.Run("ragged rows are skipped", func(t *testing.T) {
		body := searchBody(
			`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
			`["Short"],["Buoy A", "", "", "", "A01"]`,
		)

		got, err := parseSearchJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "A01" {
			t.Errorf("expected only A01, got %+v", got)
		}
	})

	t.Run("malformed body is an error", func(t *testing.T) {
		if _, err := parseSearchJSON(strings.NewReader("not json")); err == nil {
			t.Fatal("expected an error for a malformed body")
		}
	})
}

func TestSearchDatasets(t *testing.T) {
	newDS := func(t *testing.T, handler http.HandlerFunc) *Datasource {
		t.Helper()
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		return &Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
			info:       newInfoCache(),
		}
	}

	t.Run("returns parsed results", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/search/advanced.json" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(searchBody(
				`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
				`["Buoy A", "s", "NERACOOS", "https://e.org/tabledap/A", "A01"]`,
			)))
		})

		got, err := d.searchDatasets(context.Background(), "buoy", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "A01" {
			t.Errorf("got %+v", got)
		}
	})

	// A browse (empty searchFor) must hit /info/index.json instead of
	// /search/advanced.json, which 400s on a request with zero real criteria.
	// The two endpoints return the identical table shape, so the exact same
	// fixture body proves parseSearchJSON needs no endpoint-specific logic.
	t.Run("an empty search browses info/index.json", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/info/index.json" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(searchBody(
				`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
				`["Buoy A", "s", "NERACOOS", "https://e.org/tabledap/A", "A01"]`,
			)))
		})

		got, err := d.searchDatasets(context.Background(), "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "A01" {
			t.Errorf("got %+v", got)
		}
	})

	// ERDDAP answers a zero-match search with a 404, not an empty table.
	t.Run("no matching results is an empty list, not an error", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Your query produced no matching results. (nRows = 0)";
}`))
		})

		got, err := d.searchDatasets(context.Background(), "zzzzz", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("expected an empty slice, got %+v", got)
		}
	})

	t.Run("other 404s are real errors", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Currently unknown datasetID=nope";
}`))
		})

		_, err := d.searchDatasets(context.Background(), "nope", 10)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "unknown datasetID") {
			t.Errorf("error = %q, want the upstream message", err.Error())
		}
	})

	t.Run("server error is an error", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		if _, err := d.searchDatasets(context.Background(), "", 10); err == nil {
			t.Fatal("expected an error")
		}
	})

	// The read is capped, so a server streaming an unbounded body truncates
	// into a parse error rather than filling memory.
	t.Run("an oversized body is bounded", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"table": {"columnNames": ["Dataset ID"], "rows": [["`))
			chunk := strings.Repeat("x", 64<<10)
			for written := 0; written < maxSearchResponseBytes+(1<<20); written += len(chunk) {
				if _, err := w.Write([]byte(chunk)); err != nil {
					return
				}
			}
		})

		if _, err := d.searchDatasets(context.Background(), "", 10); err == nil {
			t.Fatal("expected a parse error from the truncated body")
		}
	})

	t.Run("transport failure is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		client := srv.Client()
		baseURL := srv.URL
		srv.Close()

		d := &Datasource{
			settings:   &models.PluginSettings{BaseURL: baseURL},
			httpClient: client,
			info:       newInfoCache(),
		}

		if _, err := d.searchDatasets(context.Background(), "", 10); err == nil {
			t.Fatal("expected an error from a closed server")
		}
	})
}

func TestSearchLimitFrom(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", defaultSearchLimit},
		{"nope", defaultSearchLimit},
		{"0", defaultSearchLimit},
		{"-5", defaultSearchLimit},
		{"25", 25},
		{"99999", maxSearchLimit},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := searchLimitFrom(tc.raw); got != tc.want {
				t.Errorf("searchLimitFrom(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
