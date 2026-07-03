package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/gulfofmaine/erddap/pkg/models"
)

func TestBuildTabledapURL(t *testing.T) {
	tr := backend.TimeRange{
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name    string
		baseURL string
		qm      models.QueryModel
		want    string
		wantErr bool
	}{
		{
			name:    "trailing slash base URL",
			baseURL: "https://example.com/erddap/",
			qm: models.QueryModel{
				DatasetID: "M01_sbe37_all",
				Variables: "temperature",
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
		{
			name:    "no trailing slash base URL",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID: "M01_sbe37_all",
				Variables: "temperature",
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
		{
			name:    "whitespace heavy variable list",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID: "M01_sbe37_all",
				Variables: " temperature, salinity ",
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature,salinity&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
		{
			name:    "user typed time is de-duped",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID: "M01_sbe37_all",
				Variables: "time, temperature",
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
		{
			name:    "constraints are escaped and appended",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID:   "M01_sbe37_all",
				Variables:   "temperature",
				Constraints: `station="A01"&depth<2`,
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z&station=%22A01%22&depth%3C2",
		},
		{
			name:    "empty constraints are omitted",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID:   "M01_sbe37_all",
				Variables:   "temperature",
				Constraints: "",
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
		{
			name:    "literal ampersand inside quoted constraint value is escaped, not a separator",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID:   "M01_sbe37_all",
				Variables:   "temperature",
				Constraints: `station="A&B"`,
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z&station=%22A%26B%22",
		},
		{
			name:    "literal ampersand inside quoted value followed by a real constraint separator",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID:   "M01_sbe37_all",
				Variables:   "temperature",
				Constraints: `station="A&B"&depth<2`,
			},
			want: "https://example.com/erddap/tabledap/M01_sbe37_all.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z&station=%22A%26B%22&depth%3C2",
		},
		{
			name:    "datasetId with slash and percent is path-escaped, not path-cleaned",
			baseURL: "https://example.com/erddap",
			qm: models.QueryModel{
				DatasetID: "bad/../id%",
				Variables: "temperature",
			},
			want: "https://example.com/erddap/tabledap/bad%2F..%2Fid%25.json?time,temperature&time%3E=2024-01-01T00:00:00Z&time%3C=2024-01-02T00:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildTabledapURL(tc.baseURL, tc.qm, tr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (url=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("buildTabledapURL() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

func TestEscapeERDDAP(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"space", "a b", "a%20b"},
		{"double quotes", `"A01"`, "%22A01%22"},
		{"angle brackets", "<>", "%3C%3E"},
		{"unicode", "café", "caf%C3%A9"},
		{"unreserved chars untouched", "AZaz09-_.~", "AZaz09-_.~"},
		{"structural chars untouched", "&,=!():/", "&,=!():/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeERDDAP(tc.in)
			if got != tc.want {
				t.Errorf("escapeERDDAP(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEscapeERDDAPConstraints(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "existing unquoted case is unchanged",
			in:   `station="A01"&depth<2`,
			want: "station=%22A01%22&depth%3C2",
		},
		{
			name: "literal ampersand inside quotes is escaped",
			in:   `station="A&B"`,
			want: "station=%22A%26B%22",
		},
		{
			name: "literal ampersand inside quotes followed by a real separator",
			in:   `station="A&B"&depth<2`,
			want: "station=%22A%26B%22&depth%3C2",
		},
		{
			name: "comma and parens inside quotes are escaped",
			in:   `name="A,(B)"`,
			want: "name=%22A%2C%28B%29%22",
		},
		{
			name: "structural chars outside quotes stay literal",
			in:   `a=1&b=2,c`,
			want: "a=1&b=2,c",
		},
		{
			name: "backslash-escaped quote inside a value does not toggle quote state",
			in:   `name="A\"&B"`,
			want: `name=%22A%5C%22%26B%22`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeERDDAPConstraints(tc.in)
			if got != tc.want {
				t.Errorf("escapeERDDAPConstraints(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseTableJSON(t *testing.T) {
	body := `{
		"table": {
			"columnNames": ["time", "temperature", "salinity"],
			"columnTypes": ["String", "double", "double"],
			"columnUnits": ["UTC", "degree_C", ""],
			"rows": [
				["2024-01-01T02:00:00Z", 9.1, 35.0],
				["2024-01-01T00:00:00Z", 8.2, null],
				["2024-01-01T01:00:00Z", 8.5, 34.9]
			]
		}
	}`

	frame, err := parseTableJSON(strings.NewReader(body), "response")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(frame.Fields))
	}

	timeField := frame.Fields[0]
	if timeField.Name != "time" {
		t.Errorf("expected first field name 'time', got %q", timeField.Name)
	}
	if timeField.Len() != 3 {
		t.Fatalf("expected 3 rows, got %d", timeField.Len())
	}

	wantTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC),
	}
	for i, want := range wantTimes {
		got, ok := timeField.At(i).(time.Time)
		if !ok {
			t.Fatalf("time field row %d is not a time.Time (got %T)", i, timeField.At(i))
		}
		if !got.Equal(want) {
			t.Errorf("time field row %d = %v, want %v", i, got, want)
		}
	}

	tempField := frame.Fields[1]
	if tempField.Name != "temperature" {
		t.Errorf("expected field name 'temperature', got %q", tempField.Name)
	}
	if tempField.Config == nil || tempField.Config.Unit != "degree_C" {
		t.Errorf("expected temperature Config.Unit = degree_C, got %+v", tempField.Config)
	}
	wantTemp := []float64{8.2, 8.5, 9.1}
	for i, want := range wantTemp {
		got, ok := tempField.At(i).(*float64)
		if !ok || got == nil {
			t.Fatalf("temperature row %d is nil or wrong type (got %T)", i, tempField.At(i))
		}
		if *got != want {
			t.Errorf("temperature row %d = %v, want %v", i, *got, want)
		}
	}

	salField := frame.Fields[2]
	if salField.Name != "salinity" {
		t.Errorf("expected field name 'salinity', got %q", salField.Name)
	}
	if salField.Config != nil {
		t.Errorf("expected no Config for salinity (empty unit), got %+v", salField.Config)
	}
	// row 0 (00:00, sorted first) had a null salinity cell in the source payload.
	got0, ok := salField.At(0).(*float64)
	if !ok {
		t.Fatalf("salinity row 0 wrong type (got %T)", salField.At(0))
	}
	if got0 != nil {
		t.Errorf("expected nil salinity at row 0, got %v", *got0)
	}
	got1, ok := salField.At(1).(*float64)
	if !ok || got1 == nil || *got1 != 34.9 {
		t.Errorf("expected salinity row 1 = 34.9, got %v", got1)
	}
}

func TestSortRowsByTime(t *testing.T) {
	rows := []erddapRow{
		{time: time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC)},
		{time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{time: time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)},
	}

	sortRowsByTime(rows)

	want := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC),
	}
	for i, w := range want {
		if !rows[i].time.Equal(w) {
			t.Errorf("rows[%d].time = %v, want %v", i, rows[i].time, w)
		}
	}
}
