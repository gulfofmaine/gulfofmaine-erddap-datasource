package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/gulfofmaine/erddap/pkg/models"
)

// erddapNoDataMessage is the substring ERDDAP includes in the "message" of
// its 404 response body when a syntactically valid query matches zero rows.
// Any other 404 (e.g. an unknown datasetID) does not contain this substring
// and is treated as a downstream error instead.
const erddapNoDataMessage = "Your query produced no matching results"

// erddapMessageRe extracts the message="..." field from ERDDAP's canonical
// plain-text error body, e.g.:
//
//	Error {
//	    code=404;
//	    message="Not Found: Your query produced no matching results. (nRows = 0)";
//	}
var erddapMessageRe = regexp.MustCompile(`message="([^"]*)"`)

// maxFallbackMessageLen bounds the fallback error message when an ERDDAP
// error body doesn't match the expected message="..." shape.
const maxFallbackMessageLen = 500

// maxDiagnosticBodyBytes bounds how much of a response body fetch and
// CheckHealth will read for error messages and health-check probes. ERDDAP
// error bodies and the /version endpoint are both tiny plain text; this is
// generous headroom against a misbehaving or malicious server streaming an
// unbounded body.
const maxDiagnosticBodyBytes = 8 << 10 // 8 KiB

// erddapStructuralChars are ERDDAP query-string characters that are kept
// literal (unescaped) by escapeERDDAP even though they fall outside the
// RFC 3986 "unreserved" set. `&` separates constraints/variables, `,`
// separates variable names, `=` and the relational operators are embedded
// directly in constraints (e.g. "depth<2"), and `(`, `)`, `:`, `/` show up
// in constraint values ERDDAP expects raw (timestamps, function calls).
const erddapStructuralChars = "&,=!():/"

// numericColumnTypes are the ERDDAP tabledap column dataTypes that should be
// decoded as []*float64 fields.
var numericColumnTypes = map[string]bool{
	"byte": true, "ubyte": true,
	"short": true, "ushort": true,
	"int": true, "uint": true,
	"long": true, "ulong": true,
	"float": true, "double": true,
}

// erddapRow pairs a parsed row time with the raw cells of that row, so rows
// can be sorted by time before columnar data.Fields are built. Multi-station
// tabledap results can arrive with interleaved timestamps.
type erddapRow struct {
	time  time.Time
	cells []json.RawMessage
}

// erddapTableResponse mirrors the shape of an ERDDAP tabledap .json response:
// https://erddap.<host>/erddap/tabledap/<datasetID>.json
type erddapTableResponse struct {
	Table struct {
		ColumnNames []string            `json:"columnNames"`
		ColumnTypes []string            `json:"columnTypes"`
		ColumnUnits []string            `json:"columnUnits"`
		Rows        [][]json.RawMessage `json:"rows"`
	} `json:"table"`
}

// buildTabledapURL builds the ERDDAP tabledap .json request URL for qm over
// the query's time range tr. The query string is ERDDAP's positional
// "vars&constraint&constraint" form (not key=value pairs), so it is built by
// hand and assigned directly to u.RawQuery rather than through url.Values
// (which would re-order and re-encode as key=value).
func buildTabledapURL(baseURL string, qm models.QueryModel, tr backend.TimeRange) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	// url.JoinPath treats each element as already escaped: it does not
	// percent-encode them, and it path-cleans the joined result (collapsing
	// "../" etc). url.PathEscape first turns any "/" or "%" in the
	// datasetID into %2F/%25 so JoinPath can't misinterpret them as path
	// separators or (invalid) percent-escapes to clean away.
	u = u.JoinPath("tabledap", url.PathEscape(qm.DatasetID+".json"))

	variables := cleanVariables(qm.Variables)

	// Each segment is escaped on its own, then joined with a literal "&",
	// rather than joining first and escaping the whole string: the user
	// Constraints segment needs quote-aware escaping (escapeERDDAPConstraints)
	// while the generated variables/time segments keep using escapeERDDAP.
	// Since "&" is always kept literal by both escapers, this produces the
	// same output as escaping-then-joining for every segment that doesn't
	// itself need constraint-quote awareness.
	parts := []string{
		escapeERDDAP(strings.Join(variables, ",")),
		escapeERDDAP("time>=" + tr.From.UTC().Format(time.RFC3339)),
		escapeERDDAP("time<=" + tr.To.UTC().Format(time.RFC3339)),
	}
	if qm.Constraints != "" {
		parts = append(parts, normalizeERDDAPConstraints(qm.Constraints))
	}

	u.RawQuery = strings.Join(parts, "&")

	return u.String(), nil
}

// cleanVariables splits the user-supplied comma-separated variable list,
// trims whitespace, drops empty entries, and prepends "time" (de-duping it
// if the user already included it — ERDDAP always needs a time column and
// listing it twice is invalid).
func cleanVariables(raw string) []string {
	result := []string{"time"}
	seen := map[string]bool{"time": true}

	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}

	return result
}

// escapeERDDAP percent-encodes s rune-by-rune for use in an ERDDAP tabledap
// query string: RFC 3986 unreserved characters (A-Za-z0-9-_.~) and ERDDAP's
// structural characters (erddapStructuralChars) are kept literal; everything
// else — including the `< > "` characters RFC 3986 forbids raw in a query
// string, spaces, and non-ASCII runes — is percent-encoded byte-by-byte over
// the rune's UTF-8 encoding.
//
// This treats every structural character literally regardless of context, so
// it is only safe for segments that don't contain user-supplied quoted
// string values (the variables list and the generated time>=/time<=
// constraints). User Constraints go through escapeERDDAPConstraints instead,
// which is quote-aware.
func escapeERDDAP(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if isUnreservedRune(r) || strings.ContainsRune(erddapStructuralChars, r) {
			b.WriteRune(r)
			continue
		}

		percentEncodeRune(&b, r)
	}

	return b.String()
}

// escapeERDDAPConstraints percent-encodes a user-supplied ERDDAP constraints
// segment, the same way escapeERDDAP does outside of double-quoted string
// values — but inside a quoted value (e.g. the "A&B" in station="A&B") every
// character that is not RFC 3986 "unreserved" is percent-encoded, including
// erddapStructuralChars like `& , ( ) : /`. This lets a user type a literal
// `&` (or comma, parens, etc.) inside a quoted constraint value and have it
// escaped automatically, instead of being misread as the `&` that separates
// constraints.
//
// Quote state is tracked by counting `"` runes, toggled on each unescaped
// `"`. A backslash-escaped `\"` inside a quoted value is treated as a
// literal embedded quote character, not the end of the string, so it does
// not toggle the quote state.
func escapeERDDAPConstraints(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	runes := []rune(s)
	inQuotes := false
	for i, r := range runes {
		if r == '"' {
			backslashes := 0
			for j := i - 1; j >= 0 && runes[j] == '\\'; j-- {
				backslashes++
			}
			escaped := inQuotes && backslashes%2 == 1
			if !escaped {
				inQuotes = !inQuotes
			}
			b.WriteString("%22")
			continue
		}

		if inQuotes {
			if isUnreservedRune(r) {
				b.WriteRune(r)
			} else {
				percentEncodeRune(&b, r)
			}
			continue
		}

		if isUnreservedRune(r) || strings.ContainsRune(erddapStructuralChars, r) {
			b.WriteRune(r)
			continue
		}

		percentEncodeRune(&b, r)
	}

	return b.String()
}

// normalizeERDDAPConstraints decodes s with url.PathUnescape when every "%"
// in it is a valid percent-escape (so ERDDAP's own pre-encoded Data Access
// Form output round-trips instead of being double-encoded), then escapes
// the result the same way a hand-typed value would be. An invalid escape
// falls back to treating s as raw, unescaped input — today's behavior.
func normalizeERDDAPConstraints(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		s = decoded
	}
	return escapeERDDAPConstraints(s)
}

// percentEncodeRune writes r to b as one or more "%XX" escapes over its
// UTF-8 encoding.
func percentEncodeRune(b *strings.Builder, r rune) {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	for _, c := range buf[:n] {
		fmt.Fprintf(b, "%%%02X", c)
	}
}

func isUnreservedRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '_' || r == '.' || r == '~'
}

// parseTableJSON decodes an ERDDAP tabledap .json response body (the
// "columnNames/columnTypes/columnUnits/rows" shape) into data.Frames. Rows
// are sorted ascending by their time column before fields are built, since
// multi-station queries can return interleaved timestamps.
//
// Non-numeric (String) columns are not emitted as fields. Instead, rows are
// partitioned into one frame per distinct combination of String-column
// values, and those values become data.Labels on the numeric fields. That
// keeps every frame a *wide* time series (a time field plus numeric fields
// only), which is what Grafana's alerting expression engine requires — a
// frame carrying a String field alongside time is classified as "long" and
// rejected with "input data must be a wide series". It also splits an
// interleaved multi-station response into one series per station instead of
// collapsing them into a single field with duplicate timestamps.
//
// A response with no String columns yields exactly one frame with no labels.
func parseTableJSON(r io.Reader, frameName string, mappings map[string]data.ValueMappings) (data.Frames, error) {
	var resp erddapTableResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}

	table := resp.Table

	timeIdx := -1
	for i, name := range table.ColumnNames {
		if name == "time" {
			timeIdx = i
			break
		}
	}
	if timeIdx == -1 {
		return nil, errors.New("erddap: response has no \"time\" column")
	}

	rows := make([]erddapRow, 0, len(table.Rows))
	for _, cells := range table.Rows {
		if timeIdx >= len(cells) || isJSONNull(cells[timeIdx]) {
			continue // skip rows with a null time
		}

		var s string
		if err := json.Unmarshal(cells[timeIdx], &s); err != nil {
			continue // time cell wasn't a JSON string; unparseable
		}

		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			continue // skip rows with an unparseable time
		}

		rows = append(rows, erddapRow{time: t, cells: cells})
	}

	sortRowsByTime(rows)

	// Numeric columns become value fields; every other column is a "factor"
	// whose values are lifted into labels (see the doc comment above).
	var valueIndices, factorIndices []int
	for i := range table.ColumnNames {
		if i == timeIdx {
			continue
		}
		var colType string
		if i < len(table.ColumnTypes) {
			colType = table.ColumnTypes[i]
		}
		if numericColumnTypes[colType] {
			valueIndices = append(valueIndices, i)
		} else {
			factorIndices = append(factorIndices, i)
		}
	}

	frames := make(data.Frames, 0, 1)
	for _, group := range groupRowsByFactors(rows, factorIndices, table.ColumnNames) {
		frame := data.NewFrame(frameName)

		times := make([]time.Time, len(group.rows))
		for j, row := range group.rows {
			times[j] = row.time
		}
		frame.Fields = append(frame.Fields, data.NewField(table.ColumnNames[timeIdx], nil, times))

		for _, i := range valueIndices {
			values := make([]*float64, len(group.rows))
			for j, row := range group.rows {
				values[j] = parseFloatCell(cellAt(row.cells, i))
			}

			name := table.ColumnNames[i]
			field := data.NewField(name, group.labels, values)

			var unit string
			if i < len(table.ColumnUnits) {
				unit = table.ColumnUnits[i]
			}
			if unit != "" && unit != "UTC" {
				// Grafana reads FieldConfig.Unit as a unit ID, so a raw ERDDAP unit
				// string like "m" would be formatted as minutes. The "suffix:" custom
				// unit renders the text verbatim after the value instead.
				field.Config = &data.FieldConfig{Unit: "suffix:" + unit}
			}

			if vm, ok := mappings[name]; ok {
				if field.Config == nil {
					field.Config = &data.FieldConfig{}
				}
				field.Config.Mappings = vm
			}

			frame.Fields = append(frame.Fields, field)
		}

		frames = append(frames, frame)
	}

	return frames, nil
}

// rowGroup is a set of rows sharing the same values across every factor
// (non-numeric) column, along with the labels those values produce.
type rowGroup struct {
	labels data.Labels
	rows   []erddapRow
}

// groupRowsByFactors partitions rows by their combined factor-column values,
// preserving first-appearance order so frame ordering is deterministic (rows
// arrive already sorted by time, so this is the order of each group's
// earliest row). A null or non-string factor cell contributes an empty label
// value rather than dropping the row.
//
// With no factor columns it returns a single unlabeled group holding every
// row, which is the common numeric-only query. A zero-row response likewise
// yields one unlabeled (and therefore zero-row) group rather than no groups
// at all, so callers always get a typed frame to render "No data" from.
func groupRowsByFactors(rows []erddapRow, factorIndices []int, columnNames []string) []rowGroup {
	if len(factorIndices) == 0 || len(rows) == 0 {
		return []rowGroup{{labels: nil, rows: rows}}
	}

	var groups []rowGroup
	byKey := map[string]int{}

	for _, row := range rows {
		values := make([]string, len(factorIndices))
		for k, i := range factorIndices {
			if s := parseStringCell(cellAt(row.cells, i)); s != nil {
				values[k] = *s
			}
		}

		// Rows are keyed on the encoded value tuple rather than the label map
		// (which isn't comparable). The encoding is unambiguous because column
		// count is fixed, so no separator can be confused with a value.
		key, err := json.Marshal(values)
		if err != nil {
			continue
		}

		idx, ok := byKey[string(key)]
		if !ok {
			labels := make(data.Labels, len(factorIndices))
			for k, i := range factorIndices {
				labels[columnNames[i]] = values[k]
			}
			groups = append(groups, rowGroup{labels: labels})
			idx = len(groups) - 1
			byKey[string(key)] = idx
		}

		groups[idx].rows = append(groups[idx].rows, row)
	}

	return groups
}

// sortRowsByTime sorts rows ascending by time in place. Kept as a small,
// independently testable helper since parseTableJSON must re-sort
// multi-station tabledap results that can arrive with interleaved
// timestamps.
func sortRowsByTime(rows []erddapRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].time.Before(rows[j].time)
	})
}

// cellAt returns cells[i], or a JSON null RawMessage if the row is short a
// column (defensive against malformed/ragged ERDDAP rows).
func cellAt(cells []json.RawMessage, i int) json.RawMessage {
	if i >= len(cells) {
		return json.RawMessage("null")
	}
	return cells[i]
}

// isJSONNull reports whether raw is a JSON null (or empty/absent, which is
// treated the same way).
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// parseFloatCell decodes a numeric-column cell into *float64, returning nil
// for JSON null or any value that doesn't decode as a number (e.g. ERDDAP's
// bare, unquoted "NaN" token, which is not valid JSON).
func parseFloatCell(raw json.RawMessage) *float64 {
	if isJSONNull(raw) {
		return nil
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil
	}

	return &f
}

// parseStringCell decodes a String-column cell into *string, returning nil
// for JSON null or any value that doesn't decode as a JSON string.
func parseStringCell(raw json.RawMessage) *string {
	if isJSONNull(raw) {
		return nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}

	return &s
}

// setTimeSeriesMeta stamps every frame with the multi-frame time series data
// plane type and the tabledap URL that produced it, so the query inspector
// (and alert-rule debugging) shows the real request.
//
// Note that Meta.Type is descriptive only: what Grafana's alerting
// expression engine actually enforces is that each frame's
// TimeSeriesSchema() is wide, which parseTableJSON guarantees by construction.
func setTimeSeriesMeta(frames data.Frames, url string) data.Frames {
	for _, frame := range frames {
		frame.Meta = &data.FrameMeta{
			Type:                data.FrameTypeTimeSeriesMulti,
			TypeVersion:         data.FrameTypeVersion{0, 1},
			ExecutedQueryString: url,
		}
	}
	return frames
}

// fetch issues the ERDDAP tabledap request at url and decodes the result
// into data.Frames named after qm.DatasetID.
//
//   - A transport-level failure (DNS, connection refused, timeout, ...) is
//     wrapped as a backend.DownstreamError.
//   - HTTP 200 with a body parseTableJSON can't decode is wrapped as a
//     backend.DownstreamError: the request succeeded, so this is ERDDAP
//     returning something unexpected rather than a plugin bug, and should
//     not be attributed to the plugin in error-source metrics.
//   - HTTP 404 whose body contains ERDDAP's canonical "no matching results"
//     message is not an error: it returns an empty frame with the same
//     typed fields (time + one []*float64 per requested variable) a
//     successful query would have, so panels render "No data" instead of
//     an error.
//   - Any other non-200 response has its message="..." extracted from the
//     body (falling back to a trimmed body prefix, or — if the body is
//     empty or doesn't match either shape — "ERDDAP returned HTTP <code>" so
//     the error is never blank) and is wrapped with
//     backend.NewErrorWithSource, deriving the ErrorSource from the HTTP
//     status code.
func (d *Datasource) fetch(ctx context.Context, url string, qm models.QueryModel, mappings map[string]data.ValueMappings) (data.Frames, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	if resp.StatusCode == http.StatusOK {
		frames, err := parseTableJSON(resp.Body, qm.DatasetID, mappings)
		if err != nil {
			return nil, backend.DownstreamError(fmt.Errorf("erddap: parsing response: %w", err))
		}
		return setTimeSeriesMeta(frames, url), nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDiagnosticBodyBytes))
	message := extractERDDAPMessage(body)
	if message == "" {
		message = fmt.Sprintf("ERDDAP returned HTTP %d", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusNotFound && strings.Contains(message, erddapNoDataMessage) {
		return setTimeSeriesMeta(emptyTypedFrame(qm, mappings), url), nil
	}

	return nil, backend.NewErrorWithSource(errors.New(message), backend.ErrorSourceFromHTTPStatus(resp.StatusCode))
}

// extractERDDAPMessage pulls the message="..." field out of an ERDDAP error
// body. If the body doesn't match that shape, it falls back to a trimmed
// prefix of the raw body (bounded to maxFallbackMessageLen bytes).
func extractERDDAPMessage(body []byte) string {
	if m := erddapMessageRe.FindSubmatch(body); m != nil {
		return string(m[1])
	}

	s := strings.TrimSpace(string(body))
	if len(s) > maxFallbackMessageLen {
		s = s[:maxFallbackMessageLen]
	}
	return s
}

// emptyTypedFrame builds the zero-row frame returned when an ERDDAP query is
// valid but matches no rows: a time field plus one []*float64 field per
// user-requested variable (the auto-prepended "time" entry from
// cleanVariables is not counted twice).
//
// Every variable is typed as numeric because the no-data path never sees a
// columnTypes list to tell String columns apart. The frame is wide either
// way, so an alert rule over it evaluates to NoData rather than erroring;
// the only visible effect is that a String variable's field type differs
// between the empty and non-empty responses.
func emptyTypedFrame(qm models.QueryModel, mappings map[string]data.ValueMappings) data.Frames {
	variables := cleanVariables(qm.Variables)

	frame := data.NewFrame(qm.DatasetID)
	frame.Fields = append(frame.Fields, data.NewField("time", nil, []time.Time{}))
	for _, v := range variables[1:] {
		field := data.NewField(v, nil, []*float64{})
		if vm, ok := mappings[v]; ok {
			field.Config = &data.FieldConfig{Mappings: vm}
		}
		frame.Fields = append(frame.Fields, field)
	}

	return data.Frames{frame}
}
