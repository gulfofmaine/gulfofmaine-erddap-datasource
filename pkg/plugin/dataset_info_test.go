package plugin

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// datasetInfoBody is a trimmed-down but structurally faithful ERDDAP
// .../info/<id>/index.json response: the five-column table ERDDAP emits, with
// a global-attribute row, three variables in declaration order, and one
// variable carrying CF flag attributes.
const datasetInfoBody = `{
	"table": {
		"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
		"columnTypes": ["String", "String", "String", "String", "String"],
		"rows": [
			["attribute", "NC_GLOBAL", "cdm_data_type", "String", "TimeSeries"],
			["attribute", "NC_GLOBAL", "units", "String", "not-a-variable"],
			["variable", "time", "", "double", ""],
			["attribute", "time", "units", "String", "seconds since 1970-01-01T00:00:00Z"],
			["attribute", "time", "long_name", "String", "Time"],
			["variable", "mean_temp", "", "float", ""],
			["attribute", "mean_temp", "units", "String", "celsius"],
			["attribute", "mean_temp", "long_name", "String", "Mean Temperature"],
			["variable", "mean_temp_qc", "", "int", ""],
			["attribute", "mean_temp_qc", "long_name", "String", "Mean Temperature QC"],
			["attribute", "mean_temp_qc", "flag_values", "long", "1, 2, 3, 4, 9"],
			["attribute", "mean_temp_qc", "flag_meanings", "String", "GOOD UNKNOWN SUSPECT FAIL MISSING"]
		]
	}
}`

func TestParseDatasetInfo(t *testing.T) {
	info, err := parseDatasetInfo(strings.NewReader(datasetInfoBody))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []erddapVariable{
		{Name: "time", Type: "double", Units: "seconds since 1970-01-01T00:00:00Z", LongName: "Time"},
		{Name: "mean_temp", Type: "float", Units: "celsius", LongName: "Mean Temperature"},
		{Name: "mean_temp_qc", Type: "int", Units: "", LongName: "Mean Temperature QC"},
	}

	if len(info.Variables) != len(want) {
		t.Fatalf("got %d variables (%+v), want %d", len(info.Variables), info.Variables, len(want))
	}
	for i, w := range want {
		if info.Variables[i] != w {
			t.Errorf("Variables[%d] = %+v, want %+v", i, info.Variables[i], w)
		}
	}

	// Flag mappings must still come out of the same single pass.
	vm, ok := info.Mappings["mean_temp_qc"]
	if !ok {
		t.Fatalf("expected a flag mapping for mean_temp_qc, got %+v", info.Mappings)
	}
	mapper, ok := vm[0].(data.ValueMapper)
	if !ok {
		t.Fatalf("expected a data.ValueMapper, got %T", vm[0])
	}
	if mapper["3"].Text != "SUSPECT" {
		t.Errorf("mapping for 3 = %+v, want text SUSPECT", mapper["3"])
	}
	if _, ok := info.Mappings["mean_temp"]; ok {
		t.Error("expected no mapping for mean_temp (no flag attributes)")
	}
}

func TestParseDatasetInfoNoGlobalVariable(t *testing.T) {
	info, err := parseDatasetInfo(strings.NewReader(datasetInfoBody))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, v := range info.Variables {
		if v.Name == "NC_GLOBAL" {
			t.Fatal("NC_GLOBAL must not appear as a variable")
		}
	}
}

func TestParseDatasetInfoMissingColumns(t *testing.T) {
	tests := []struct {
		name        string
		columnNames string
	}{
		{"missing Row Type", `["Variable Name", "Attribute Name", "Data Type", "Value"]`},
		{"missing Data Type", `["Row Type", "Variable Name", "Attribute Name", "Value"]`},
		{"missing Variable Name", `["Row Type", "Attribute Name", "Data Type", "Value"]`},
		{"missing Attribute Name", `["Row Type", "Variable Name", "Data Type", "Value"]`},
		{"missing Value", `["Row Type", "Variable Name", "Attribute Name", "Data Type"]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"table": {"columnNames": ` + tc.columnNames + `, "rows": []}}`
			if _, err := parseDatasetInfo(strings.NewReader(body)); err == nil {
				t.Fatal("expected an error for missing expected columns")
			}
		})
	}
}

func TestParseDatasetInfoRaggedRow(t *testing.T) {
	body := `{
		"table": {
			"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
			"rows": [
				["variable", "ok", "", "float", ""],
				["variable", "short"],
				[],
				["attribute", "ok", "units"],
				["attribute", "ok", "long_name", "String", "OK"]
			]
		}
	}`

	info, err := parseDatasetInfo(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.Variables) != 1 {
		t.Fatalf("expected the ragged rows to be skipped, got %+v", info.Variables)
	}
	if info.Variables[0].LongName != "OK" {
		t.Errorf("Variables[0] = %+v, want LongName OK", info.Variables[0])
	}
}

func TestParseDatasetInfoNoVariables(t *testing.T) {
	body := `{
		"table": {
			"columnNames": ["Row Type", "Variable Name", "Attribute Name", "Data Type", "Value"],
			"rows": [
				["attribute", "NC_GLOBAL", "title", "String", "Some dataset"]
			]
		}
	}`

	info, err := parseDatasetInfo(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.Variables) != 0 {
		t.Fatalf("expected no variables, got %+v", info.Variables)
	}
	if info.Mappings == nil {
		t.Error("expected a non-nil (empty) mappings map")
	}
}

func TestParseDatasetInfoMalformedJSON(t *testing.T) {
	if _, err := parseDatasetInfo(strings.NewReader("not json")); err == nil {
		t.Fatal("expected an error for a malformed body")
	}
}
