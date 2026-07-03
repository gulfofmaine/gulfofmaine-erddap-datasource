package models

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestLoadPluginSettings_TrimsTrailingSlash(t *testing.T) {
	settings, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		JSONData: []byte(`{"baseUrl": "https://erddap.example.com/erddap/"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if settings.BaseURL != "https://erddap.example.com/erddap" {
		t.Errorf("expected trailing slash trimmed, got %q", settings.BaseURL)
	}
}

func TestLoadPluginSettings_InvalidJSON(t *testing.T) {
	_, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		JSONData: []byte(`{invalid`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
