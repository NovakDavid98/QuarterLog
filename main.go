package main

import (
	"context"
	"embed"
	"fmt"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"quarterlog/internal/config"
	"quarterlog/internal/winutil"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

func main() {
	if !winutil.SingleInstance("Quarterlog-Single-Instance-Mutex") {
		fmt.Println("Quarterlog is already running.")
		return
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Quarterlog",
		Width:            popupW,
		Height:           popupH,
		DisableResize:    true,
		Frameless:        true,
		AlwaysOnTop:      true,
		StartHidden:      true,
		HideWindowOnClose: true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer:      &assetserver.Options{Assets: assets},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windows.Acrylic,
			DisableWindowIcon:    true,
			Theme:                windows.SystemDefault,
		},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
			go systray.Run(func() { onTrayReady(ctx, app) }, func() {})
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// onTrayReady builds the system-tray icon and menu.
func onTrayReady(ctx context.Context, app *App) {
	systray.SetIcon(trayIcon)
	systray.SetTitle("Quarterlog")
	systray.SetTooltip("Quarterlog — worklog assistant")

	mLogNow := systray.AddMenuItem("Log now", "Capture the current quarter and log it now")
	mReview := systray.AddMenuItem("Review queue", "Show pending intervals")
	systray.AddSeparator()
	mPause := systray.AddMenuItemCheckbox("Pause capturing", "Stop taking screenshots", config.Current().Paused)
	mSettings := systray.AddMenuItem("Settings…", "Configure Quarterlog")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit Quarterlog")

	// Wire the tray -> app updates.
	app.trayUpdate = func(count int) {
		if count > 0 {
			mReview.SetTitle(fmt.Sprintf("Review queue (%d)", count))
			systray.SetTooltip(fmt.Sprintf("Quarterlog — %d pending", count))
		} else {
			mReview.SetTitle("Review queue")
			systray.SetTooltip("Quarterlog — worklog assistant")
		}
	}
	app.updateTrayTitle()

	// Tray click handlers run on the tray's message-loop thread, so any real
	// work (screen capture, window ops) is dispatched to a goroutine to keep the
	// menu responsive.
	mLogNow.Click(func() { go app.CaptureNow() })
	mReview.Click(func() { go app.showLarge("queue") })
	mSettings.Click(func() { go app.showLarge("settings") })
	mPause.Click(func() {
		paused := !mPause.Checked()
		if paused {
			mPause.Check()
		} else {
			mPause.Uncheck()
		}
		go func() { _ = app.SetPaused(paused) }()
	})
	mQuit.Click(func() {
		go func() {
			systray.Quit()
			wrt.Quit(ctx)
		}()
	})
}
