//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireApplicationLock keeps one Windows application instance alive. Windows
// releases a byte-range lock when the owning process exits, so a crashed app
// cannot leave a stale lock behind — the same guarantee flock gives on macOS.
func acquireApplicationLock(dataDir string) (release func(), alreadyRunning bool, err error) {
	lockPath := filepath.Join(dataDir, ".application.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false, fmt.Errorf("open application lock: %w", err)
	}

	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if err == nil {
		return func() {
			_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
			_ = file.Close()
		}, false, nil
	}

	_ = file.Close()
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return func() {}, true, nil
	}
	return func() {}, false, fmt.Errorf("lock application instance: %w", err)
}
