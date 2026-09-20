//go:build darwin

package config

import "os/exec"

func detectSystemProxy() string {
	output, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return ""
	}
	return proxyFromSystemValues(parseSystemProxyOutput(string(output)))
}
