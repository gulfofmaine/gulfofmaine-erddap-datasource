package models

import (
	"encoding/json"
	"testing"
)

func TestLoadQueryModel_ParsesAllFields(t *testing.T) {
	raw := json.RawMessage(`{"datasetId": "M01_sbe37_all", "variables": "temperature, salinity", "constraints": "depth<2&station=\"A01\""}`)

	qm, err := LoadQueryModel(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if qm.DatasetID != "M01_sbe37_all" {
		t.Errorf("expected DatasetID %q, got %q", "M01_sbe37_all", qm.DatasetID)
	}
	if qm.Variables != "temperature, salinity" {
		t.Errorf("expected Variables %q, got %q", "temperature, salinity", qm.Variables)
	}
	if qm.Constraints != `depth<2&station="A01"` {
		t.Errorf("expected Constraints %q, got %q", `depth<2&station="A01"`, qm.Constraints)
	}
}

func TestLoadQueryModel_MissingDatasetID(t *testing.T) {
	raw := json.RawMessage(`{"datasetId": "", "variables": "temperature"}`)

	_, err := LoadQueryModel(raw)
	if err == nil {
		t.Fatal("expected error for empty datasetId, got nil")
	}
}

func TestLoadQueryModel_MissingVariables(t *testing.T) {
	raw := json.RawMessage(`{"datasetId": "M01_sbe37_all", "variables": ""}`)

	_, err := LoadQueryModel(raw)
	if err == nil {
		t.Fatal("expected error for empty variables, got nil")
	}
}

func TestLoadQueryModel_WhitespaceOnlyDatasetID(t *testing.T) {
	raw := json.RawMessage(`{"datasetId": "   ", "variables": "temperature"}`)

	_, err := LoadQueryModel(raw)
	if err == nil {
		t.Fatal("expected error for whitespace-only datasetId, got nil")
	}
}

func TestLoadQueryModel_WhitespaceOnlyVariables(t *testing.T) {
	raw := json.RawMessage(`{"datasetId": "M01_sbe37_all", "variables": "   "}`)

	_, err := LoadQueryModel(raw)
	if err == nil {
		t.Fatal("expected error for whitespace-only variables, got nil")
	}
}
