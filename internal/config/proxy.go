package config

import (
	"net/url"
	"os"
	"strings"
)

// resolveUpstreamProxy chooses an explicit application setting first, then
// conventional proxy environment variables, and finally the platform's
// configured system proxy.
func resolveUpstreamProxy() string {
	for _, name := range []string{
		"UPSTREAM_PROXY",
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if value := normalizeProxyURL(os.Getenv(name)); value != "" {
			return value
		}
	}
	return detectSystemProxy()
}

func normalizeProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func parseSystemProxyOutput(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" && value != "" {
			values[key] = value
		}
	}
	return values
}

func proxyFromSystemValues(values map[string]string) string {
	for _, candidate := range []struct {
		enabled string
		host    string
		port    string
		scheme  string
	}{
		{enabled: "HTTPSEnable", host: "HTTPSProxy", port: "HTTPSPort", scheme: "http"},
		{enabled: "HTTPEnable", host: "HTTPProxy", port: "HTTPPort", scheme: "http"},
		{enabled: "SOCKSEnable", host: "SOCKSProxy", port: "SOCKSPort", scheme: "socks5"},
	} {
		if values[candidate.enabled] != "1" {
			continue
		}
		host := strings.TrimSpace(values[candidate.host])
		port := strings.TrimSpace(values[candidate.port])
		if host == "" || port == "" {
			continue
		}
		return candidate.scheme + "://" + host + ":" + port
	}
	return ""
}
