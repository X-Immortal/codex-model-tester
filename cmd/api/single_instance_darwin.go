//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireApplicationLock keeps one bundled macOS application instance alive.
// flock locks are released automatically by the kernel when the process exits,
// so a crashed app cannot leave a stale lock behind.
func acquireApplicationLock(dataDir string) (release func(), alreadyRunning bool, err error) {
	lockPath := filepath.Join(dataDir, ".application.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false, fmt.Errorf("open application lock: %w", err)
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return func() {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
		}, false, nil
	}

	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return func() {}, true, nil
	}
	return func() {}, false, fmt.Errorf("lock application instance: %w", err)
}
