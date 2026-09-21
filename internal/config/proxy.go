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

// windowsProxyTokens lists the protocol tokens WinINET accepts in the
// ProxyServer value. A value that uses one of them is a per-protocol list
// rather than a single proxy, even when the token is one this application
// never routes through (ftp, file).
var windowsProxyTokens = map[string]bool{
	"http":  true,
	"https": true,
	"ftp":   true,
	"file":  true,
	"socks": true,
}

// proxyFromWindowsServer converts the WinINET ProxyServer value into the
// single upstream proxy used by the application. Windows accepts either one
// proxy for every protocol (host:port) or a semicolon-separated list such as
// "http=host:port;https=host:port;socks=host:port". The named entries describe
// the destination protocol, so both HTTP and HTTPS entries normally point to an
// HTTP CONNECT proxy. Upstream traffic is HTTPS, so an "https" entry wins over
// an "http" entry and SOCKS is only used when neither is present.
func proxyFromWindowsServer(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	proxies, listed := parseWindowsProxyList(raw)
	if !listed {
		return normalizeProxyURL(raw)
	}
	for _, name := range []string{"https", "http"} {
		if proxy := normalizeProxyURL(proxies[name]); proxy != "" {
			return proxy
		}
	}
	if proxy := strings.TrimSpace(proxies["socks"]); proxy != "" {
		if !strings.Contains(proxy, "://") {
			proxy = "socks5://" + proxy
		}
		if normalized := normalizeProxyURL(proxy); normalized != "" {
			return normalized
		}
	}
	return ""
}

// parseWindowsProxyList splits a WinINET per-protocol list. It reports whether
// any entry used a protocol token, so a single proxy whose credentials happen
// to contain "=" is still treated as one proxy. Entries with an empty value are
// recorded as a list but contribute no proxy.
func parseWindowsProxyList(raw string) (map[string]string, bool) {
	proxies := make(map[string]string)
	listed := false
	for _, entry := range strings.Split(raw, ";") {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if !windowsProxyTokens[name] {
			continue
		}
		listed = true
		if value = strings.TrimSpace(value); value != "" {
			proxies[name] = value
		}
	}
	return proxies, listed
}
