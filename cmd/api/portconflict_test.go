package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatgpt-codex-proxy/internal/server"
)

func TestParseLsofOwners(t *testing.T) {
	output := "p1234\ncnginx\np5678\ncnode"
	want := []portOccupant{{PID: 1234, Name: "nginx"}, {PID: 5678, Name: "node"}}

	owners := parseLsofOwners(output)
	if len(owners) != len(want) {
		t.Fatalf("parseLsofOwners() = %v, want %v", owners, want)
	}
	for index := range want {
		if owners[index] != want[index] {
			t.Fatalf("owner %d = %v, want %v", index, owners[index], want[index])
		}
	}
}

func TestParseLsofOwnersWithoutNames(t *testing.T) {
	owners := parseLsofOwners("p1234\n")
	if len(owners) != 1 || owners[0].PID != 1234 || owners[0].Name != "" {
		t.Fatalf("parseLsofOwners() = %v, want a single PID record", owners)
	}
}

func TestParseNetstatListeners(t *testing.T) {
	output := `
活动连接

  协议  本地地址          外部地址        状态           PID
  TCP    0.0.0.0:8080           0.0.0.0:0              LISTENING       1234
  TCP    127.0.0.1:8080         127.0.0.1:51422        ESTABLISHED     4321
  TCP    [::]:8080              [::]:0                 LISTENING       1234
  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       900
`
	pids := parseNetstatListeners(output, "8080")
	if len(pids) != 1 || pids[0] != 1234 {
		t.Fatalf("parseNetstatListeners() = %v, want [1234]", pids)
	}
}

func TestParseNetstatListenersIgnoresLocalizedHeaders(t *testing.T) {
	output := "  协议  本地地址          外部地址        状态           PID\n"
	if pids := parseNetstatListeners(output, "8080"); len(pids) != 0 {
		t.Fatalf("parseNetstatListeners() = %v, want no listeners", pids)
	}
}

func TestParseTasklistCSV(t *testing.T) {
	output := "\"nginx.exe\",\"1234\",\"Console\",\"1\",\"5,000 K\"\r\n" +
		"\"Codex-Model-Tester-windows-x64.exe\",\"5678\",\"Console\",\"1\",\"120,000 K\"\r\n"

	names := parseTasklistCSV(output)
	if names[1234] != "nginx.exe" {
		t.Fatalf("names[1234] = %q, want nginx.exe", names[1234])
	}
	if names[5678] != "Codex-Model-Tester-windows-x64.exe" {
		t.Fatalf("names[5678] = %q, want the application image name", names[5678])
	}
}

func TestPortConflictMessageNamesOccupantAndSuggestions(t *testing.T) {
	message := portConflictMessage("8080", []portOccupant{{PID: 4242, Name: "nginx.exe"}})
	for _, want := range []string{"8080", "nginx.exe", "PID 4242", "PORT=8081", "http://127.0.0.1:8080/"} {
		if !strings.Contains(message, want) {
			t.Fatalf("portConflictMessage() = %q, missing %q", message, want)
		}
	}
}

func TestPortConflictMessageWithoutOccupant(t *testing.T) {
	message := portConflictMessage("8080", nil)
	if !strings.Contains(message, "未能识别") {
		t.Fatalf("portConflictMessage() = %q, want an unknown-occupant hint", message)
	}
}

func TestSuggestedPort(t *testing.T) {
	cases := map[string]string{
		"8080":  "8081",
		"1":     "2",
		"65535": "8081",
		"":      "8081",
		"web":   "8081",
	}
	for port, want := range cases {
		if got := suggestedPort(port); got != want {
			t.Fatalf("suggestedPort(%q) = %q, want %q", port, got, want)
		}
	}
}

func TestListenPort(t *testing.T) {
	port, err := listenPort(":8080")
	if err != nil || port != "8080" {
		t.Fatalf("listenPort(\":8080\") = %q, %v", port, err)
	}
	if _, err := listenPort("8080"); err == nil {
		t.Fatal("listenPort(\"8080\") = nil error, want a parse failure")
	}
}

func TestBackendIdentityAtRecognizesOwnBackend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": server.ServiceIdentity})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if !backendIdentityAt(srv.URL) {
		t.Fatal("backendIdentityAt() = false, want true for this application's liveness payload")
	}
}

func TestBackendIdentityAtRecognizesLegacyBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		_, _ = w.Write([]byte("<html><head>" + adminUITitleMarker + "</head></html>"))
	}))
	defer srv.Close()

	if !backendIdentityAt(srv.URL) {
		t.Fatal("backendIdentityAt() = false, want true for a build without a service marker")
	}
}

func TestBackendIdentityAtRejectsOtherService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if backendIdentityAt(srv.URL) {
		t.Fatal("backendIdentityAt() = true, want false for an unrelated service")
	}
}

func TestBackendIdentityAtRejectsClosedPort(t *testing.T) {
	if backendIdentityAt("http://127.0.0.1:1") {
		t.Fatal("backendIdentityAt() = true, want false when nothing is listening")
	}
}

func TestRunningBackendOnPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": server.ServiceIdentity})
	}))
	defer srv.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}
	if !runningBackendOnPort(port) {
		t.Fatalf("runningBackendOnPort(%q) = false, want true", port)
	}
}

func TestPortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	if !portInUse(port) {
		t.Fatalf("portInUse(%q) = false, want true for a listening port", port)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if portInUse(port) {
		t.Fatalf("portInUse(%q) = true, want false after the listener closed", port)
	}
}

func TestIsOwnProcessName(t *testing.T) {
	if len(os.Args) == 0 {
		t.Skip("no process arguments available")
	}
	if !isOwnProcessName(filepath.Base(os.Args[0])) {
		t.Fatalf("isOwnProcessName(%q) = false, want true", filepath.Base(os.Args[0]))
	}
	for _, name := range []string{"", "   ", "nginx.exe"} {
		if isOwnProcessName(name) {
			t.Fatalf("isOwnProcessName(%q) = true, want false", name)
		}
	}
}

func TestAllOwnProcesses(t *testing.T) {
	if !allOwnProcesses(nil) {
		t.Fatal("allOwnProcesses(nil) = false, want true")
	}
	own := filepath.Base(os.Args[0])
	if !allOwnProcesses([]portOccupant{{PID: 1, Name: own}}) {
		t.Fatal("allOwnProcesses() = false for the current executable")
	}
	if allOwnProcesses([]portOccupant{{PID: 1, Name: own}, {PID: 2, Name: "nginx.exe"}}) {
		t.Fatal("allOwnProcesses() = true with a foreign process present")
	}
}
