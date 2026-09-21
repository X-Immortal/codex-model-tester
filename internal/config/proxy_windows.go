//go:build windows

package config

import "golang.org/x/sys/windows/registry"

const windowsInternetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// detectSystemProxy reads the per-user WinINET settings used by Windows and
// by applications such as Clash Verge when System Proxy mode is enabled.
func detectSystemProxy() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return proxyFromWindowsServer(server)
}
