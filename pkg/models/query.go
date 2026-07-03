package models

import (
	"encoding/json"
	"errors"
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

	if qm.DatasetID == "" {
		return nil, errors.New("datasetId is required")
	}

	if qm.Variables == "" {
		return nil, errors.New("variables is required")
	}

	return &qm, nil
}
