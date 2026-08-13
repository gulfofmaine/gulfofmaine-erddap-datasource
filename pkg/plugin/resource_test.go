package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		for _, target := range []string{"/variables?datasetId=foo"} {
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
