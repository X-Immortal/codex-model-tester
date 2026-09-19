package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDesktopParentShutdownContextStopsOnCommand(t *testing.T) {
	ctx, cancel := desktopParentShutdownContext(context.Background(), strings.NewReader("shutdown\n"), true)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("desktop shutdown command did not cancel context")
	}
}

func TestDesktopParentShutdownContextStopsOnParentPipeClose(t *testing.T) {
	ctx, cancel := desktopParentShutdownContext(context.Background(), strings.NewReader(""), true)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("desktop parent pipe close did not cancel context")
	}
}

func TestDesktopParentShutdownContextDisabled(t *testing.T) {
	parent, stopParent := context.WithCancel(context.Background())
	ctx, cancel := desktopParentShutdownContext(parent, strings.NewReader("shutdown\n"), false)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("disabled desktop shutdown listener canceled context")
	case <-time.After(25 * time.Millisecond):
	}
	stopParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not propagate")
	}
}
