package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	// defaultSearchLimit is the itemsPerPage used when the caller doesn't ask
	// for a specific page size.
	defaultSearchLimit = 100

	// maxSearchLimit caps itemsPerPage. A full unfiltered browse of a large
	// ERDDAP server runs to hundreds of kilobytes, dominated by summaries.
	maxSearchLimit = 500

	// maxSearchResponseBytes bounds how much of a search response is read.
	maxSearchResponseBytes = 2 << 20 // 2 MiB

	// maxSummaryLen bounds each dataset's summary. Full ERDDAP summaries are
	// paragraphs long and account for most of a search response's size; a
	// picker only needs enough to recognize a dataset.
	maxSummaryLen = 300

	// erddapAllDatasetsID is ERDDAP's pseudo-dataset describing every other
	// dataset on the server. It shows up in search results but is never
	// something a user means to plot.
	erddapAllDatasetsID = "allDatasets"
)

// ERDDAP /search/advanced.json result column names. The response also carries
// griddap/wms/files/RSS/Email/FGDC/ISO 19115/Info columns this plugin ignores.
const (
	searchColDatasetID   = "Dataset ID"
	searchColTitle       = "Title"
	searchColInstitution = "Institution"
	searchColSummary     = "Summary"
	searchColTabledap    = "tabledap"
)

// erddapSearchResponse mirrors an ERDDAP /search/advanced.json response.
// Every column is a JSON string (columnTypes is all "String"), so rows decode
// directly as [][]string.
type erddapSearchResponse struct {
	Table struct {
		ColumnNames []string   `json:"columnNames"`
		Rows        [][]string `json:"rows"`
	} `json:"table"`
}

// erddapDataset is one dataset from a search result.
type erddapDataset struct {
	ID          string
	Title       string
	Institution string
	Summary     string

	// TabledapSupported reports whether the dataset is reachable over
	// tabledap, which is the only protocol this plugin queries. Griddap-only
	// datasets are still returned so a picker can show them as unsupported
	// rather than silently hiding a dataset the user searched for by name.
	TabledapSupported bool
}

// buildSearchURL builds the ERDDAP advanced-search request URL.
//
// page and itemsPerPage are always set: ERDDAP responds to a
// /search/advanced.json request missing either one with a 302 redirect to the
// HTML search form, so the plugin would quietly receive a page of HTML in
// place of the JSON it asked for.
//
// No protocol filter is applied — griddap-only datasets are returned and
// flagged rather than excluded. Unlike the tabledap query string (which is
// ERDDAP's positional form and needs escapeERDDAP), this is an ordinary
// key=value query string, so url.Values handles the encoding.
func buildSearchURL(baseURL, searchFor string, limit int) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u = u.JoinPath("search", "advanced.json")

	q := url.Values{}
	q.Set("page", "1")
	q.Set("itemsPerPage", strconv.Itoa(limit))
	if searchFor != "" {
		q.Set("searchFor", searchFor)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// parseSearchJSON decodes an ERDDAP /search/advanced.json response body.
//
// Columns are resolved by name, since ERDDAP's column set and ordering vary
// with server configuration. Only Dataset ID is required; the descriptive
// columns default to empty when a server omits them. Rows without a dataset
// ID, and ERDDAP's allDatasets pseudo-dataset, are dropped.
func parseSearchJSON(r io.Reader) ([]erddapDataset, error) {
	var resp erddapSearchResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}

	idx := map[string]int{}
	for i, name := range resp.Table.ColumnNames {
		idx[name] = i
	}

	idIdx, ok := idx[searchColDatasetID]
	if !ok {
		return nil, errors.New("erddap: search response missing the \"Dataset ID\" column")
	}

	datasets := make([]erddapDataset, 0, len(resp.Table.Rows))
	for _, row := range resp.Table.Rows {
		if idIdx >= len(row) {
			continue // defensive against a ragged row
		}

		id := row[idIdx]
		if id == "" || id == erddapAllDatasetsID {
			continue
		}

		datasets = append(datasets, erddapDataset{
			ID:                id,
			Title:             cellFor(row, idx, searchColTitle),
			Institution:       cellFor(row, idx, searchColInstitution),
			Summary:           truncateRunes(cellFor(row, idx, searchColSummary), maxSummaryLen),
			TabledapSupported: cellFor(row, idx, searchColTabledap) != "",
		})
	}

	return datasets, nil
}

// cellFor returns row's value for the named column, or "" if the column is
// absent from this response or the row is short.
func cellFor(row []string, idx map[string]int, column string) string {
	i, ok := idx[column]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

// truncateRunes shortens s to at most n runes, cutting on a rune boundary so
// a truncated summary never ends in a broken UTF-8 sequence.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s // byte length bounds rune count, so this is already short enough
	}

	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// searchDatasets runs an ERDDAP advanced search and returns the matching
// datasets.
//
// A zero-match search is not an error: ERDDAP reports it as a 404 carrying
// its canonical "no matching results" message, which becomes an empty list so
// a picker can say "no matches" instead of showing a failure. Every other
// non-200 is returned as an error carrying the upstream status and message.
func (d *Datasource) searchDatasets(ctx context.Context, searchFor string, limit int) ([]erddapDataset, error) {
	searchURL, err := buildSearchURL(d.settings.BaseURL, searchFor, limit)
	if err != nil {
		return nil, backend.DownstreamError(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
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
		message := extractERDDAPMessage(body)

		if resp.StatusCode == http.StatusNotFound && strings.Contains(message, erddapNoDataMessage) {
			return []erddapDataset{}, nil
		}

		return nil, newERDDAPStatusError(resp.StatusCode, message)
	}

	datasets, err := parseSearchJSON(io.LimitReader(resp.Body, maxSearchResponseBytes))
	if err != nil {
		return nil, backend.DownstreamError(fmt.Errorf("erddap: parsing search results: %w", err))
	}

	return datasets, nil
}

// searchLimitFrom parses the caller's ?limit= into an itemsPerPage value,
// clamping it to maxSearchLimit and falling back to defaultSearchLimit when
// it is missing, unparseable, or not positive.
func searchLimitFrom(raw string) int {
	if raw == "" {
		return defaultSearchLimit
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultSearchLimit
	}

	return min(n, maxSearchLimit)
}
