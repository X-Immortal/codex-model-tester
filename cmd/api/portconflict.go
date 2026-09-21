package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"chatgpt-codex-proxy/internal/config"
	"chatgpt-codex-proxy/internal/server"
)

// probeTimeout bounds the loopback probes used while diagnosing a failed bind.
const probeTimeout = 800 * time.Millisecond

// adminUITitleMarker identifies builds released before the liveness payload
// carried a service name. It lets an upgraded app hand off to an instance that
// is still running from an older release.
const adminUITitleMarker = "<title>Codex 模型测试</title>"

// startupOutcome tells main how to leave the startup path.
type startupOutcome int

const (
	// startupContinue keeps starting the backend.
	startupContinue startupOutcome = iota
	// startupHandoff means another instance already serves the UI, so this
	// launch succeeded by opening it.
	startupHandoff
	// startupBlocked means the port belongs to someone else, so this launch
	// did not start a backend.
	startupBlocked
)

// portOccupant identifies a process listening on the configured port.
type portOccupant struct {
	PID  int
	Name string
}

func (o portOccupant) describe() string {
	name := strings.TrimSpace(o.Name)
	switch {
	case name != "" && o.PID > 0:
		return fmt.Sprintf("%s (PID %d)", name, o.PID)
	case name != "":
		return name
	case o.PID > 0:
		return fmt.Sprintf("PID %d", o.PID)
	default:
		return ""
	}
}

// checkPortAvailability runs before the backend binds.
//
// The loopback probe is what makes this reliable on macOS: a process that holds
// only 127.0.0.1:8080 does not stop us from binding [::]:8080, so the bind
// succeeds while the address the user browses to still belongs to that other
// process. Checking first reports the conflict instead of opening a browser
// into an unrelated program.
func checkPortAvailability(cfg config.Config, logger *slog.Logger) startupOutcome {
	port, err := listenPort(cfg.ListenAddr)
	if err != nil {
		return startupContinue
	}
	if !portInUse(port) {
		return startupContinue
	}
	return handleOccupiedPort(cfg, logger, port, nil)
}

// handleListenFailure reacts to a bind that failed even though the loopback
// port looked free, for example when another process owns the wildcard address.
func handleListenFailure(cfg config.Config, logger *slog.Logger, listenErr error) startupOutcome {
	port, err := listenPort(cfg.ListenAddr)
	if err != nil {
		return startupContinue
	}
	if !portInUse(port) {
		return startupContinue
	}
	return handleOccupiedPort(cfg, logger, port, listenErr)
}

// handleOccupiedPort either hands the user to an instance that is already
// serving this application or explains which other program owns the port.
func handleOccupiedPort(cfg config.Config, logger *slog.Logger, port string, listenErr error) startupOutcome {
	if runningBackendOnPort(port) {
		logger.Info("local service port already serves this application; opening the existing UI", "port", port)
		openExistingUI(cfg, logger)
		return startupHandoff
	}

	occupants := portOwners(port)
	if len(occupants) > 0 && allOwnProcesses(occupants) {
		logger.Info("local service port is held by another copy of this application; opening its UI", "port", port)
		openExistingUI(cfg, logger)
		return startupHandoff
	}

	fields := []any{"addr", cfg.ListenAddr, "port", port, "occupants", describeOccupants(occupants)}
	if listenErr != nil {
		fields = append(fields, "error", listenErr)
	}
	logger.Error("local service port is already in use", fields...)
	showStartupProblem("Codex Model Tester 无法启动本地服务", portConflictMessage(port, occupants))
	return startupBlocked
}

// openExistingUI points the browser at a backend that is already running.
func openExistingUI(cfg config.Config, logger *slog.Logger) {
	url, err := adminUIURL(cfg.ListenAddr)
	if err != nil {
		logger.Warn("running backend found, but its URL could not be resolved", "error", err)
		return
	}
	if err := openBrowser(url); err != nil {
		logger.Warn("running backend found, but opening its UI failed", "url", url, "error", err)
	}
}

// listenPort extracts the port from a listen address such as ":8080".
func listenPort(listenAddr string) (string, error) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("parse listen address %q", listenAddr)
	}
	return port, nil
}

// runningBackendOnPort reports whether something is already serving this
// application's UI on the loopback interface.
func runningBackendOnPort(port string) bool {
	return backendIdentityAt("http://" + net.JoinHostPort("127.0.0.1", port))
}

// backendIdentityAt reports whether baseURL belongs to this application.
func backendIdentityAt(baseURL string) bool {
	client := &http.Client{
		Timeout:   probeTimeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}

	if status, body, ok := probeLocal(client, baseURL+"/health/live", 4<<10); ok && status == http.StatusOK {
		var payload struct {
			Status  string `json:"status"`
			Service string `json:"service"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Status == "ok" && payload.Service == server.ServiceIdentity {
			return true
		}
	}

	status, body, ok := probeLocal(client, baseURL+"/", 64<<10)
	return ok && status == http.StatusOK && strings.Contains(string(body), adminUITitleMarker)
}

func probeLocal(client *http.Client, url string, limit int64) (int, []byte, bool) {
	response, err := client.Get(url)
	if err != nil {
		return 0, nil, false
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return 0, nil, false
	}
	return response.StatusCode, body, true
}

// portInUse reports whether the loopback port accepts connections, which
// distinguishes a busy port from other bind failures such as permissions.
func portInUse(port string) bool {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), probeTimeout)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// isOwnProcessName reports whether a listening process is another copy of this
// executable, which happens when two builds use different data directories.
func isOwnProcessName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(os.Args) == 0 {
		return false
	}
	return strings.EqualFold(name, filepath.Base(os.Args[0]))
}

func allOwnProcesses(occupants []portOccupant) bool {
	for _, occupant := range occupants {
		if !isOwnProcessName(occupant.Name) {
			return false
		}
	}
	return true
}

func describeOccupants(occupants []portOccupant) string {
	parts := make([]string, 0, len(occupants))
	for _, occupant := range occupants {
		if described := occupant.describe(); described != "" {
			parts = append(parts, described)
		}
	}
	return strings.Join(parts, "、")
}

// portConflictMessage is the text a non-technical user sees when another
// program owns the port.
func portConflictMessage(port string, occupants []portOccupant) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "端口 %s 已被其他程序占用，本地服务无法启动。", port)
	if described := describeOccupants(occupants); described != "" {
		fmt.Fprintf(&builder, "\n\n占用端口的程序：%s", described)
	} else {
		builder.WriteString("\n\n未能识别占用端口的程序。")
	}
	builder.WriteString("\n\n可以这样处理：")
	builder.WriteString("\n· 关闭上面的程序后，重新打开本程序；")
	fmt.Fprintf(&builder, "\n· 或让本程序改用其他端口：设置环境变量 PORT=%s 后重新启动；", suggestedPort(port))
	fmt.Fprintf(&builder, "\n· 如果本程序其实已经在运行，直接在浏览器打开 http://127.0.0.1:%s/ 。", port)
	return builder.String()
}

func suggestedPort(port string) string {
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value >= 65535 {
		return "8081"
	}
	return strconv.Itoa(value + 1)
}

// showStartupProblem reports an actionable startup problem through the same
// native dialog used for fatal errors.
func showStartupProblem(title, message string) {
	showFatalError(title, errors.New(message))
}
