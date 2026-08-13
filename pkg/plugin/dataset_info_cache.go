package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// datasetInfoCacheTTL bounds how long a dataset's info document (its variable
// list and flag_values/flag_meanings metadata) is cached before being
// re-fetched from ERDDAP. Metadata changes rarely; this just bounds staleness
// if a dataset's variables or flag vocabulary change upstream.
const datasetInfoCacheTTL = time.Hour

// maxInfoResponseBytes bounds how much of an ERDDAP dataset info response
// (.../info/<id>/index.json) datasetInfoFor will read. Info responses list
// every global and per-variable attribute, so they run larger than the tiny
// error/version bodies maxDiagnosticBodyBytes is sized for.
const maxInfoResponseBytes = 256 << 10 // 256 KiB

// infoCacheEntry is one dataset's cached info document and expiry.
type infoCacheEntry struct {
	info      *datasetInfo
	expiresAt time.Time
}

// infoCache is an in-memory, per-Datasource-instance cache of dataset ID ->
// parsed info document, guarded by a mutex since QueryData serves concurrent
// queries and resource calls run concurrently with them.
type infoCache struct {
	mu      sync.Mutex
	entries map[string]infoCacheEntry
}

func newInfoCache() *infoCache {
	return &infoCache{entries: map[string]infoCacheEntry{}}
}

// get returns the cached info for datasetID, and false if there is no
// unexpired entry.
func (c *infoCache) get(datasetID string) (*datasetInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[datasetID]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.info, true
}

// set caches info for datasetID for datasetInfoCacheTTL.
func (c *infoCache) set(datasetID string, info *datasetInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[datasetID] = infoCacheEntry{
		info:      info,
		expiresAt: time.Now().Add(datasetInfoCacheTTL),
	}
}

// datasetInfoFor returns datasetID's parsed info document, fetching ERDDAP's
// .../info/<datasetID>/index.json and caching the result on a miss.
//
// Unlike flagMappingsFor, failures are returned rather than swallowed: the
// /variables resource endpoint needs to tell the user *why* the variable
// picker is empty. Errors follow the same conventions as fetch — transport
// and parse failures are backend.DownstreamError, and a non-200 carries the
// upstream status through erddapStatusError so callers can map it back onto
// an HTTP status of their own.
func (d *Datasource) datasetInfoFor(ctx context.Context, datasetID string) (*datasetInfo, error) {
	if cached, ok := d.info.get(datasetID); ok {
		return cached, nil
	}

	infoURL := d.settings.BaseURL + "/info/" + url.PathEscape(datasetID) + "/index.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, backend.DownstreamError(err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, backend.DownstreamError(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDiagnosticBodyBytes))
		return nil, newERDDAPStatusError(resp.StatusCode, extractERDDAPMessage(body))
	}

	info, err := parseDatasetInfo(io.LimitReader(resp.Body, maxInfoResponseBytes))
	if err != nil {
		return nil, backend.DownstreamError(fmt.Errorf("erddap: parsing dataset info: %w", err))
	}

	d.info.set(datasetID, info)
	return info, nil
}

// flagMappingsFor returns the flag_values/flag_meanings-derived value
// mappings for datasetID's variables. Any failure (transport error, non-200,
// malformed body) logs a warning and returns nil: a query proceeds with no
// mappings rather than failing over a metadata hiccup.
func (d *Datasource) flagMappingsFor(ctx context.Context, datasetID string) map[string]data.ValueMappings {
	info, err := d.datasetInfoFor(ctx, datasetID)
	if err != nil {
		backend.Logger.Warn("erddap: loading dataset info failed", "datasetId", datasetID, "error", err)
		return nil
	}
	return info.Mappings
}

// erddapStatusError is a non-200 ERDDAP response, carrying the upstream HTTP
// status alongside the message extracted from the body so a resource handler
// can translate it (e.g. an unknown datasetID's 404 stays a 404) instead of
// flattening everything into one generic failure.
type erddapStatusError struct {
	status  int
	message string
}

func (e *erddapStatusError) Error() string {
	return e.message
}

// newERDDAPStatusError wraps status/message as an erddapStatusError tagged
// with the error source implied by the HTTP status, matching fetch's
// convention. An empty message falls back to naming the status code so the
// error is never blank.
func newERDDAPStatusError(status int, message string) error {
	if message == "" {
		message = fmt.Sprintf("ERDDAP returned HTTP %d", status)
	}
	return backend.NewErrorWithSource(
		&erddapStatusError{status: status, message: message},
		backend.ErrorSourceFromHTTPStatus(status),
	)
}

// erddapStatusOf reports the upstream HTTP status carried by err, and false
// if err did not come from a non-200 ERDDAP response.
func erddapStatusOf(err error) (int, bool) {
	var statusErr *erddapStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status, true
	}
	return 0, false
}
