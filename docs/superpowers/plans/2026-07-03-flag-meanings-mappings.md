# CF flag_meanings Value Mappings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** For every queried ERDDAP variable that declares both the CF `flag_values` and `flag_meanings`
attributes (e.g. QARTOD quality-flag variables), attach Grafana value mappings to that field so panels
show the flag meaning ("GOOD", "SUSPECT", ...) and a conventional color instead of a bare integer.

**Architecture:** The Go backend fetches each dataset's `{baseUrl}/info/{datasetID}/index.json`
metadata, extracts `flag_values`/`flag_meanings` pairs per variable, and turns them into
`data.ValueMappings`. Metadata is cached per `Datasource` instance (keyed by dataset ID, 1 hour TTL) so
it isn't re-fetched on every query. `parseTableJSON` and `emptyTypedFrame` accept this mappings map and
attach it to the matching field's `FieldConfig.Mappings`. Any metadata failure (transport, non-200,
malformed body) logs a warning and the query proceeds with no mappings — it never fails a query.

**Tech Stack:** Go, `github.com/grafana/grafana-plugin-sdk-go` (`data.ValueMappings` /
`data.ValueMapper` / `data.ValueMappingResult` types), Go's standard `net/http`/`encoding/json`.

## Global Constraints

- Detection is attribute-driven (CF `flag_values` + `flag_meanings` pair) — no QARTOD variable names
  hardcoded anywhere.
- Data stays numeric; only `FieldConfig.Mappings` is attached, so time-series plotting is unaffected.
- Metadata cache: per `Datasource` instance, keyed by dataset ID, TTL = 1 hour, mutex-guarded.
  Failures are never cached (retried on the next query).
- Any flag-metadata fetch/parse failure must fail soft: log a warning via `backend.Logger.Warn` and
  return no mappings. It must never turn a working query into an error.
- If `flag_values` and `flag_meanings` have different list lengths for a variable, skip that variable
  entirely (no partial/misaligned mapping).
- Colors (case-insensitive meaning token match): `GOOD`/`PASS` → `#73BF69` (green),
  `SUSPECT`/`SUSPECT_OR_OF_HIGH_INTEREST` → `#FF9830` (orange), `FAIL` → `#F2495C` (red),
  `UNKNOWN`/`NOT_EVALUATED` → `#CCCCCC` (light grey), `MISSING` → `#808080` (dark grey). Unrecognized
  tokens get a text-only mapping (no color).
- No frontend, query model, or `plugin.json` changes are needed for this feature.

---

## File Structure

- **Create** `pkg/plugin/flag_mappings.go` — pure functions: parse an ERDDAP dataset info JSON body,
  zip `flag_values`/`flag_meanings` into `data.ValueMappings`, and pick a color per meaning token. No
  HTTP, no caching, no `Datasource`.
- **Create** `pkg/plugin/flag_mappings_test.go` — unit tests for the above.
- **Create** `pkg/plugin/flag_mappings_cache.go` — the per-`Datasource` TTL cache
  (`flagMappingsCache`) and the `Datasource.flagMappingsFor` method that fetches, parses, caches, and
  fails soft.
- **Create** `pkg/plugin/flag_mappings_cache_test.go` — unit tests for the cache and
  `flagMappingsFor` (via `httptest`).
- **Modify** `pkg/plugin/erddap.go` — thread a `mappings map[string]data.ValueMappings` parameter
  through `fetch`, `parseTableJSON`, and `emptyTypedFrame`; merge it into each field's `Config`.
- **Modify** `pkg/plugin/erddap_test.go` — update existing `parseTableJSON` call site for the new
  parameter; add a test asserting mappings land on the right field and coexist with `Config.Unit`.
- **Modify** `pkg/plugin/datasource.go` — add the `flagMappings *flagMappingsCache` field to
  `Datasource`, initialize it in `NewDatasource`, and call `flagMappingsFor` from `handleQuery`.
- **Modify** `pkg/plugin/datasource_test.go` — add the new field to existing `Datasource{}` test
  literals, make the `TestQueryData` fake server path-aware, add an end-to-end
  `TestQueryDataWithFlagMappings` test.
- **Modify** `README.md` and `CHANGELOG.md` — document the new behavior.

---

### Task 1: Parse dataset info JSON into flag value mappings

**Files:**

- Create: `pkg/plugin/flag_mappings.go`
- Test: `pkg/plugin/flag_mappings_test.go`

**Interfaces:**

- Produces: `flagMappingColor(meaning string) string`, `buildFlagMapping(rawValues, rawMeanings string) (data.ValueMappings, bool)`, `parseInfoJSON(r io.Reader) (map[string]data.ValueMappings, error)` — all consumed by Task 3's `flagMappingsFor`.

- [ ] **Step 1: Write failing tests for `flagMappingColor`**

Create `pkg/plugin/flag_mappings_test.go`. This step only tests `flagMappingColor`, so only `testing`
is needed — Step 5 adds `data` when it adds `TestBuildFlagMapping`, and Step 9 adds `strings` when it
adds `TestParseInfoJSON`:

```go
package plugin

import (
	"testing"
)

func TestFlagMappingColor(t *testing.T) {
	tests := []struct {
		meaning string
		want    string
	}{
		{"GOOD", "#73BF69"},
		{"good", "#73BF69"},
		{"PASS", "#73BF69"},
		{"SUSPECT", "#FF9830"},
		{"SUSPECT_OR_OF_HIGH_INTEREST", "#FF9830"},
		{"FAIL", "#F2495C"},
		{"UNKNOWN", "#CCCCCC"},
		{"NOT_EVALUATED", "#CCCCCC"},
		{"MISSING", "#808080"},
		{"SOMETHING_ELSE", ""},
	}

	for _, tc := range tests {
		t.Run(tc.meaning, func(t *testing.T) {
			got := flagMappingColor(tc.meaning)
			if got != tc.want {
				t.Errorf("flagMappingColor(%q) = %q, want %q", tc.meaning, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestFlagMappingColor -v`
Expected: FAIL — `undefined: flagMappingColor`

- [ ] **Step 3: Implement `flagMappingColor`**

Create `pkg/plugin/flag_mappings.go` with this content. Only `strings` is imported here — later steps
add more functions to this file and will extend this import block exactly when a new import is first
used, so the file compiles cleanly at every step:

```go
package plugin

import (
	"strings"
)

// flagMappingColor returns the conventional color for a QARTOD/CF flag
// meaning token (matched case-insensitively), or "" for an unrecognized
// token, which leaves that mapping entry text-only.
func flagMappingColor(meaning string) string {
	switch strings.ToUpper(meaning) {
	case "GOOD", "PASS":
		return "#73BF69" // green
	case "SUSPECT", "SUSPECT_OR_OF_HIGH_INTEREST":
		return "#FF9830" // orange
	case "FAIL":
		return "#F2495C" // red
	case "UNKNOWN", "NOT_EVALUATED":
		return "#CCCCCC" // light grey
	case "MISSING":
		return "#808080" // dark grey
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestFlagMappingColor -v`
Expected: PASS

- [ ] **Step 5: Write failing tests for `buildFlagMapping`**

`TestBuildFlagMapping` needs the SDK's `data` types. First change `flag_mappings_test.go`'s import
block from:

```go
import (
	"testing"
)
```

to:

```go
import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

Then append to `pkg/plugin/flag_mappings_test.go`:

```go
func TestBuildFlagMapping(t *testing.T) {
	t.Run("QARTOD flags", func(t *testing.T) {
		vm, ok := buildFlagMapping("1, 2, 3, 4, 9", "GOOD UNKNOWN SUSPECT FAIL MISSING")
		if !ok {
			t.Fatal("expected ok = true")
		}
		if len(vm) != 1 {
			t.Fatalf("expected 1 ValueMapping, got %d", len(vm))
		}

		mapper, ok := vm[0].(data.ValueMapper)
		if !ok {
			t.Fatalf("expected a data.ValueMapper, got %T", vm[0])
		}

		want := map[string]data.ValueMappingResult{
			"1": {Text: "GOOD", Color: "#73BF69", Index: 0},
			"2": {Text: "UNKNOWN", Color: "#CCCCCC", Index: 1},
			"3": {Text: "SUSPECT", Color: "#FF9830", Index: 2},
			"4": {Text: "FAIL", Color: "#F2495C", Index: 3},
			"9": {Text: "MISSING", Color: "#808080", Index: 4},
		}
		for k, w := range want {
			got, ok := mapper[k]
			if !ok {
				t.Errorf("missing mapping for value %q", k)
				continue
			}
			if got != w {
				t.Errorf("mapping[%q] = %+v, want %+v", k, got, w)
			}
		}
	})

	t.Run("mismatched lengths are rejected", func(t *testing.T) {
		_, ok := buildFlagMapping("1, 2, 3", "GOOD UNKNOWN")
		if ok {
			t.Fatal("expected ok = false for mismatched list lengths")
		}
	})

	t.Run("empty flag_values is rejected", func(t *testing.T) {
		_, ok := buildFlagMapping("", "GOOD UNKNOWN")
		if ok {
			t.Fatal("expected ok = false for empty flag_values")
		}
	})

	t.Run("empty flag_meanings is rejected", func(t *testing.T) {
		_, ok := buildFlagMapping("1, 2", "")
		if ok {
			t.Fatal("expected ok = false for empty flag_meanings")
		}
	})
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestBuildFlagMapping -v`
Expected: FAIL — `undefined: buildFlagMapping`

- [ ] **Step 7: Implement `buildFlagMapping`**

`buildFlagMapping` needs the SDK's `data` types, so first change `flag_mappings.go`'s import block from:

```go
import (
	"strings"
)
```

to:

```go
import (
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

Then append to `pkg/plugin/flag_mappings.go`:

```go
// buildFlagMapping zips a CF flag_values attribute (a comma-separated
// numeric list, e.g. "1, 2, 3, 4, 9") with a flag_meanings attribute (a
// space-separated token list, e.g. "GOOD UNKNOWN SUSPECT FAIL MISSING")
// into a Grafana value mapping. It reports ok=false — and no mapping — if
// either attribute is empty or the two lists have different lengths, since
// a mismatched zip would silently mislabel flag values.
func buildFlagMapping(rawValues, rawMeanings string) (data.ValueMappings, bool) {
	if strings.TrimSpace(rawValues) == "" || strings.TrimSpace(rawMeanings) == "" {
		return nil, false
	}

	rawValueList := strings.Split(rawValues, ",")
	values := make([]string, len(rawValueList))
	for i, v := range rawValueList {
		values[i] = strings.TrimSpace(v)
	}

	meanings := strings.Fields(rawMeanings)

	if len(values) != len(meanings) {
		return nil, false
	}

	mapper := data.ValueMapper{}
	for i, v := range values {
		mapper[v] = data.ValueMappingResult{
			Text:  meanings[i],
			Color: flagMappingColor(meanings[i]),
			Index: i,
		}
	}

	return data.ValueMappings{mapper}, true
}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestBuildFlagMapping -v`
Expected: PASS

- [ ] **Step 9: Write failing tests for `parseInfoJSON`**

`TestParseInfoJSON` needs `strings.NewReader`. First change `flag_mappings_test.go`'s import block
from:

```go
import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

to:

```go
import (
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

Then append to `pkg/plugin/flag_mappings_test.go`:

```go
func TestParseInfoJSON(t *testing.T) {
	body := `{
		"table": {
			"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
			"columnTypes": ["String", "String", "String", "String", "String"],
			"rows": [
				["attribute", "NC_GLOBAL", "cdm_data_type", "String", "Point"],
				["variable", "temperature", "", "double", ""],
				["attribute", "temperature", "units", "String", "degree_C"],
				["variable", "navd88_meters_qartod_gross_range_test", "", "int", ""],
				["attribute", "navd88_meters_qartod_gross_range_test", "flag_meanings", "String", "GOOD UNKNOWN SUSPECT FAIL MISSING"],
				["attribute", "navd88_meters_qartod_gross_range_test", "flag_values", "long", "1, 2, 3, 4, 9"],
				["attribute", "navd88_meters_qartod_gross_range_test", "long_name", "String", "Gross Range Test Quality Flag"]
			]
		}
	}`

	mappings, err := parseInfoJSON(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := mappings["temperature"]; ok {
		t.Error("expected no mapping for temperature (no flag attributes)")
	}
	if _, ok := mappings["NC_GLOBAL"]; ok {
		t.Error("expected no mapping for NC_GLOBAL")
	}

	vm, ok := mappings["navd88_meters_qartod_gross_range_test"]
	if !ok {
		t.Fatal("expected a mapping for navd88_meters_qartod_gross_range_test")
	}
	mapper, ok := vm[0].(data.ValueMapper)
	if !ok {
		t.Fatalf("expected a data.ValueMapper, got %T", vm[0])
	}
	if mapper["3"].Text != "SUSPECT" {
		t.Errorf("expected value 3 = SUSPECT, got %+v", mapper["3"])
	}
}

func TestParseInfoJSONMissingColumns(t *testing.T) {
	body := `{"table": {"columnNames": ["Row Type", "Value"], "rows": []}}`

	_, err := parseInfoJSON(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected an error for missing expected columns")
	}
}
```

- [ ] **Step 10: Run the test to verify it fails**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestParseInfoJSON -v`
Expected: FAIL — `undefined: parseInfoJSON`

- [ ] **Step 11: Implement `parseInfoJSON`**

`parseInfoJSON` needs `encoding/json`, `errors`, and `io` in addition to the existing imports. Change
`flag_mappings.go`'s import block from:

```go
import (
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

to:

```go
import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

Then append to `pkg/plugin/flag_mappings.go`:

```go
// erddapInfoResponse mirrors the shape of an ERDDAP dataset .../info/<id>/index.json
// response: a flat table of variable/attribute rows. Every column in this
// response is a JSON string (columnTypes is all "String"), including
// numeric-looking attribute values like flag_values, so rows decode
// directly as [][]string.
type erddapInfoResponse struct {
	Table struct {
		ColumnNames []string   `json:"columnNames"`
		Rows        [][]string `json:"rows"`
	} `json:"table"`
}

// ERDDAP info response column names (see erddapInfoResponse).
const (
	infoColVariableName  = "Variable Name"
	infoColAttributeName = "Attribute Name"
	infoColValue         = "Value"
)

// CF flag attribute names.
const (
	flagValuesAttr   = "flag_values"
	flagMeaningsAttr = "flag_meanings"
)

// parseInfoJSON decodes an ERDDAP dataset info .../index.json response body
// and builds value mappings for every variable that declares both the
// flag_values and flag_meanings CF attributes. Variables without both
// attributes, or with mismatched flag_values/flag_meanings counts, are
// omitted from the result rather than erroring.
func parseInfoJSON(r io.Reader) (map[string]data.ValueMappings, error) {
	var resp erddapInfoResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}

	varIdx, attrIdx, valIdx := -1, -1, -1
	for i, name := range resp.Table.ColumnNames {
		switch name {
		case infoColVariableName:
			varIdx = i
		case infoColAttributeName:
			attrIdx = i
		case infoColValue:
			valIdx = i
		}
	}
	if varIdx == -1 || attrIdx == -1 || valIdx == -1 {
		return nil, errors.New("erddap: info response missing expected columns")
	}

	type flagAttrs struct {
		values   string
		meanings string
	}
	byVariable := map[string]*flagAttrs{}

	maxIdx := varIdx
	if attrIdx > maxIdx {
		maxIdx = attrIdx
	}
	if valIdx > maxIdx {
		maxIdx = valIdx
	}

	for _, row := range resp.Table.Rows {
		if maxIdx >= len(row) {
			continue // defensive against a ragged row
		}

		attr := row[attrIdx]
		if attr != flagValuesAttr && attr != flagMeaningsAttr {
			continue
		}

		variable := row[varIdx]
		fa, ok := byVariable[variable]
		if !ok {
			fa = &flagAttrs{}
			byVariable[variable] = fa
		}
		if attr == flagValuesAttr {
			fa.values = row[valIdx]
		} else {
			fa.meanings = row[valIdx]
		}
	}

	mappings := map[string]data.ValueMappings{}
	for variable, fa := range byVariable {
		vm, ok := buildFlagMapping(fa.values, fa.meanings)
		if !ok {
			continue
		}
		mappings[variable] = vm
	}

	return mappings, nil
}
```

- [ ] **Step 12: Run all of this file's tests to verify they pass**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run 'TestFlagMappingColor|TestBuildFlagMapping|TestParseInfoJSON' -v`
Expected: PASS (all subtests)

- [ ] **Step 13: Commit**

```bash
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH"
but commit flag-meanings-mappings -m "Parse ERDDAP dataset info JSON into flag value mappings" --changes <ids from `but diff`>
```

---

### Task 2: Thread mappings through the tabledap frame builder

**Files:**

- Modify: `pkg/plugin/erddap.go` (`fetch`, `parseTableJSON`, `emptyTypedFrame`)
- Modify: `pkg/plugin/datasource.go:91` (temporary `nil` placeholder — Task 4 replaces it)
- Modify: `pkg/plugin/erddap_test.go`

**Interfaces:**

- Consumes: nothing from Task 1 directly (this task just threads a parameter of the same
  `map[string]data.ValueMappings` type Task 1 produces).
- Produces: `parseTableJSON(r io.Reader, frameName string, mappings map[string]data.ValueMappings) (*data.Frame, error)`, `emptyTypedFrame(qm models.QueryModel, mappings map[string]data.ValueMappings) *data.Frame`, `(d *Datasource) fetch(ctx context.Context, url string, qm models.QueryModel, mappings map[string]data.ValueMappings) (*data.Frame, error)` — all consumed by Task 4's `handleQuery`.

- [ ] **Step 1: Write a failing test for mappings landing on a field**

In `pkg/plugin/erddap_test.go`, add `"github.com/grafana/grafana-plugin-sdk-go/data"` to the import
block, then append:

```go
func TestParseTableJSONWithMappings(t *testing.T) {
	body := `{
		"table": {
			"columnNames": ["time", "temperature", "navd88_meters_qartod_gross_range_test"],
			"columnTypes": ["String", "double", "int"],
			"columnUnits": ["UTC", "degree_C", ""],
			"rows": [
				["2024-01-01T00:00:00Z", 8.2, 1],
				["2024-01-01T01:00:00Z", 8.5, 3]
			]
		}
	}`

	mappings := map[string]data.ValueMappings{
		"temperature": {data.ValueMapper{"8.2": {Text: "COLD"}}},
		"navd88_meters_qartod_gross_range_test": {
			data.ValueMapper{
				"1": {Text: "GOOD", Color: "#73BF69"},
				"3": {Text: "SUSPECT", Color: "#FF9830"},
			},
		},
	}

	frame, err := parseTableJSON(strings.NewReader(body), "response", mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tempField := frame.Fields[1]
	if tempField.Config == nil || tempField.Config.Unit != "suffix:degree_C" {
		t.Fatalf("expected temperature to keep its Unit config, got %+v", tempField.Config)
	}
	if tempField.Config.Mappings == nil {
		t.Error("expected temperature Config.Mappings to be set alongside Unit")
	}

	flagField := frame.Fields[2]
	if flagField.Config == nil || flagField.Config.Mappings == nil {
		t.Fatalf("expected flag field to have Config.Mappings, got %+v", flagField.Config)
	}
	mapper, ok := flagField.Config.Mappings[0].(data.ValueMapper)
	if !ok {
		t.Fatalf("expected a data.ValueMapper, got %T", flagField.Config.Mappings[0])
	}
	if mapper["3"].Text != "SUSPECT" {
		t.Errorf("expected value 3 = SUSPECT, got %+v", mapper["3"])
	}
}
```

Also update the existing call site so the file still compiles: in `TestParseTableJSON`, change

```go
	frame, err := parseTableJSON(strings.NewReader(body), "response")
```

to

```go
	frame, err := parseTableJSON(strings.NewReader(body), "response", nil)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestParseTableJSON -v`
Expected: FAIL — too many arguments in call to `parseTableJSON`

- [ ] **Step 3: Update `parseTableJSON`'s signature and merge mappings into `Config`**

In `pkg/plugin/erddap.go`, change the function signature from
`func parseTableJSON(r io.Reader, frameName string) (*data.Frame, error) {` to:

```go
func parseTableJSON(r io.Reader, frameName string, mappings map[string]data.ValueMappings) (*data.Frame, error) {
```

Then, inside the `for i, name := range table.ColumnNames` loop, replace:

```go
		if unit != "" && unit != "UTC" {
			// Grafana reads FieldConfig.Unit as a unit ID, so a raw ERDDAP unit
			// string like "m" would be formatted as minutes. The "suffix:" custom
			// unit renders the text verbatim after the value instead.
			field.Config = &data.FieldConfig{Unit: "suffix:" + unit}
		}

		frame.Fields = append(frame.Fields, field)
```

with:

```go
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
```

- [ ] **Step 4: Update `fetch` and `emptyTypedFrame` to accept and pass through mappings**

In `pkg/plugin/erddap.go`, change the `fetch` signature from
`func (d *Datasource) fetch(ctx context.Context, url string, qm models.QueryModel) (*data.Frame, error) {`
to:

```go
func (d *Datasource) fetch(ctx context.Context, url string, qm models.QueryModel, mappings map[string]data.ValueMappings) (*data.Frame, error) {
```

Inside `fetch`, update the two call sites:

```go
		frame, err := parseTableJSON(resp.Body, qm.DatasetID, mappings)
```

and

```go
	if resp.StatusCode == http.StatusNotFound && strings.Contains(message, erddapNoDataMessage) {
		return emptyTypedFrame(qm, mappings), nil
	}
```

Change the `emptyTypedFrame` signature from `func emptyTypedFrame(qm models.QueryModel) *data.Frame {` to:

```go
func emptyTypedFrame(qm models.QueryModel, mappings map[string]data.ValueMappings) *data.Frame {
```

And inside it, replace:

```go
	for _, v := range variables[1:] {
		frame.Fields = append(frame.Fields, data.NewField(v, nil, []*float64{}))
	}
```

with:

```go
	for _, v := range variables[1:] {
		field := data.NewField(v, nil, []*float64{})
		if vm, ok := mappings[v]; ok {
			field.Config = &data.FieldConfig{Mappings: vm}
		}
		frame.Fields = append(frame.Fields, field)
	}
```

- [ ] **Step 5: Fix the now-broken call site in `datasource.go`**

In `pkg/plugin/datasource.go`, `handleQuery` currently has:

```go
	frame, err := d.fetch(ctx, tabledapURL, *qm)
```

Change it to pass a temporary `nil` (Task 4 replaces this with the real cache lookup):

```go
	frame, err := d.fetch(ctx, tabledapURL, *qm, nil)
```

- [ ] **Step 6: Run the full package test suite to verify everything passes**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -v`
Expected: PASS (all existing tests plus `TestParseTableJSONWithMappings`)

- [ ] **Step 7: Commit**

```bash
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH"
but commit flag-meanings-mappings -m "Thread flag value mappings through the tabledap frame builder" --changes <ids from `but diff`>
```

---

### Task 3: Per-dataset TTL cache and fail-soft metadata fetch

**Files:**

- Create: `pkg/plugin/flag_mappings_cache.go`
- Create: `pkg/plugin/flag_mappings_cache_test.go`
- Modify: `pkg/plugin/datasource.go` (`Datasource` struct, `NewDatasource`)

**Interfaces:**

- Consumes: `parseInfoJSON` from Task 1 (`pkg/plugin/flag_mappings.go`).
- Produces: `newFlagMappingsCache() *flagMappingsCache`, `(d *Datasource) flagMappingsFor(ctx context.Context, datasetID string) map[string]data.ValueMappings`, and the `Datasource.flagMappings *flagMappingsCache` field — all consumed by Task 4's `handleQuery`.

- [ ] **Step 1: Write failing tests for the cache**

Create `pkg/plugin/flag_mappings_cache_test.go`. This step only tests the cache itself, so only
`testing`, `time`, and `data` are needed — Step 6 below extends this import block when it adds
`TestFlagMappingsFor`, which needs `httptest`:

```go
package plugin

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestFlagMappingsCache(t *testing.T) {
	t.Run("miss for unknown dataset", func(t *testing.T) {
		c := newFlagMappingsCache()
		if _, ok := c.get("unknown"); ok {
			t.Fatal("expected a cache miss")
		}
	})

	t.Run("hit after set", func(t *testing.T) {
		c := newFlagMappingsCache()
		want := map[string]data.ValueMappings{"flag": {data.ValueMapper{"1": {Text: "GOOD"}}}}
		c.set("foo", want)

		got, ok := c.get("foo")
		if !ok {
			t.Fatal("expected a cache hit")
		}
		if got["flag"][0].(data.ValueMapper)["1"].Text != "GOOD" {
			t.Errorf("unexpected cached mappings: %+v", got)
		}
	})

	t.Run("expired entry is a miss", func(t *testing.T) {
		c := newFlagMappingsCache()
		c.entries["foo"] = flagMappingsCacheEntry{
			mappings:  map[string]data.ValueMappings{},
			expiresAt: time.Now().Add(-time.Second),
		}

		if _, ok := c.get("foo"); ok {
			t.Fatal("expected an expired entry to be a cache miss")
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestFlagMappingsCache -v`
Expected: FAIL — `undefined: newFlagMappingsCache`

- [ ] **Step 3: Implement `flagMappingsCache`**

Create `pkg/plugin/flag_mappings_cache.go`:

```go
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
```

- [ ] **Step 4: Add the `flagMappings` field to `Datasource` so the file above compiles**

In `pkg/plugin/datasource.go`, change:

```go
type Datasource struct {
	settings   *models.PluginSettings
	httpClient *http.Client
}
```

to:

```go
type Datasource struct {
	settings     *models.PluginSettings
	httpClient   *http.Client
	flagMappings *flagMappingsCache
}
```

And in `NewDatasource`, change:

```go
	return &Datasource{
		settings:   pluginSettings,
		httpClient: httpClient,
	}, nil
```

to:

```go
	return &Datasource{
		settings:     pluginSettings,
		httpClient:   httpClient,
		flagMappings: newFlagMappingsCache(),
	}, nil
```

- [ ] **Step 5: Run the cache tests to verify they pass**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestFlagMappingsCache -v`
Expected: PASS

- [ ] **Step 6: Write failing tests for `flagMappingsFor`**

`TestFlagMappingsFor` needs `context`, `net/http`, `net/http/httptest`, and the `models` package for
`*models.PluginSettings`. First change `flag_mappings_cache_test.go`'s import block from:

```go
import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)
```

to:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/gulfofmaine/erddap/pkg/models"
)
```

Then append to `pkg/plugin/flag_mappings_cache_test.go`:

```go
func TestFlagMappingsFor(t *testing.T) {
	t.Run("success caches the result", func(t *testing.T) {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.URL.Path != "/info/foo/index.json" {
				t.Errorf("expected request to /info/foo/index.json, got %q", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"table": {
					"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
					"rows": [
						["attribute", "flag", "flag_meanings", "String", "GOOD FAIL"],
						["attribute", "flag", "flag_values", "long", "1, 2"]
					]
				}
			}`))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings["flag"] == nil {
			t.Fatalf("expected a mapping for 'flag', got %+v", mappings)
		}

		// Second call should be served from cache, not a second request.
		ds.flagMappingsFor(context.Background(), "foo")
		if requests != 1 {
			t.Errorf("expected 1 request (second call served from cache), got %d", requests)
		}
	})

	t.Run("non-200 fails soft", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a non-200 response, got %+v", mappings)
		}
	})

	t.Run("malformed body fails soft", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		ds := &Datasource{
			settings:     &models.PluginSettings{BaseURL: srv.URL},
			httpClient:   srv.Client(),
			flagMappings: newFlagMappingsCache(),
		}

		mappings := ds.flagMappingsFor(context.Background(), "foo")
		if mappings != nil {
			t.Errorf("expected nil mappings on a malformed body, got %+v", mappings)
		}
	})
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run 'TestFlagMappingsCache|TestFlagMappingsFor' -v`
Expected: PASS (these should already pass against the Step 3 implementation — this step is the
verification pass, not a new implementation step)

- [ ] **Step 8: Run the full package test suite to verify nothing else broke**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH"
but commit flag-meanings-mappings -m "Add per-dataset TTL cache and fail-soft flag metadata fetch" --changes <ids from `but diff`>
```

---

### Task 4: Wire the cache into `handleQuery` end-to-end

**Files:**

- Modify: `pkg/plugin/datasource.go` (`handleQuery`)
- Modify: `pkg/plugin/datasource_test.go`

**Interfaces:**

- Consumes: `(d *Datasource) flagMappingsFor(ctx, datasetID) map[string]data.ValueMappings` (Task 3),
  `(d *Datasource) fetch(ctx, url, qm, mappings) (*data.Frame, error)` (Task 2).

- [ ] **Step 1: Write a failing end-to-end test**

In `pkg/plugin/datasource_test.go`, add `"github.com/grafana/grafana-plugin-sdk-go/data"` to the import
block, then append:

```go
func TestQueryDataWithFlagMappings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/info/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"table": {
					"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
					"rows": [
						["variable", "navd88_meters_qartod_gross_range_test", "", "int", ""],
						["attribute", "navd88_meters_qartod_gross_range_test", "flag_meanings", "String", "GOOD UNKNOWN SUSPECT FAIL MISSING"],
						["attribute", "navd88_meters_qartod_gross_range_test", "flag_values", "long", "1, 2, 3, 4, 9"]
					]
				}
			}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"table": {
				"columnNames": ["time", "navd88_meters_qartod_gross_range_test"],
				"columnTypes": ["String", "int"],
				"columnUnits": ["UTC", ""],
				"rows": [
					["2024-01-01T00:00:00Z", 1],
					["2024-01-01T01:00:00Z", 3]
				]
			}
		}`))
	}))
	defer srv.Close()

	ds := Datasource{
		settings:     &models.PluginSettings{BaseURL: srv.URL},
		httpClient:   srv.Client(),
		flagMappings: newFlagMappingsCache(),
	}

	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"datasetId": "foo", "variables": "navd88_meters_qartod_gross_range_test", "constraints": ""}`),
			},
		},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dr := resp.Responses["A"]
	if dr.Error != nil {
		t.Fatalf("unexpected DataResponse error: %v", dr.Error)
	}

	flagField := dr.Frames[0].Fields[1]
	if flagField.Config == nil || flagField.Config.Mappings == nil {
		t.Fatalf("expected flag field to have Config.Mappings, got %+v", flagField.Config)
	}

	mapper, ok := flagField.Config.Mappings[0].(data.ValueMapper)
	if !ok {
		t.Fatalf("expected a data.ValueMapper, got %T", flagField.Config.Mappings[0])
	}
	if mapper["1"].Text != "GOOD" {
		t.Errorf("expected value 1 = GOOD, got %+v", mapper["1"])
	}
	if mapper["3"].Text != "SUSPECT" {
		t.Errorf("expected value 3 = SUSPECT, got %+v", mapper["3"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -run TestQueryDataWithFlagMappings -v`
Expected: FAIL — `flagField.Config` is nil (handleQuery still passes `nil` mappings to `fetch`)

- [ ] **Step 3: Wire `flagMappingsFor` into `handleQuery`**

In `pkg/plugin/datasource.go`, change:

```go
	tabledapURL, err := buildTabledapURL(d.settings.BaseURL, *qm, q.DataQuery.TimeRange)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	frame, err := d.fetch(ctx, tabledapURL, *qm, nil)
```

to:

```go
	tabledapURL, err := buildTabledapURL(d.settings.BaseURL, *qm, q.DataQuery.TimeRange)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	mappings := d.flagMappingsFor(ctx, qm.DatasetID)

	frame, err := d.fetch(ctx, tabledapURL, *qm, mappings)
```

- [ ] **Step 4: Update existing `Datasource{}` test literals so they don't nil-panic**

The tests below now exercise `handleQuery`, which calls `d.flagMappings.get(...)` — a nil
`*flagMappingsCache` field would panic. In `pkg/plugin/datasource_test.go`, every occurrence of this
exact literal (in `TestQueryData`, `TestQueryDataNoData`, `TestQueryDataServerError`,
`TestQueryDataEmptyErrorBody`, and `TestQueryDataMalformedOKBody`):

```go
	ds := Datasource{
		settings:   &models.PluginSettings{BaseURL: srv.URL},
		httpClient: srv.Client(),
	}
```

becomes:

```go
	ds := Datasource{
		settings:     &models.PluginSettings{BaseURL: srv.URL},
		httpClient:   srv.Client(),
		flagMappings: newFlagMappingsCache(),
	}
```

(All five occurrences are character-for-character identical, so a single find-and-replace-all across
the file is correct here. Leave `TestCheckHealth`'s `Datasource{}` literals alone — `CheckHealth` never
touches `flagMappings`.)

- [ ] **Step 5: Make `TestQueryData`'s fake server path-aware**

`TestQueryData` asserts the exact request path and query string the datasource sent, captured in
`gotPath`/`gotRawQuery`. Once `handleQuery` also issues a `/info/foo/index.json` request, that request
must not overwrite those captured variables. In `pkg/plugin/datasource_test.go`, change `TestQueryData`'s
server handler from:

```go
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
```

to:

```go
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/info/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"table": {"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"], "rows": []}}`))
			return
		}

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
```

This requires `"strings"` to already be imported in `datasource_test.go` — it is (used by
`TestCheckHealth`).

- [ ] **Step 6: Run the full package test suite to verify everything passes**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go test ./pkg/plugin/... -v`
Expected: PASS (all tests, including the new `TestQueryDataWithFlagMappings`)

- [ ] **Step 7: Run the full repo test suite and lint**

Run: `export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && go build ./... && go vet ./... && go test ./...`
Expected: PASS, no build or vet errors

- [ ] **Step 8: Commit**

```bash
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH"
but commit flag-meanings-mappings -m "Wire flag value mappings into QueryData end-to-end" --changes <ids from `but diff`>
```

---

### Task 5: Document the feature

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: nothing (pure documentation, written after the feature is complete and tested).

- [ ] **Step 1: Add a README section**

In `README.md`, insert a new subsection immediately after the existing "### No matching results"
section and before "## Development":

```markdown
### Quality-flag value mappings

If a requested variable declares both the CF `flag_values` and `flag_meanings` attributes (as
QARTOD quality-control variables typically do, e.g. `flag_meanings="GOOD UNKNOWN SUSPECT FAIL MISSING"`),
the plugin fetches that information from `{baseUrl}/info/{datasetID}/index.json` and attaches Grafana
value mappings to the field: `1` renders as "GOOD" (green), `3` as "SUSPECT" (orange), and so on. The
underlying values remain numeric, so the field can still be plotted as a time series; tables, stat
panels, and state timelines display the mapped text and color instead of the raw number. This metadata
is cached per datasource instance for one hour. If the metadata can't be fetched or parsed, the query
still succeeds — the field is just left without mappings.
```

- [ ] **Step 2: Add a CHANGELOG entry**

In `CHANGELOG.md`, add a new bullet to the existing "## 1.0.0 (Unreleased)" list (after the last
existing bullet):

```markdown
- Add Grafana value mappings for variables with CF `flag_values`/`flag_meanings` attributes (e.g.
  QARTOD quality flags), so panels display the flag meaning and a conventional color instead of the raw
  numeric code.
```

- [ ] **Step 3: Verify the docs render sensibly**

Run: `grep -A8 "Quality-flag value mappings" README.md && grep -A2 "flag_values" CHANGELOG.md`
Expected: both new blocks print back exactly as written above, with no broken Markdown (matching
heading level, list indentation).

- [ ] **Step 4: Commit**

```bash
export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH"
but commit flag-meanings-mappings -m "Document CF flag_meanings value mappings" --changes <ids from `but diff`>
```
