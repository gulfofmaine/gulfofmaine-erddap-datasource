package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/gulfofmaine/erddap/pkg/models"
)

// newTestDatasource builds a Datasource wired to baseURL with a live resource
// handler, the way NewDatasource would.
func newTestDatasource(baseURL string, client *http.Client) *Datasource {
	d := &Datasource{
		settings:   &models.PluginSettings{BaseURL: baseURL},
		httpClient: client,
		info:       newInfoCache(),
	}
	d.resourceHandler = d.newResourceHandler()
	return d
}

// callResource drives d.CallResource the way Grafana does — a method, a path,
// and the full URL the query string is read from — and returns the first
// backend.CallResourceResponse the handler sends.
func callResource(t *testing.T, d *Datasource, method, target string) *backend.CallResourceResponse {
	t.Helper()

	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("bad target %q: %v", target, err)
	}

	var got *backend.CallResourceResponse
	sender := backend.CallResourceResponseSenderFunc(func(resp *backend.CallResourceResponse) error {
		if got == nil {
			got = resp
		}
		return nil
	})

	req := &backend.CallResourceRequest{
		Method: method,
		Path:   strings.TrimPrefix(u.Path, "/"),
		URL:    target,
	}

	if err := d.CallResource(context.Background(), req, sender); err != nil {
		t.Fatalf("CallResource returned an error: %v", err)
	}
	if got == nil {
		t.Fatal("handler sent no response")
	}

	return got
}

// decodeResourceBody unmarshals a resource response body into v.
func decodeResourceBody(t *testing.T, resp *backend.CallResourceResponse, v any) {
	t.Helper()

	if err := json.Unmarshal(resp.Body, v); err != nil {
		t.Fatalf("decoding response body %q: %v", resp.Body, err)
	}
}

// infoTestServer serves datasetInfoBody at /info/<known>/index.json and
// ERDDAP's unknown-dataset 404 for anything else.
func infoTestServer(t *testing.T, known string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info/"+known+"/index.json" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Currently unknown datasetID=nope";
}`))
			return
		}
		_, _ = w.Write([]byte(datasetInfoBody))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestResourceVariables(t *testing.T) {
	t.Run("lists a dataset's variables", func(t *testing.T) {
		srv := infoTestServer(t, "foo")
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/variables?datasetId=foo")
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}

		var body struct {
			Variables []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Units    string `json:"units"`
				LongName string `json:"longName"`
			} `json:"variables"`
		}
		decodeResourceBody(t, resp, &body)

		if len(body.Variables) != 3 {
			t.Fatalf("expected 3 variables, got %+v", body.Variables)
		}
		// time is returned too: filtering it is a query-editor policy call.
		if body.Variables[0].Name != "time" {
			t.Errorf("Variables[0].Name = %q, want time", body.Variables[0].Name)
		}
		v := body.Variables[1]
		if v.Name != "mean_temp" || v.Type != "float" || v.Units != "celsius" || v.LongName != "Mean Temperature" {
			t.Errorf("Variables[1] = %+v, want the mean_temp metadata", v)
		}
	})

	t.Run("missing datasetId is a 400", func(t *testing.T) {
		srv := infoTestServer(t, "foo")
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/variables")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.Status)
		}

		var body map[string]string
		decodeResourceBody(t, resp, &body)
		if body["error"] != "datasetId is required" {
			t.Errorf("error = %q, want %q", body["error"], "datasetId is required")
		}
	})

	t.Run("unknown dataset propagates the upstream 404", func(t *testing.T) {
		srv := infoTestServer(t, "foo")
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/variables?datasetId=nope")
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.Status)
		}

		var body map[string]string
		decodeResourceBody(t, resp, &body)
		if !strings.Contains(body["error"], "unknown datasetID=nope") {
			t.Errorf("error = %q, want the upstream ERDDAP message", body["error"])
		}
	})

	t.Run("upstream failure is a 502", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/variables?datasetId=foo")
		if resp.Status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.Status)
		}
	})
}

func TestResourceDatasets(t *testing.T) {
	// searchTestServer answers /search/advanced.json with a fixed result and
	// records the query string it was called with, so the tests can prove the
	// resource call's parameters survive httpadapter end to end.
	searchTestServer := func(t *testing.T, gotQuery *url.Values) *httptest.Server {
		t.Helper()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*gotQuery = r.URL.Query()
			_, _ = w.Write([]byte(searchBody(
				`["Title", "Summary", "Institution", "tabledap", "Dataset ID"]`,
				`["Buoy A", "A summary", "NERACOOS", "https://e.org/tabledap/A", "A01"],`+
					`["Grid Only", "", "NOAA", "", "G01"]`,
			)))
		}))
		t.Cleanup(srv.Close)

		return srv
	}

	type datasetsResponse struct {
		Datasets []struct {
			ID                string `json:"id"`
			Title             string `json:"title"`
			Institution       string `json:"institution"`
			Summary           string `json:"summary"`
			TabledapSupported bool   `json:"tabledapSupported"`
		} `json:"datasets"`
		Truncated bool `json:"truncated"`
	}

	t.Run("forwards the search text and returns results", func(t *testing.T) {
		var gotQuery url.Values
		srv := searchTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/datasets?q=buoy+temperature")
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}

		if got := gotQuery.Get("searchFor"); got != "buoy temperature" {
			t.Errorf("upstream searchFor = %q, want %q", got, "buoy temperature")
		}
		if gotQuery.Get("page") != "1" || gotQuery.Get("itemsPerPage") == "" {
			t.Errorf("upstream query missing page/itemsPerPage: %v", gotQuery)
		}

		var body datasetsResponse
		decodeResourceBody(t, resp, &body)

		if len(body.Datasets) != 2 {
			t.Fatalf("expected 2 datasets, got %+v", body.Datasets)
		}
		if body.Datasets[0].ID != "A01" || !body.Datasets[0].TabledapSupported {
			t.Errorf("Datasets[0] = %+v, want tabledap-capable A01", body.Datasets[0])
		}
		if body.Datasets[1].ID != "G01" || body.Datasets[1].TabledapSupported {
			t.Errorf("Datasets[1] = %+v, want griddap-only G01", body.Datasets[1])
		}
		if body.Truncated {
			t.Error("expected truncated = false for a short result set")
		}
	})

	t.Run("limit is clamped", func(t *testing.T) {
		var gotQuery url.Values
		srv := searchTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/datasets?limit=99999")
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}
		if got := gotQuery.Get("itemsPerPage"); got != strconv.Itoa(maxSearchLimit) {
			t.Errorf("itemsPerPage = %q, want %d", got, maxSearchLimit)
		}
	})

	t.Run("truncated is set when the page is full", func(t *testing.T) {
		var gotQuery url.Values
		srv := searchTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/datasets?limit=2")
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}

		var body datasetsResponse
		decodeResourceBody(t, resp, &body)
		if !body.Truncated {
			t.Error("expected truncated = true when the result count equals the limit")
		}
	})

	t.Run("upstream failure is a 502", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/datasets")
		if resp.Status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.Status)
		}
	})
}

func TestResourceDistinct(t *testing.T) {
	// distinctTestServer answers any tabledap request with a fixed
	// single-column distinct() result and records the query string it was
	// called with, so the tests can prove the resource call's parameters
	// survive httpadapter end to end.
	distinctTestServer := func(t *testing.T, gotQuery *string) *httptest.Server {
		t.Helper()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(distinctBody("station", "String", `["A01"],["B01"]`)))
		}))
		t.Cleanup(srv.Close)

		return srv
	}

	type distinctResponse struct {
		Values []string `json:"values"`
	}

	t.Run("lists a variable's distinct values", func(t *testing.T) {
		var gotQuery string
		srv := distinctTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/distinct?datasetId=A01&variable=station")
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}

		var body distinctResponse
		decodeResourceBody(t, resp, &body)

		if len(body.Values) != 2 || body.Values[0] != "A01" || body.Values[1] != "B01" {
			t.Errorf("values = %+v, want [A01 B01]", body.Values)
		}
		if !strings.Contains(gotQuery, "station") || !strings.Contains(gotQuery, "distinct()") {
			t.Errorf("upstream query = %q, want the variable and distinct()", gotQuery)
		}
	})

	t.Run("forwards the constraints parameter", func(t *testing.T) {
		var gotQuery string
		srv := distinctTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet,
			"/distinct?datasetId=A01&variable=station&constraints="+url.QueryEscape("time>=2024-01-01"))
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d (%s), want 200", resp.Status, resp.Body)
		}

		if !strings.Contains(gotQuery, "time%3E=2024-01-01") {
			t.Errorf("upstream query = %q, want the escaped constraint", gotQuery)
		}
	})

	t.Run("missing datasetId is a 400", func(t *testing.T) {
		var gotQuery string
		srv := distinctTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/distinct?variable=station")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.Status)
		}

		var body map[string]string
		decodeResourceBody(t, resp, &body)
		if body["error"] != "datasetId is required" {
			t.Errorf("error = %q, want %q", body["error"], "datasetId is required")
		}
	})

	t.Run("missing variable is a 400", func(t *testing.T) {
		var gotQuery string
		srv := distinctTestServer(t, &gotQuery)
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/distinct?datasetId=A01")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.Status)
		}

		var body map[string]string
		decodeResourceBody(t, resp, &body)
		if body["error"] != "variable is required" {
			t.Errorf("error = %q, want %q", body["error"], "variable is required")
		}
	})

	t.Run("unknown dataset propagates the upstream 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Currently unknown datasetID=nope";
}`))
		}))
		defer srv.Close()

		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/distinct?datasetId=nope&variable=station")
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.Status)
		}

		var body map[string]string
		decodeResourceBody(t, resp, &body)
		if !strings.Contains(body["error"], "unknown datasetID=nope") {
			t.Errorf("error = %q, want the upstream ERDDAP message", body["error"])
		}
	})

	t.Run("upstream failure is a 502", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/distinct?datasetId=A01&variable=station")
		if resp.Status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.Status)
		}
	})
}

func TestResourceRouting(t *testing.T) {
	t.Run("wrong method is a 405", func(t *testing.T) {
		srv := infoTestServer(t, "foo")
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodPost, "/variables?datasetId=foo")
		if resp.Status != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", resp.Status)
		}
	})

	t.Run("unknown route is a 404", func(t *testing.T) {
		srv := infoTestServer(t, "foo")
		d := newTestDatasource(srv.URL, srv.Client())

		resp := callResource(t, d, http.MethodGet, "/nope")
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.Status)
		}
	})

	t.Run("missing base URL is a 400 on every route", func(t *testing.T) {
		for _, target := range []string{
			"/variables?datasetId=foo",
			"/datasets",
			"/distinct?datasetId=foo&variable=station",
		} {
			t.Run(target, func(t *testing.T) {
				d := newTestDatasource("", http.DefaultClient)

				resp := callResource(t, d, http.MethodGet, target)
				if resp.Status != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", resp.Status)
				}

				var body map[string]string
				decodeResourceBody(t, resp, &body)
				if body["error"] != errBaseURLMissing {
					t.Errorf("error = %q, want %q", body["error"], errBaseURLMissing)
				}
			})
		}
	})
}
