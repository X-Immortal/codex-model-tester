//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// showFatalError surfaces a startup failure to a user who launched the .app
// from Finder and has no console to read.
func showFatalError(title string, err error) {
	command := exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display alert (item 1 of argv) message (item 2 of argv) as critical giving up after 180`,
		"-e", "end run",
		"--", title, fmt.Sprint(err),
	)
	_ = command.Run()
}
