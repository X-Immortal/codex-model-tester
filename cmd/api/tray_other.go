//go:build !windows && !darwin

package main

import (
	"context"
	"log/slog"
)

func waitForApplicationExit(ctx context.Context, _ context.CancelFunc, _ string, _ *slog.Logger) error {
	<-ctx.Done()
	return nil
}
