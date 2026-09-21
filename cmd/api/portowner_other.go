//go:build !windows

package main

import "os/exec"

// portOwners lists the processes listening on port. lsof ships with macOS and
// with most Linux distributions; when it is missing the caller falls back to a
// generic message.
func portOwners(port string) []portOccupant {
	output, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return nil
	}
	return parseLsofOwners(string(output))
}
