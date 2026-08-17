package plugin

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Additional ERDDAP info response column names (see erddapInfoResponse in
// flag_mappings.go for the response shape and the remaining column names).
const (
	infoColRowType  = "Row Type"
	infoColDataType = "Data Type"
)

// infoRowTypeVariable is the Row Type value marking a variable *declaration*
// row, e.g. ["variable", "mean_temp", "", "float", ""]. Every other row is an
// "attribute" row attached to a variable (or to NC_GLOBAL, the dataset-level
// pseudo-variable, which never gets a declaration row of its own).
const infoRowTypeVariable = "variable"

// CF/ACDD attribute names carrying human-facing variable metadata.
const (
	unitsAttr    = "units"
	longNameAttr = "long_name"
)

// erddapVariable is one variable declared by a dataset, with the display
// metadata ERDDAP attaches to it as attributes.
type erddapVariable struct {
	Name     string
	Type     string
	Units    string
	LongName string
}

// datasetInfo is everything this plugin reads out of a dataset's
// .../info/<datasetID>/index.json document: the variable list (for the query
// editor's variable picker) and the flag_values/flag_meanings-derived value
// mappings (for rendering QC flag fields). Both come from the same document,
// so they are parsed together in one pass and cached as a unit.
type datasetInfo struct {
	Variables []erddapVariable
	Mappings  map[string]data.ValueMappings
}

// parseDatasetInfo decodes an ERDDAP dataset info .../index.json response
// body in a single pass, collecting both the declared variables (in dataset
// declaration order, which is the order ERDDAP lists them) and the CF flag
// value mappings.
//
// Variable rows seed the list; later attribute rows patch the matching
// variable's Units/LongName by name. An attribute naming a variable that was
// never declared (most commonly NC_GLOBAL) is ignored rather than inventing a
// variable for it. Ragged rows shorter than the widest column index the
// parser needs are skipped defensively.
func parseDatasetInfo(r io.Reader) (*datasetInfo, error) {
	var resp erddapInfoResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}

	rowTypeIdx, varIdx, attrIdx, dataTypeIdx, valIdx := -1, -1, -1, -1, -1
	for i, name := range resp.Table.ColumnNames {
		switch name {
		case infoColRowType:
			rowTypeIdx = i
		case infoColVariableName:
			varIdx = i
		case infoColAttributeName:
			attrIdx = i
		case infoColDataType:
			dataTypeIdx = i
		case infoColValue:
			valIdx = i
		}
	}
	if rowTypeIdx == -1 || varIdx == -1 || attrIdx == -1 || dataTypeIdx == -1 || valIdx == -1 {
		return nil, errors.New("erddap: info response missing expected columns")
	}

	maxIdx := max(rowTypeIdx, varIdx, attrIdx, dataTypeIdx, valIdx)

	info := &datasetInfo{Variables: []erddapVariable{}}
	// indexOf maps a declared variable's name to its slice index in
	// info.Variables, so attribute rows can patch it in place without
	// disturbing declaration order.
	indexOf := map[string]int{}
	flags := map[string]*flagAttrs{}

	for _, row := range resp.Table.Rows {
		if maxIdx >= len(row) {
			continue // defensive against a ragged row
		}

		variable := row[varIdx]

		if row[rowTypeIdx] == infoRowTypeVariable {
			if _, seen := indexOf[variable]; seen {
				continue
			}
			indexOf[variable] = len(info.Variables)
			info.Variables = append(info.Variables, erddapVariable{
				Name: variable,
				Type: row[dataTypeIdx],
			})
			continue
		}

		switch row[attrIdx] {
		case unitsAttr, longNameAttr:
			i, ok := indexOf[variable]
			if !ok {
				continue // an attribute of something that isn't a variable
			}
			if row[attrIdx] == unitsAttr {
				info.Variables[i].Units = row[valIdx]
			} else {
				info.Variables[i].LongName = row[valIdx]
			}
		case flagValuesAttr:
			flagAttrsFor(flags, variable).values = row[valIdx]
		case flagMeaningsAttr:
			flagAttrsFor(flags, variable).meanings = row[valIdx]
		}
	}

	info.Mappings = buildFlagMappings(flags)

	return info, nil
}
