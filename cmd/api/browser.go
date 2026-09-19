package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func adminUIURL(listenAddr string) (string, error) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("parse listen address %q: %w", listenAddr, err)
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + "/", nil
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		if runningInWSL() {
			command = exec.Command("cmd.exe", "/c", "start", "", url)
		} else {
			command = exec.Command("xdg-open", url)
		}
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func runningInWSL() bool {
	if strings.TrimSpace(os.Getenv("WSL_INTEROP")) != "" || strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" {
		return true
	}
	version, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(version)), "microsoft")
}
