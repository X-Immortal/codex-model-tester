package main

import (
	"bufio"
	"context"
	"io"
	"strings"
)

func desktopParentShutdownContext(parent context.Context, input io.Reader, enabled bool) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if !enabled {
		return ctx, cancel
	}
	go func() {
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			if strings.EqualFold(strings.TrimSpace(scanner.Text()), "shutdown") {
				cancel()
				return
			}
		}
		cancel()
	}()
	return ctx, cancel
}
