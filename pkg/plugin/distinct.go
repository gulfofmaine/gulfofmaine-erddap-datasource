package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// maxDistinctResponseBytes bounds how much of a distinct() response is read.
// A distinct-values list is a single column of short values — a station list
// or a depth list runs to a few kilobytes — so this is generous headroom
// rather than a working size, and far below the 2 MiB a full catalog search
// (maxSearchResponseBytes) needs.
const maxDistinctResponseBytes = 512 << 10 // 512 KiB

// erddapDistinctFunc is ERDDAP's server-side function that sorts the results
// table and drops non-unique rows. Requested alongside exactly one variable,
// it yields that variable's distinct values.
//
// It is written literally rather than escaped: `(` and `)` are in
// erddapStructuralChars, so escapeERDDAP would leave it byte-identical
// anyway, and ERDDAP requires the parens raw.
const erddapDistinctFunc = "distinct()"

// buildDistinctURL builds the ERDDAP tabledap .json request URL listing the
// distinct values of one variable in datasetID, optionally narrowed by
// constraints.
//
// The query string is ERDDAP's positional "vars&constraint&function" form
// (not key=value pairs), so it is built by hand and assigned directly to
// u.RawQuery rather than through url.Values — the same convention
// buildTabledapURL uses.
//
// distinct() is placed last: ERDDAP's tabledap documentation says to add it
// "to the end of a query", and that server-side functions are applied in the
// order they appear, so any caller-supplied constraints must precede it.
//
// Note that no time range is applied. A caller who wants the values scoped to
// a window says so through constraints (e.g. "time>=2024-01-01"); this is
// deliberate, since a dashboard variable's option list generally should not
// churn with the panel's time picker.
func buildDistinctURL(baseURL, datasetID, variable, constraints string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	// See buildTabledapURL: JoinPath treats each element as pre-escaped and
	// path-cleans the result, so PathEscape first neutralizes any "/" or "%"
	// in the datasetID.
	u = u.JoinPath("tabledap", url.PathEscape(datasetID+".json"))

	parts := []string{escapeERDDAP(variable)}
	if constraints != "" {
		parts = append(parts, normalizeERDDAPConstraints(constraints))
	}
	parts = append(parts, erddapDistinctFunc)

	u.RawQuery = strings.Join(parts, "&")

	return u.String(), nil
}

// parseDistinctJSON decodes a single-column ERDDAP tabledap .json response
// into the column's values, in the order ERDDAP returned them.
//
// Order is preserved rather than re-sorted: distinct() already sorts its
// output, so ERDDAP's ordering is the meaningful one for the variable's type
// (chronological for a time column, ascending for a numeric one).
//
// Cells are decoded as json.RawMessage because the column's type decides
// their shape: a String variable yields quoted strings, while a numeric one
// (e.g. depth) yields bare JSON numbers. A number is rendered from its
// literal text so the value round-trips exactly as ERDDAP wrote it, without
// a float64 detour that could reformat or lose precision. Null cells, empty
// strings, and ragged (short) rows are skipped.
func parseDistinctJSON(r io.Reader) ([]string, error) {
	var resp erddapTableResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}

	values := make([]string, 0, len(resp.Table.Rows))
	for _, row := range resp.Table.Rows {
		if len(row) == 0 || isJSONNull(row[0]) {
			continue
		}

		var value string
		if s := parseStringCell(row[0]); s != nil {
			value = *s
		} else {
			value = strings.TrimSpace(string(row[0]))
		}

		if value == "" {
			continue
		}

		values = append(values, value)
	}

	return values, nil
}

// distinctValues returns the distinct values of variable in datasetID,
// optionally narrowed by constraints.
//
// Error handling follows searchDatasets: a 404 carrying ERDDAP's canonical
// "no matching results" message is not a failure but an empty list (a
// dashboard variable with no options, not a broken query), while every other
// non-200 becomes an error carrying the upstream status and message so a
// resource handler can map it back onto an HTTP status of its own.
func (d *Datasource) distinctValues(ctx context.Context, datasetID, variable, constraints string) ([]string, error) {
	distinctURL, err := buildDistinctURL(d.settings.BaseURL, datasetID, variable, constraints)
	if err != nil {
		return nil, backend.DownstreamError(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, distinctURL, nil)
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
			return []string{}, nil
		}

		return nil, newERDDAPStatusError(resp.StatusCode, message)
	}

	values, err := parseDistinctJSON(io.LimitReader(resp.Body, maxDistinctResponseBytes))
	if err != nil {
		return nil, backend.DownstreamError(fmt.Errorf("erddap: parsing distinct values: %w", err))
	}

	return values, nil
}
