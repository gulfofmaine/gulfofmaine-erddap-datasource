package plugin

import (
	"io"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"
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

// flagAttrs accumulates one variable's flag_values/flag_meanings attribute
// values as the info table is scanned; the two arrive on separate rows and
// are only useful together.
type flagAttrs struct {
	values   string
	meanings string
}

// flagAttrsFor returns byVariable[variable], creating the entry on first use.
func flagAttrsFor(byVariable map[string]*flagAttrs, variable string) *flagAttrs {
	fa, ok := byVariable[variable]
	if !ok {
		fa = &flagAttrs{}
		byVariable[variable] = fa
	}
	return fa
}

// buildFlagMappings zips each variable's collected flag_values/flag_meanings
// into a Grafana value mapping. Variables missing either attribute, or whose
// two lists disagree in length, are omitted rather than erroring.
func buildFlagMappings(byVariable map[string]*flagAttrs) map[string]data.ValueMappings {
	mappings := map[string]data.ValueMappings{}
	for variable, fa := range byVariable {
		vm, ok := buildFlagMapping(fa.values, fa.meanings)
		if !ok {
			continue
		}
		mappings[variable] = vm
	}
	return mappings
}

// parseInfoJSON decodes an ERDDAP dataset info .../index.json response body
// and builds value mappings for every variable that declares both the
// flag_values and flag_meanings CF attributes. Variables without both
// attributes, or with mismatched flag_values/flag_meanings counts, are
// omitted from the result rather than erroring.
//
// This is the flag-mapping view of parseDatasetInfo, which parses the same
// document once for both the mappings and the dataset's variable list.
func parseInfoJSON(r io.Reader) (map[string]data.ValueMappings, error) {
	info, err := parseDatasetInfo(r)
	if err != nil {
		return nil, err
	}
	return info.Mappings, nil
}
