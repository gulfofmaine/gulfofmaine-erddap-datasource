package models

import (
	"encoding/json"
	"errors"
	"strings"
)

type QueryModel struct {
	DatasetID   string `json:"datasetId"`
	Variables   string `json:"variables"`
	Constraints string `json:"constraints"`
}

func LoadQueryModel(raw json.RawMessage) (*QueryModel, error) {
	qm := QueryModel{}
	err := json.Unmarshal(raw, &qm)
	if err != nil {
		return nil, err
	}

	// TrimSpace before checking for emptiness (rather than requiring
	// qm.DatasetID/Variables == "") so a whitespace-only value is rejected
	// the same way the frontend's filterQuery gate (src/datasource.ts)
	// already rejects it before ever sending a query.
	if strings.TrimSpace(qm.DatasetID) == "" {
		return nil, errors.New("datasetId is required")
	}

	if strings.TrimSpace(qm.Variables) == "" {
		return nil, errors.New("variables is required")
	}

	return &qm, nil
}
