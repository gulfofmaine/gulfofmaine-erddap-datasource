package plugin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/gulfofmaine/erddap/pkg/models"
)

func TestQueryData(t *testing.T) {
	var gotPath, gotRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"table": {
				"columnNames": ["time", "temperature"],
				"columnTypes": ["String", "double"],
				"columnUnits": ["UTC", "degree_C"],
				"rows": [
					["2024-01-01T00:00:00Z", 8.2],
					["2024-01-01T01:00:00Z", 8.5]
				]
			}
		}`))
	}))
	defer srv.Close()

	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}

	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{},
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "temperature", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr, ok := resp.Responses["A"]
	if !ok {
		t.Fatal("expected a response for RefID A")
	}
	if dr.Error != nil {
		t.Fatalf("unexpected DataResponse error: %v", dr.Error)
	}
	if len(dr.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(dr.Frames))
	}

	frame := dr.Frames[0]
	if frame.Name != "foo" {
		t.Errorf("expected frame name %q, got %q", "foo", frame.Name)
	}
	if len(frame.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(frame.Fields))
	}
	if frame.Fields[0].Len() != 2 {
		t.Fatalf("expected 2 rows, got %d", frame.Fields[0].Len())
	}

	if gotPath != "/tabledap/foo.json" {
		t.Errorf("expected request path %q, got %q", "/tabledap/foo.json", gotPath)
	}
	wantRawQuery := "time,temperature&time%3E=0001-01-01T00:00:00Z&time%3C=0001-01-01T00:00:00Z"
	if gotRawQuery != wantRawQuery {
		t.Errorf("expected RawQuery %q, got %q", wantRawQuery, gotRawQuery)
	}
}

func TestQueryDataNoData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`Error {
    code=404;
    message="Not Found: Your query produced no matching results. (nRows = 0)";
}`))
	}))
	defer srv.Close()

	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}

	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "temperature, salinity", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr := resp.Responses["A"]
	if dr.Error != nil {
		t.Fatalf("expected no error for 'no matching results', got: %v", dr.Error)
	}
	if len(dr.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(dr.Frames))
	}

	frame := dr.Frames[0]
	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields (time, temperature, salinity), got %d", len(frame.Fields))
	}
	for _, f := range frame.Fields {
		if f.Len() != 0 {
			t.Errorf("expected zero-length field %q, got len %d", f.Name, f.Len())
		}
	}
}

func TestQueryDataServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`Error {
    code=500;
    message="Internal server error: something went wrong.";
}`))
	}))
	defer srv.Close()

	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}

	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "temperature", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr := resp.Responses["A"]
	if dr.Error == nil {
		t.Fatal("expected DataResponse error, got nil")
	}
	if dr.ErrorSource != backend.ErrorSourceDownstream {
		t.Errorf("expected downstream ErrorSource, got %q", dr.ErrorSource)
	}
}

func TestQueryDataEmptyErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		// Deliberately no body written.
	}))
	defer srv.Close()

	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}

	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "temperature", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr := resp.Responses["A"]
	if dr.Error == nil {
		t.Fatal("expected DataResponse error, got nil")
	}
	if dr.Error.Error() != "ERDDAP returned HTTP 502" {
		t.Errorf("expected fallback message %q, got %q", "ERDDAP returned HTTP 502", dr.Error.Error())
	}
}

func TestQueryDataMalformedOKBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}

	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "temperature", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr := resp.Responses["A"]
	if dr.Error == nil {
		t.Fatal("expected DataResponse error, got nil")
	}
	if dr.ErrorSource != backend.ErrorSourceDownstream {
		t.Errorf("expected downstream ErrorSource for a malformed 200 body, got %q", dr.ErrorSource)
	}
}

func TestCheckHealth(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/version" {
				t.Errorf("expected request to /version, got %q", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ERDDAP_version=2.23\n"))
		}))
		defer srv.Close()

		ds := Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
		}

		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != backend.HealthStatusOk {
			t.Errorf("expected HealthStatusOk, got %v (message=%q)", res.Status, res.Message)
		}
		if res.Message != "Connected to ERDDAP_version=2.23" {
			t.Errorf("unexpected message: %q", res.Message)
		}
	})

	t.Run("missing base URL", func(t *testing.T) {
		ds := Datasource{
			settings:   &models.PluginSettings{BaseURL: ""},
			httpClient: http.DefaultClient,
		}

		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != backend.HealthStatusError {
			t.Errorf("expected HealthStatusError, got %v", res.Status)
		}
		if res.Message != "ERDDAP base URL is missing" {
			t.Errorf("expected exact message %q, got %q", "ERDDAP base URL is missing", res.Message)
		}
	})

	t.Run("non-ERDDAP server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>not erddap</html>"))
		}))
		defer srv.Close()

		ds := Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
		}

		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != backend.HealthStatusError {
			t.Errorf("expected HealthStatusError, got %v", res.Status)
		}
		if !strings.Contains(res.Message, "200") {
			t.Errorf("expected message to include the HTTP status code 200, got %q", res.Message)
		}
	})

	t.Run("non-200 status includes status code in message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service unavailable"))
		}))
		defer srv.Close()

		ds := Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
		}

		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != backend.HealthStatusError {
			t.Errorf("expected HealthStatusError, got %v", res.Status)
		}
		if !strings.Contains(res.Message, "503") {
			t.Errorf("expected message to include the HTTP status code 503, got %q", res.Message)
		}
	})

	t.Run("oversized body is bounded, not read in full", func(t *testing.T) {
		huge := bytes.Repeat([]byte("x"), 5*1024*1024) // 5 MiB, well over any sane cap
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(huge)
		}))
		defer srv.Close()

		ds := Datasource{
			settings:   &models.PluginSettings{BaseURL: srv.URL},
			httpClient: srv.Client(),
		}

		res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != backend.HealthStatusError {
			t.Errorf("expected HealthStatusError, got %v", res.Status)
		}
	})
}
