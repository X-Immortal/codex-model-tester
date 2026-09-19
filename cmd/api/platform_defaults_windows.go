//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

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
