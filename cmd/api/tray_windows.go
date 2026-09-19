//go:build windows

package main

import (
	"context"
	_ "embed"
	"log/slog"
	"sync"

	"github.com/gogpu/systray"
)

//go:embed tray_icon.png
var trayIconPNG []byte

func waitForApplicationExit(ctx context.Context, requestShutdown context.CancelFunc, url string, logger *slog.Logger) error {
	tray := systray.New()
	menu := systray.NewMenu()

	openUI := func() {
		if err := openBrowser(url); err != nil {
			logger.Warn("open admin UI from tray failed", "url", url, "error", err)
		}
	}
	menu.Add("打开网页", openUI)
	menu.AddSeparator()

	var removeOnce sync.Once
	removeTray := func() {
		removeOnce.Do(tray.Remove)
	}
	menu.Add("退出", func() {
		requestShutdown()
		removeTray()
	})

	tray.SetIcon(trayIconPNG).
		SetTooltip("Codex Backend Model Tester").
		SetMenu(menu).
		OnDoubleClick(openUI).
		Show()

	go func() {
		<-ctx.Done()
		removeTray()
	}()

	err := tray.Run()
	requestShutdown()
	removeTray()
	return err
}
