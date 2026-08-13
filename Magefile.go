//go:build mage
// +build mage

package main

import (
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"

	// mage:import
	build "github.com/grafana/grafana-plugin-sdk-go/build"
)

// Default configures the default target.
var Default = build.BuildAll

// TestJUnit runs backend tests via gotestsum, producing a coverage profile and a JUnit report.
func TestJUnit() error {
	if err := os.MkdirAll(filepath.Join(".", "coverage"), os.ModePerm); err != nil {
		return err
	}
	return sh.RunV("go", "run", "gotest.tools/gotestsum@v1.13.0",
		"--junitfile", "coverage/backend-junit.xml",
		"--", "./pkg/...", "-coverpkg", "./...", "-cover", "-coverprofile=coverage/backend.out")
}
