//go:build !darwin && !windows

package main

func acquireApplicationLock(_ string) (release func(), alreadyRunning bool, err error) {
	return func() {}, false, nil
}
