package plugin

import (
	"encoding/json"
	"errors"
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
