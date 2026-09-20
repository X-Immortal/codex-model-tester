//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPlatformDefaultsSetsDataDir(t *testing.T) {
	t.Setenv("DATA_DIR", "")

	applyPlatformDefaults()

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("UserConfigDir() error = %v", err)
	}
	want := filepath.Join(configDir, "Codex Backend Model Tester", "data")
	if got := os.Getenv("DATA_DIR"); got != want {
		t.Fatalf("DATA_DIR = %q, want %q", got, want)
	}
	// A .app launched from Finder runs with "/" as its working directory, so
	// the default must be absolute to stay writable.
	if !filepath.IsAbs(want) || !strings.HasPrefix(want, "/") {
		t.Fatalf("default DATA_DIR %q is not an absolute path", want)
	}
}

func TestApplyPlatformDefaultsKeepsExplicitDataDir(t *testing.T) {
	t.Setenv("DATA_DIR", "/tmp/explicit-codex-data")

	applyPlatformDefaults()

	if got := os.Getenv("DATA_DIR"); got != "/tmp/explicit-codex-data" {
		t.Fatalf("DATA_DIR = %q, want it left untouched", got)
	}
}
