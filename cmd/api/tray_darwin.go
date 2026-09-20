//go:build darwin

package main

import (
	"context"
	_ "embed"
	"log/slog"
	"sync"

	"github.com/gogpu/systray"
)

//go:embed tray_icon_template.png
var trayIconTemplatePNG []byte

// waitForApplicationExit keeps the process alive as a macOS menu bar
// (NSStatusItem) application. systray locks the main OS thread in its package
// init, so Run must be reached from the main goroutine, which main() does.
func waitForApplicationExit(ctx context.Context, requestShutdown context.CancelFunc, url string, logger *slog.Logger) error {
	tray := systray.New()
	menu := systray.NewMenu()

	openUI := func() {
		if err := openBrowser(url); err != nil {
			logger.Warn("open admin UI from menu bar failed", "url", url, "error", err)
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

	// macOS renders a template image in the correct color for the current menu
	// bar appearance, so the icon stays legible in light and dark mode.
	tray.SetTemplateIcon(trayIconTemplatePNG).
		SetTooltip("Codex Backend Model Tester").
		SetMenu(menu).
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
