package plugin

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// flagMappingsCacheTTL bounds how long a dataset's flag_values/flag_meanings
// metadata is cached before being re-fetched from ERDDAP. Metadata changes
// rarely; this just bounds staleness if a dataset's flag vocabulary changes
// upstream.
const flagMappingsCacheTTL = time.Hour

// maxInfoResponseBytes bounds how much of an ERDDAP dataset info response
// (.../info/<id>/index.json) flagMappingsFor will read. Info responses list
// every global and per-variable attribute, so they run larger than the tiny
// error/version bodies maxDiagnosticBodyBytes is sized for.
const maxInfoResponseBytes = 256 << 10 // 256 KiB

// flagMappingsCacheEntry is one dataset's cached mappings and expiry.
type flagMappingsCacheEntry struct {
	mappings  map[string]data.ValueMappings
	expiresAt time.Time
}

// flagMappingsCache is an in-memory, per-Datasource-instance cache of
// dataset ID -> flag value mappings, guarded by a mutex since QueryData
// serves concurrent queries.
type flagMappingsCache struct {
	mu      sync.Mutex
	entries map[string]flagMappingsCacheEntry
}

func newFlagMappingsCache() *flagMappingsCache {
	return &flagMappingsCache{entries: map[string]flagMappingsCacheEntry{}}
}

// get returns the cached mappings for datasetID, and false if there is no
// unexpired entry.
func (c *flagMappingsCache) get(datasetID string) (map[string]data.ValueMappings, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[datasetID]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.mappings, true
}

// set caches mappings for datasetID for flagMappingsCacheTTL.
func (c *flagMappingsCache) set(datasetID string, mappings map[string]data.ValueMappings) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[datasetID] = flagMappingsCacheEntry{
		mappings:  mappings,
		expiresAt: time.Now().Add(flagMappingsCacheTTL),
	}
}

// flagMappingsFor returns the flag_values/flag_meanings-derived value
// mappings for datasetID's variables, fetching ERDDAP's
// .../info/<datasetID>/index.json and caching the result on a miss. Any
// failure (transport error, non-200, malformed body) logs a warning and
// returns nil: the caller proceeds with no mappings rather than failing the
// query over a metadata hiccup.
func (d *Datasource) flagMappingsFor(ctx context.Context, datasetID string) map[string]data.ValueMappings {
	if cached, ok := d.flagMappings.get(datasetID); ok {
		return cached
	}

	infoURL := d.settings.BaseURL + "/info/" + url.PathEscape(datasetID) + "/index.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		backend.Logger.Warn("erddap: building dataset info request failed", "datasetId", datasetID, "error", err)
		return nil
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		backend.Logger.Warn("erddap: fetching dataset info failed", "datasetId", datasetID, "error", err)
		return nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		backend.Logger.Warn("erddap: dataset info returned non-200", "datasetId", datasetID, "status", resp.StatusCode)
		return nil
	}

	mappings, err := parseInfoJSON(io.LimitReader(resp.Body, maxInfoResponseBytes))
	if err != nil {
		backend.Logger.Warn("erddap: parsing dataset info failed", "datasetId", datasetID, "error", err)
		return nil
	}

	d.flagMappings.set(datasetID, mappings)
	return mappings
}
