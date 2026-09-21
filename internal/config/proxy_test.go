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

func TestProxyFromWindowsServerSingleProxy(t *testing.T) {
	if got := proxyFromWindowsServer("127.0.0.1:7897"); got != "http://127.0.0.1:7897" {
		t.Fatalf("proxyFromWindowsServer() = %q, want single HTTP proxy", got)
	}
}

func TestProxyFromWindowsServerProtocolList(t *testing.T) {
	if got := proxyFromWindowsServer("http=127.0.0.1:8080; https=127.0.0.1:7897; socks=127.0.0.1:1080"); got != "http://127.0.0.1:7897" {
		t.Fatalf("proxyFromWindowsServer() = %q, want HTTPS destination proxy", got)
	}
}

func TestProxyFromWindowsServerFallsBackToHTTP(t *testing.T) {
	if got := proxyFromWindowsServer("http=proxy.example:8080;socks=127.0.0.1:1080"); got != "http://proxy.example:8080" {
		t.Fatalf("proxyFromWindowsServer() = %q, want HTTP proxy", got)
	}
}

func TestProxyFromWindowsServerAcceptsNamedEntriesOnly(t *testing.T) {
	if got := proxyFromWindowsServer("https=127.0.0.1:7897"); got != "http://127.0.0.1:7897" {
		t.Fatalf("proxyFromWindowsServer() = %q, want HTTPS entry", got)
	}
	if got := proxyFromWindowsServer("HTTPS=127.0.0.1:7897; HTTP=127.0.0.1:8080"); got != "http://127.0.0.1:7897" {
		t.Fatalf("proxyFromWindowsServer() = %q, want HTTPS entry from uppercase tokens", got)
	}
}

func TestProxyFromWindowsServerFallsBackToSOCKS(t *testing.T) {
	if got := proxyFromWindowsServer("socks=127.0.0.1:1080"); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyFromWindowsServer() = %q, want SOCKS5 proxy", got)
	}
	if got := proxyFromWindowsServer("socks=socks5://127.0.0.1:1080"); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyFromWindowsServer() = %q, want SOCKS5 proxy scheme preserved", got)
	}
}

func TestProxyFromWindowsServerEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t"} {
		if got := proxyFromWindowsServer(raw); got != "" {
			t.Fatalf("proxyFromWindowsServer(%q) = %q, want no proxy", raw, got)
		}
	}
}

func TestProxyFromWindowsServerKeepsSingleProxyVerbatim(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:7897", "http://127.0.0.1:7897"} {
		want := "http://127.0.0.1:7897"
		if got := proxyFromWindowsServer(raw); got != want {
			t.Fatalf("proxyFromWindowsServer(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestProxyFromWindowsServerKeepsCredentialsContainingEquals(t *testing.T) {
	if got := proxyFromWindowsServer("user:p=ss@127.0.0.1:7897"); got != "http://user:p=ss@127.0.0.1:7897" {
		t.Fatalf("proxyFromWindowsServer() = %q, want single proxy with credentials", got)
	}
}

func TestProxyFromWindowsServerIgnoresIrrelevantProtocolList(t *testing.T) {
	for _, raw := range []string{"ftp=127.0.0.1:21", "file=127.0.0.1:80", "socks="} {
		if got := proxyFromWindowsServer(raw); got != "" {
			t.Fatalf("proxyFromWindowsServer(%q) = %q, want no proxy", raw, got)
		}
	}
}
