//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// applyPlatformDefaults keeps runtime state in the standard macOS location.
// A bundled .app is launched with "/" as its working directory, so the
// relative default would otherwise resolve to an unwritable path.
func applyPlatformDefaults() {
	if strings.TrimSpace(os.Getenv("DATA_DIR")) != "" {
		return
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	_ = os.Setenv("DATA_DIR", filepath.Join(configDir, "Codex Backend Model Tester", "data"))
}
