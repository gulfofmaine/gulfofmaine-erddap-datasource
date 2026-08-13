package plugin

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
)

// newResourceHandler builds the plugin's resource router. Resource calls
// arrive as HTTP-shaped requests, so they are served by an http.ServeMux
// wrapped in httpadapter: method-qualified patterns give correct 404/405
// handling for free.
//
// Routes:
//
//	GET /datasets?q=&limit=   search the server's datasets
//	GET /variables?datasetId= list one dataset's variables
func (d *Datasource) newResourceHandler() backend.CallResourceHandler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /datasets", d.handleDatasets)
	mux.HandleFunc("GET /variables", d.handleVariables)

	return httpadapter.New(mux)
}

// CallResource handles resource calls sent from the frontend (the query
// editor's dataset and variable pickers).
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return d.resourceHandler.CallResource(ctx, req, sender)
}

// resourceDataset is the frontend-facing JSON shape of an erddapDataset.
type resourceDataset struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Institution       string `json:"institution"`
	Summary           string `json:"summary"`
	TabledapSupported bool   `json:"tabledapSupported"`
}

// handleDatasets serves GET /datasets?q=<text>&limit=<n>, searching the
// ERDDAP server's catalog so the query editor can offer a dataset picker.
//
// An omitted q browses everything (bounded by limit). The response's
// "truncated" flag reports that the result set filled the requested page, so
// the picker can prompt the user to narrow their search rather than implying
// they have seen every match.
func (d *Datasource) handleDatasets(w http.ResponseWriter, r *http.Request) {
	if !d.requireBaseURL(w) {
		return
	}

	limit := searchLimitFrom(r.URL.Query().Get("limit"))

	results, err := d.searchDatasets(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}

	datasets := make([]resourceDataset, 0, len(results))
	for _, ds := range results {
		datasets = append(datasets, resourceDataset(ds))
	}

	writeResourceJSON(w, http.StatusOK, map[string]any{
		"datasets":  datasets,
		"truncated": len(results) == limit,
	})
}

// resourceVariable is the frontend-facing JSON shape of an erddapVariable.
// It is a separate type so the wire contract (camelCase JSON keys) is stated
// explicitly and can diverge from the internal struct later; today the fields
// still line up, so the two are directly convertible.
type resourceVariable struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Units    string `json:"units"`
	LongName string `json:"longName"`
}

// handleVariables serves GET /variables?datasetId=<id>, listing every
// variable a dataset declares along with its type, units and long name.
//
// Coordinate variables (time, latitude, longitude) are included: whether to
// offer them in a picker is a query-editor policy decision, not something
// this endpoint should decide by omission.
func (d *Datasource) handleVariables(w http.ResponseWriter, r *http.Request) {
	if !d.requireBaseURL(w) {
		return
	}

	datasetID := r.URL.Query().Get("datasetId")
	if datasetID == "" {
		writeResourceError(w, http.StatusBadRequest, "datasetId is required")
		return
	}

	info, err := d.datasetInfoFor(r.Context(), datasetID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}

	variables := make([]resourceVariable, 0, len(info.Variables))
	for _, v := range info.Variables {
		variables = append(variables, resourceVariable(v))
	}

	writeResourceJSON(w, http.StatusOK, map[string]any{"variables": variables})
}

// requireBaseURL writes the standard 400 and reports false when the
// datasource has not been configured with an ERDDAP base URL yet.
func (d *Datasource) requireBaseURL(w http.ResponseWriter) bool {
	if d.settings == nil || d.settings.BaseURL == "" {
		writeResourceError(w, http.StatusBadRequest, errBaseURLMissing)
		return false
	}
	return true
}

// writeUpstreamError translates an error from an ERDDAP call into a resource
// response. A 404 (typically an unknown datasetID) is propagated as-is with
// the upstream message, since that is actionable by the user; everything else
// — transport failures, upstream 5xx, unparseable bodies — becomes a 502,
// because the plugin reached out and got something it could not use.
func writeUpstreamError(w http.ResponseWriter, err error) {
	if status, ok := erddapStatusOf(err); ok && status == http.StatusNotFound {
		writeResourceError(w, http.StatusNotFound, err.Error())
		return
	}
	writeResourceError(w, http.StatusBadGateway, err.Error())
}

// writeResourceJSON writes v as a JSON resource response. WriteHeader must
// precede Write: httpadapter's ResponseWriter locks the status in on the
// first write, so a later WriteHeader is silently dropped.
func writeResourceJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		backend.Logger.Warn("erddap: encoding resource response failed", "error", err)
	}
}

// writeResourceError writes the {"error": "..."} shape the frontend reads
// failures out of.
func writeResourceError(w http.ResponseWriter, status int, msg string) {
	writeResourceJSON(w, status, map[string]string{"error": msg})
}
