package server

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const windowsNotificationScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$notification = New-Object System.Windows.Forms.NotifyIcon
$notification.Icon = [System.Drawing.SystemIcons]::Warning
$notification.BalloonTipTitle = $env:CODEX_HEARTBEAT_TITLE
$notification.BalloonTipText = $env:CODEX_HEARTBEAT_MESSAGE
$notification.Visible = $true
$notification.ShowBalloonTip(8000)
Start-Sleep -Seconds 9
$notification.Dispose()
`

func sendDesktopNotification(title, message string) error {
	command, err := desktopNotificationCommand(title, message)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func desktopNotificationCommand(title, message string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "windows":
		return windowsNotificationCommand("powershell.exe", title, message), nil
	case "darwin":
		return exec.Command("osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			"--", title, message,
		), nil
	default:
		if serverRunningInWSL() {
			return windowsNotificationCommand("powershell.exe", title, message), nil
		}
		return exec.Command("notify-send", "--app-name=Codex Model Tester", title, message), nil
	}
}

func windowsNotificationCommand(binary, title, message string) *exec.Cmd {
	command := exec.Command(binary, "-NoProfile", "-NonInteractive", "-Command", windowsNotificationScript)
	command.Env = append(os.Environ(), "CODEX_HEARTBEAT_TITLE="+title, "CODEX_HEARTBEAT_MESSAGE="+message)
	return command
}

func serverRunningInWSL() bool {
	if strings.TrimSpace(os.Getenv("WSL_INTEROP")) != "" || strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" {
		return true
	}
	version, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(version)), "microsoft")
}
