//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Validate the final PE icon via the real Windows loader and create/remove a
// native tray (no tunnel or network changes). Used by the Windows release gate.
func checkShellCommand(args []string) int {
	if len(args) != 1 {
		return 2
	}
	report := struct {
		IconLoaded  bool   `json:"iconLoaded"`
		TrayReady   bool   `json:"trayReady"`
		TrayStopped bool   `json:"trayStopped"`
		Error       string `json:"error,omitempty"`
	}{}
	check := func() error {
		instance, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		user := windows.NewLazySystemDLL("user32.dll")
		icon, _, err := user.NewProc("LoadImageW").Call(instance, 3, 1, 32, 32, 0)
		if icon == 0 {
			return fmt.Errorf("load embedded application icon: %w", err)
		}
		defer user.NewProc("DestroyIcon").Call(icon)
		report.IconLoaded = true
		noop := func() {}
		tray := startSystemTray(trayActions{Show: noop, Disconnect: noop, Quit: noop})
		defer tray.stop()
		select {
		case <-tray.ready:
			report.TrayReady = true
		case <-time.After(10 * time.Second):
			return errors.New("native tray initialization timed out")
		}
		tray.stop()
		select {
		case <-tray.done:
			report.TrayStopped = true
		case <-time.After(5 * time.Second):
			return errors.New("native tray cleanup timed out")
		}
		return nil
	}
	if err := check(); err != nil {
		report.Error = err.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return 2
	}
	if err = os.WriteFile(args[0], data, 0600); err != nil {
		return 2
	}
	if report.Error != "" {
		return 1
	}
	return 0
}
