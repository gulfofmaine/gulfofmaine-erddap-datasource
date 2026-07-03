package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/concurrent"
	"github.com/gulfofmaine/erddap/pkg/models"
)

// Make sure Datasource implements required interfaces. This is important to do
// since otherwise we will only get a not implemented error response from plugin in
// runtime.
var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// errBaseURLMissing is the exact CheckHealth error message asserted verbatim
// by the frontend e2e test. handleQuery reuses it for the same underlying
// condition (missing configuration) so the two code paths stay in sync.
const errBaseURLMissing = "ERDDAP base URL is missing"

// erddapVersionPrefix is the prefix of a healthy ERDDAP server's response
// body at GET {base}/version.
const erddapVersionPrefix = "ERDDAP_version="

// Datasource queries an ERDDAP server's tabledap endpoints via the Grafana
// plugin SDK.
type Datasource struct {
	settings   *models.PluginSettings
	httpClient *http.Client
}

// NewDatasource creates a new datasource instance.
func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	pluginSettings, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}

	opts, _ := settings.HTTPClientOptions(ctx)
	httpClient, err := httpclient.New(opts)
	if err != nil {
		return nil, err
	}

	return &Datasource{
		settings:   pluginSettings,
		httpClient: httpClient,
	}, nil
}

// Dispose cleans up datasource instance resources when a new instance is
// created (e.g. after datasource settings change).
func (d *Datasource) Dispose() {
	d.httpClient.CloseIdleConnections()
}

// QueryData handles multiple queries and returns multiple responses,
// executing up to 10 queries concurrently.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return concurrent.QueryData(ctx, req, d.handleQuery, 10)
}

// handleQuery executes a single query against ERDDAP and returns the
// resulting data.Frame wrapped in a backend.DataResponse.
func (d *Datasource) handleQuery(ctx context.Context, q concurrent.Query) backend.DataResponse {
	qm, err := models.LoadQueryModel(q.DataQuery.JSON)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	if d.settings == nil || d.settings.BaseURL == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, errBaseURLMissing)
	}

	tabledapURL, err := buildTabledapURL(d.settings.BaseURL, *qm, q.DataQuery.TimeRange)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	frame, err := d.fetch(ctx, tabledapURL, *qm)
	if err != nil {
		return backend.ErrorResponseWithErrorSource(err)
	}

	return backend.DataResponse{Frames: data.Frames{frame}}
}

// CheckHealth handles health checks sent from Grafana to the plugin. The
// main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that a
// datasource is working as expected.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.settings == nil || d.settings.BaseURL == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: errBaseURLMissing,
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.settings.BaseURL+"/version", nil)
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("failed to build request: %s", err),
		}, nil
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("failed to connect to ERDDAP server: %s", err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiagnosticBodyBytes))
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("failed to read response from ERDDAP server: %s", err),
		}, nil
	}

	trimmed := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusOK && strings.HasPrefix(trimmed, erddapVersionPrefix) {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusOk,
			Message: "Connected to " + trimmed,
		}, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusError,
		Message: fmt.Sprintf("URL does not appear to be an ERDDAP server (HTTP %d)", resp.StatusCode),
	}, nil
}
