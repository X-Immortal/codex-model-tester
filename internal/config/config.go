package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func generateProxyAPIKey() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate proxy API key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

type Config struct {
	ListenAddr       string
	DataDir          string
	UpstreamProxy    string
	ProxyAPIKey      string
	OpenBrowser      bool
	DebugLogPayloads bool
	DefaultModel     string
	CodexBaseURL     string
	AuthIssuer       string
	OAuthClientID    string
	LoginTimeout     time.Duration
	ContinuationTTL  time.Duration
	RequestTimeout   time.Duration
	RefreshSkew      time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	if !filepath.IsAbs(dataDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve cwd: %w", err)
		}
		dataDir = filepath.Join(cwd, dataDir)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber <= 0 || portNumber > 65535 {
		return Config{}, fmt.Errorf("PORT must be a valid TCP port")
	}
	debugLogPayloads := false
	if raw := strings.TrimSpace(os.Getenv("DEBUG_LOG_PAYLOADS")); raw != "" {
		var err error
		debugLogPayloads, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("DEBUG_LOG_PAYLOADS must be a boolean")
		}
	}
	openBrowser := true
	if raw := strings.TrimSpace(os.Getenv("OPEN_BROWSER")); raw != "" {
		var err error
		openBrowser, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("OPEN_BROWSER must be a boolean")
		}
	}
	proxyAPIKey := strings.TrimSpace(os.Getenv("PROXY_API_KEY"))
	if proxyAPIKey == "" {
		proxyAPIKey, err = generateProxyAPIKey()
		if err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		ListenAddr:       ":" + strconv.Itoa(portNumber),
		DataDir:          dataDir,
		UpstreamProxy:    resolveUpstreamProxy(),
		ProxyAPIKey:      proxyAPIKey,
		OpenBrowser:      openBrowser,
		DebugLogPayloads: debugLogPayloads,
		DefaultModel:     "gpt-6-astra",
		CodexBaseURL:     "https://chatgpt.com/backend-api",
		AuthIssuer:       "https://auth.openai.com",
		OAuthClientID:    "app_EMoamEEZ73f0CkXaXp7hrann",
		LoginTimeout:     15 * time.Minute,
		ContinuationTTL:  time.Hour,
		RequestTimeout:   30 * time.Minute,
		RefreshSkew:      time.Minute,
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	return cfg, nil
}
