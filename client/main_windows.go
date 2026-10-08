//go:build windows

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"

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
	// Run the same driver initialization as the application without opening WebView2
	// or creating an adapter. A report file works for GUI-subsystem executables in CI.
	if len(os.Args) > 1 && os.Args[1] == "--check-driver" {
		os.Exit(checkDriverCommand(os.Args[2:]))
	}
	release, err := service.AcquireInstance()
	if err != nil {
		showError(err)
		return
	}
	defer release()
	app := newApp()
	err = wails.Run(&options.App{
		Title: "WireGuard Manager · 现场连接", Width: 1180, Height: 820, MinWidth: 850, MinHeight: 700,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 246, G: 248, B: 250, A: 255},
		OnStartup:        app.startup, OnBeforeClose: app.beforeClose, OnShutdown: app.shutdown, Bind: []interface{}{app},
		Windows: &windows.Options{WebviewIsTransparent: false, WindowIsTranslucent: false},
	})
	if err != nil {
		showError(err)
	}
}

func checkDriverCommand(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: WireguardManagerDesktop.exe --check-driver <report.json>")
		return 2
	}
	check, err := service.CheckDriver()
	report := struct {
		service.DriverCheck
		Error     string `json:"error,omitempty"`
		ServerURL string `json:"serverURL"`
	}{DriverCheck: check}
	report.ServerURL, _ = deploymentServerURL()
	if err != nil {
		report.Error = err.Error()
	}
	data, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return 2
	}
	if writeErr := os.WriteFile(args[0], data, 0600); writeErr != nil {
		fmt.Fprintln(os.Stderr, writeErr)
		return 2
	}
	if err != nil {
		return 1
	}
	return 0
}
