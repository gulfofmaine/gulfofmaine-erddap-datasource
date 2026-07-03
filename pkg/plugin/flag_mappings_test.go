package plugin

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
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
