package config

import (
	"encoding/base64"
	"path/filepath"
	"testing"
)

func TestLoadGeneratesProxyAPIKeyWhenMissing(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "")
	t.Chdir(t.TempDir())

	first, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.ProxyAPIKey)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("generated proxy API key is not a 32-byte base64url secret: len=%d err=%v", len(decoded), err)
	}
	second, err := Load()
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if first.ProxyAPIKey == second.ProxyAPIKey {
		t.Fatal("separate config loads generated the same proxy API key")
	}
}

func TestLoadPreservesConfiguredProxyAPIKey(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "configured-key")
	t.Chdir(t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProxyAPIKey != "configured-key" {
		t.Fatalf("Load() proxy API key = %q, want configured-key", cfg.ProxyAPIKey)
	}
}

func TestLoadBuildsListenAddrAndDataDir(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "test-key")

	tests := []struct {
		name       string
		env        map[string]string
		wantListen string
		wantData   string
	}{
		{
			name:       "defaults",
			wantListen: ":8080",
			wantData:   "data",
		},
		{
			name: "data dir override",
			env: map[string]string{
				"DATA_DIR": "custom-data",
			},
			wantListen: ":8080",
			wantData:   "custom-data",
		},
		{
			name: "port override",
			env: map[string]string{
				"PORT": "9090",
			},
			wantListen: ":9090",
			wantData:   "data",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			t.Chdir(cwd)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.ListenAddr != tc.wantListen {
				t.Fatalf("Load() listen addr = %q, want %q", cfg.ListenAddr, tc.wantListen)
			}
			if cfg.DefaultModel != "gpt-6-astra" {
				t.Fatalf("Load() default model = %q, want gpt-6-astra", cfg.DefaultModel)
			}
			wantDataDir := filepath.Join(cwd, tc.wantData)
			if cfg.DataDir != wantDataDir {
				t.Fatalf("Load() data dir = %q, want %q", cfg.DataDir, wantDataDir)
			}
		})
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("PROXY_API_KEY", "test-key")
	t.Setenv("PORT", "not-a-port")
	t.Chdir(t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid PORT error")
	}
}

func TestLoadParsesDebugLogPayloads(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantDebug bool
		wantErr   bool
	}{
		{name: "true", value: "true", wantDebug: true},
		{name: "invalid", value: "definitely-not-bool", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PROXY_API_KEY", "test-key")
			t.Setenv("DEBUG_LOG_PAYLOADS", tc.value)
			t.Chdir(t.TempDir())

			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want invalid DEBUG_LOG_PAYLOADS error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.DebugLogPayloads != tc.wantDebug {
				t.Fatalf("Load() debug log payloads = %v, want %v", cfg.DebugLogPayloads, tc.wantDebug)
			}
		})
	}
}

func TestLoadParsesOpenBrowser(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantOpen bool
		wantErr  bool
	}{
		{name: "defaults to true", wantOpen: true},
		{name: "can disable", value: "false", wantOpen: false},
		{name: "invalid", value: "sometimes", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PROXY_API_KEY", "test-key")
			t.Setenv("OPEN_BROWSER", tc.value)
			t.Chdir(t.TempDir())

			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want invalid OPEN_BROWSER error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.OpenBrowser != tc.wantOpen {
				t.Fatalf("Load() open browser = %v, want %v", cfg.OpenBrowser, tc.wantOpen)
			}
		})
	}
}
