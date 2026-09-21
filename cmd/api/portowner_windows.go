//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps helper tools from flashing a console window, which is
// visible in the windowsgui build of this application.
const createNoWindow = 0x08000000

func runHidden(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	output, err := command.Output()
	return string(output), err
}

// portOwners lists the processes listening on port using netstat plus tasklist.
func portOwners(port string) []portOccupant {
	netstatOutput, err := runHidden("netstat", "-ano", "-p", "tcp")
	if err != nil {
		return nil
	}
	pids := parseNetstatListeners(netstatOutput, port)
	if len(pids) == 0 {
		return nil
	}

	names := map[int]string{}
	if tasklistOutput, tasklistErr := runHidden("tasklist", "/FO", "CSV", "/NH"); tasklistErr == nil {
		names = parseTasklistCSV(tasklistOutput)
	}

	owners := make([]portOccupant, 0, len(pids))
	for _, pid := range pids {
		owners = append(owners, portOccupant{PID: pid, Name: names[pid]})
	}
	return owners
}
