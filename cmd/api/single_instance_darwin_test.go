//go:build darwin

package main

import "testing"

func TestAcquireApplicationLock(t *testing.T) {
	dataDir := t.TempDir()

	releaseFirst, alreadyRunning, err := acquireApplicationLock(dataDir)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	if alreadyRunning {
		t.Fatal("first lock reported an existing instance")
	}

	releaseSecond, alreadyRunning, err := acquireApplicationLock(dataDir)
	if err != nil {
		t.Fatalf("acquire second lock: %v", err)
	}
	if !alreadyRunning {
		t.Fatal("second lock did not report an existing instance")
	}
	releaseSecond()

	releaseFirst()
	releaseThird, alreadyRunning, err := acquireApplicationLock(dataDir)
	if err != nil {
		t.Fatalf("acquire lock after release: %v", err)
	}
	defer releaseThird()
	if alreadyRunning {
		t.Fatal("lock remained held after release")
	}
}
