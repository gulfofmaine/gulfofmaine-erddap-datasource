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

func TestBuildDistinctURL(t *testing.T) {
	t.Run("builds the tabledap distinct query", func(t *testing.T) {
		raw, err := buildDistinctURL("https://data.neracoos.org/erddap", "A01_sbe37_all", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := "https://data.neracoos.org/erddap/tabledap/A01_sbe37_all.json?station&distinct()"
		if raw != want {
			t.Errorf("url = %q, want %q", raw, want)
		}
	})

	// escapeERDDAP keeps "(" and ")" literal (they are in
	// erddapStructuralChars), so the distinct() call survives escaping intact
	// rather than arriving at ERDDAP as distinct%28%29.
	t.Run("distinct() parens are not percent-encoded", func(t *testing.T) {
		raw, err := buildDistinctURL("https://example.org/erddap", "ds", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(raw, "&distinct()") {
			t.Errorf("url = %q, want it to end in &distinct()", raw)
		}
		if strings.Contains(raw, "%28") || strings.Contains(raw, "%29") {
			t.Errorf("url = %q, want literal parens", raw)
		}
	})

	t.Run("no constraints leaves no empty query segment", func(t *testing.T) {
		raw, err := buildDistinctURL("https://example.org/erddap", "ds", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(u.RawQuery, "&&") || strings.HasSuffix(u.RawQuery, "&") {
			t.Errorf("query = %q, want no empty segment", u.RawQuery)
		}
	})

	// The constraints segment goes through the same normalization a tabledap
	// query's constraints do, so an ERDDAP-pre-encoded value round-trips and a
	// hand-typed quote/ampersand is escaped.
	t.Run("constraints are normalized and appended", func(t *testing.T) {
		constraints := `station="A&B"`

		raw, err := buildDistinctURL("https://example.org/erddap", "ds", "depth", constraints)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := normalizeERDDAPConstraints(constraints)
		if !strings.Contains(raw, want) {
			t.Errorf("url = %q, want it to contain the normalized constraints %q", raw, want)
		}
		if strings.Contains(raw, `"`) {
			t.Errorf("url = %q, want the quotes percent-encoded", raw)
		}
	})

	// ERDDAP's docs say to add &distinct() to the *end* of a query, and that
	// server-side functions are applied in the order they appear, so the
	// constraints must precede it.
	t.Run("distinct() stays last when constraints are present", func(t *testing.T) {
		raw, err := buildDistinctURL("https://example.org/erddap", "ds", "station", "time>=2024-01-01")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(raw, "&distinct()") {
			t.Errorf("url = %q, want it to end in &distinct()", raw)
		}
		if !strings.Contains(raw, "time%3E=2024-01-01") {
			t.Errorf("url = %q, want the escaped time constraint", raw)
		}
	})

	t.Run("the variable is escaped", func(t *testing.T) {
		raw, err := buildDistinctURL("https://example.org/erddap", "ds", `weird name`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(raw, "weird%20name") {
			t.Errorf("url = %q, want the space percent-encoded", raw)
		}
	})

	t.Run("preserves an existing base path", func(t *testing.T) {
		raw, err := buildDistinctURL("https://data.neracoos.org/erddap", "ds", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Path != "/erddap/tabledap/ds.json" {
			t.Errorf("path = %q, want /erddap/tabledap/ds.json", u.Path)
		}
	})

	t.Run("a dataset ID with a slash cannot escape the tabledap path", func(t *testing.T) {
		raw, err := buildDistinctURL("https://example.org/erddap", "../../evil", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(raw, "/erddap/tabledap/") {
			t.Errorf("url = %q, want it to stay under /erddap/tabledap/", raw)
		}
	})

	t.Run("rejects an unparseable base URL", func(t *testing.T) {
		if _, err := buildDistinctURL("://nope", "ds", "station", ""); err == nil {
			t.Fatal("expected an error for an unparseable base URL")
		}
	})
}

// distinctBody renders a single-column tabledap distinct() response with the
// given raw JSON rows, so tests can mix string and bare-number cells.
func distinctBody(column, columnType, rows string) string {
	return `{"table": {"columnNames": ["` + column + `"], "columnTypes": ["` + columnType +
		`"], "rows": [` + rows + `]}}`
}

func TestParseDistinctJSON(t *testing.T) {
	t.Run("reads string cells", func(t *testing.T) {
		body := distinctBody("station", "String", `["A01"],["B01"],["E01"]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"A01", "B01", "E01"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	// A distinct() query on a numeric variable returns bare JSON numbers, not
	// quoted strings, so the parser must handle both cell shapes.
	t.Run("reads bare numeric cells", func(t *testing.T) {
		body := distinctBody("depth", "double", `[1],[2.5],[-3],[1e2]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"1", "2.5", "-3", "1e2"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("skips null cells", func(t *testing.T) {
		body := distinctBody("station", "String", `["A01"],[null],["B01"]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"A01", "B01"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("skips empty-string cells", func(t *testing.T) {
		body := distinctBody("station", "String", `["A01"],[""],["B01"]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"A01", "B01"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	// ERDDAP's distinct() already sorts its output; whatever order it chose is
	// the meaningful one, so the parser must not re-sort.
	t.Run("preserves ERDDAP's row order", func(t *testing.T) {
		body := distinctBody("station", "String", `["zulu"],["alpha"],["mike"]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"zulu", "alpha", "mike"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v (order must be preserved)", got, want)
		}
	})

	t.Run("empty rows is an empty slice", func(t *testing.T) {
		got, err := parseDistinctJSON(strings.NewReader(distinctBody("station", "String", ``)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected an empty slice, got nil")
		}
		if len(got) != 0 {
			t.Errorf("got %v, want an empty slice", got)
		}
	})

	t.Run("ragged rows are skipped", func(t *testing.T) {
		body := distinctBody("station", "String", `[],["A01"]`)

		got, err := parseDistinctJSON(strings.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStrings(got, []string{"A01"}) {
			t.Errorf("got %v, want [A01]", got)
		}
	})

	t.Run("malformed body is an error", func(t *testing.T) {
		if _, err := parseDistinctJSON(strings.NewReader("not json")); err == nil {
			t.Fatal("expected an error for a malformed body")
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDistinctValues(t *testing.T) {
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

	t.Run("returns the parsed values", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/tabledap/A01.json" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			if !strings.Contains(r.URL.RawQuery, "distinct()") {
				t.Errorf("query = %q, want a distinct() call", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(distinctBody("station", "String", `["A01"],["B01"]`)))
		})

		got, err := d.distinctValues(context.Background(), "A01", "station", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStrings(got, []string{"A01", "B01"}) {
			t.Errorf("got %v, want [A01 B01]", got)
		}
	})

	// The dashboard time range is deliberately not applied: a caller who wants
	// that scoping passes it through constraints.
	t.Run("adds no time constraint of its own", func(t *testing.T) {
		var gotQuery string
		d := newDS(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(distinctBody("station", "String", `["A01"]`)))
		})

		if _, err := d.distinctValues(context.Background(), "A01", "station", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(gotQuery, "time>=") || strings.Contains(gotQuery, "time<=") {
			t.Errorf("query = %q, want no time constraint", gotQuery)
		}
	})

	t.Run("forwards constraints", func(t *testing.T) {
		var gotQuery string
		d := newDS(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(distinctBody("station", "String", `["A01"]`)))
		})

		if _, err := d.distinctValues(context.Background(), "A01", "station", "time>=2024-01-01"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(gotQuery, "time%3E=2024-01-01") {
			t.Errorf("query = %q, want the escaped constraint", gotQuery)
		}
	})

	t.Run("no matching results is an empty slice, not an error", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Your query produced no matching results. (nRows = 0)";
}`))
		})

		got, err := d.distinctValues(context.Background(), "A01", "station", "")
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

		_, err := d.distinctValues(context.Background(), "nope", "station", "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "unknown datasetID") {
			t.Errorf("error = %q, want the upstream message", err.Error())
		}
		if status, ok := erddapStatusOf(err); !ok || status != http.StatusNotFound {
			t.Errorf("erddapStatusOf = (%d, %v), want (404, true)", status, ok)
		}
	})

	t.Run("server error is an error", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		if _, err := d.distinctValues(context.Background(), "A01", "station", ""); err == nil {
			t.Fatal("expected an error")
		}
	})

	// The read is capped, so a server streaming an unbounded body truncates
	// into a parse error rather than filling memory.
	t.Run("an oversized body is bounded", func(t *testing.T) {
		d := newDS(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"table": {"columnNames": ["station"], "rows": [["`))
			chunk := strings.Repeat("x", 64<<10)
			for written := 0; written < maxDistinctResponseBytes+(1<<20); written += len(chunk) {
				if _, err := w.Write([]byte(chunk)); err != nil {
					return
				}
			}
		})

		if _, err := d.distinctValues(context.Background(), "A01", "station", ""); err == nil {
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

		if _, err := d.distinctValues(context.Background(), "A01", "station", ""); err == nil {
			t.Fatal("expected an error from a closed server")
		}
	})

	t.Run("rejects an unparseable base URL", func(t *testing.T) {
		d := &Datasource{
			settings:   &models.PluginSettings{BaseURL: "://nope"},
			httpClient: http.DefaultClient,
			info:       newInfoCache(),
		}

		if _, err := d.distinctValues(context.Background(), "A01", "station", ""); err == nil {
			t.Fatal("expected an error for an unparseable base URL")
		}
	})
}
