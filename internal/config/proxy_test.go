package config

import "testing"

func TestProxyFromSystemValues(t *testing.T) {
	values := parseSystemProxyOutput(`<dictionary> {
  HTTPEnable : 1
  HTTPProxy : 127.0.0.1
  HTTPPort : 7897
  HTTPSEnable : 1
  HTTPSProxy : 127.0.0.1
  HTTPSPort : 7897
  SOCKSEnable : 1
  SOCKSProxy : 127.0.0.1
  SOCKSPort : 7897
}`)
	if got := proxyFromSystemValues(values); got != "http://127.0.0.1:7897" {
		t.Fatalf("proxyFromSystemValues() = %q, want HTTP proxy", got)
	}
}

func TestProxyFromSystemValuesFallsBackToSOCKS(t *testing.T) {
	values := map[string]string{
		"SOCKSEnable": "1",
		"SOCKSProxy":  "127.0.0.1",
		"SOCKSPort":   "1080",
	}
	if got := proxyFromSystemValues(values); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyFromSystemValues() = %q, want SOCKS5 proxy", got)
	}
}

func TestResolveUpstreamProxyPrefersExplicitValue(t *testing.T) {
	t.Setenv("UPSTREAM_PROXY", "http://proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://other.example:8080")
	if got := resolveUpstreamProxy(); got != "http://proxy.example:8080" {
		t.Fatalf("resolveUpstreamProxy() = %q, want explicit proxy", got)
	}
}
