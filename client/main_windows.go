//go:build windows

package main

import (
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	win "golang.org/x/sys/windows"
	"wireguardmanager/client/service"
)

//go:embed all:frontend/dist
var assets embed.FS

func showError(err error) {
	title, _ := win.UTF16PtrFromString("WireGuard Manager")
	body, _ := win.UTF16PtrFromString(err.Error())
	win.MessageBox(0, body, title, win.MB_OK|win.MB_ICONERROR)
}
func main() {
	release, err := service.AcquireInstance()
	if err != nil {
		showError(err)
		return
	}
	defer release()
	app := newApp()
	err = wails.Run(&options.App{
		Title: "WireGuard Manager · 现场连接", Width: 1060, Height: 820, MinWidth: 850, MinHeight: 700,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 13, G: 20, B: 32, A: 255},
		OnStartup:        app.startup, OnBeforeClose: app.beforeClose, OnShutdown: app.shutdown, Bind: []interface{}{app},
		Windows: &windows.Options{WebviewIsTransparent: false, WindowIsTranslucent: false},
	})
	if err != nil {
		showError(err)
	}
}
