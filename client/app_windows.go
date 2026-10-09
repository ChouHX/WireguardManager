//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"wireguardmanager/client/cloud"
	"wireguardmanager/client/service"
)

type App struct {
	mu           sync.RWMutex
	operations   sync.Mutex
	ctx          context.Context
	desktop      *service.Desktop
	startupError error
	tray         *systemTray
	exiting      atomic.Bool
	quitPending  atomic.Bool
	layoutMu     sync.Mutex
	wideLayout   bool
}

func newApp() *App { return &App{} }

// SetWindowLayout follows authentication transitions, not device refreshes.
// This also returns to the compact login window after a session expires.
func (a *App) SetWindowLayout(authenticated bool) {
	a.layoutMu.Lock()
	defer a.layoutMu.Unlock()
	if a.wideLayout == authenticated {
		return
	}
	a.wideLayout = authenticated
	wailsruntime.WindowUnmaximise(a.ctx)
	if authenticated {
		wailsruntime.WindowSetMinSize(a.ctx, 850, 600)
		wailsruntime.WindowSetSize(a.ctx, 1000, 660)
	} else {
		wailsruntime.WindowSetMinSize(a.ctx, 380, 440)
		wailsruntime.WindowSetSize(a.ctx, 420, 480)
	}
	wailsruntime.WindowCenter(a.ctx)
}
func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx = ctx
	server, err := deploymentServerURL()
	if err != nil {
		a.startupError = err
		return
	}
	api, err := cloud.New(server)
	if err != nil {
		a.startupError = err
		return
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		a.startupError = err
		return
	}
	dir = filepath.Join(dir, "WireguardManagerDesktop")
	backend, err := service.NewWindowsBackend(dir)
	if err != nil {
		a.startupError = err
		return
	}
	a.desktop, a.startupError = service.NewDesktop(api, backend, service.CloudStore{Path: filepath.Join(dir, "cloud.dpapi"), Cipher: service.DPAPI{}})
}
func (a *App) ready() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.quitPending.Load() {
		return errors.New("客户端正在退出")
	}
	if a.startupError != nil {
		return a.startupError
	}
	if a.desktop == nil {
		return errors.New("客户端正在初始化")
	}
	return nil
}
func (a *App) domReady(ctx context.Context) {
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return
	}
	a.tray = startSystemTray(trayActions{
		Show: a.showWindow,
		Disconnect: func() {
			if err := a.Disconnect(); err != nil {
				a.notifyError(err)
				return
			}
			wailsruntime.EventsEmit(ctx, "desktop:notice", "连接已断开，网络设置已恢复")
		},
		Quit: func() {
			if err := a.Quit(); err != nil {
				a.notifyError(err)
			}
		},
	})
	a.mu.Unlock()
}
func (a *App) showWindow() {
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
}
func (a *App) notifyError(err error) {
	a.showWindow()
	wailsruntime.EventsEmit(a.ctx, "desktop:error", err.Error())
}

// Quit is the explicit exit action. A normal window close keeps the tunnel alive.
func (a *App) Quit() error {
	if !a.quitPending.CompareAndSwap(false, true) {
		return nil
	}
	a.operations.Lock()
	defer a.operations.Unlock()
	a.mu.RLock()
	desktop := a.desktop
	a.mu.RUnlock()
	if desktop != nil {
		if err := desktop.Disconnect(); err != nil {
			a.quitPending.Store(false)
			return err
		}
	}
	a.exiting.Store(true)
	wailsruntime.Quit(a.ctx)
	return nil
}
func (a *App) beforeClose(ctx context.Context) bool {
	if a.exiting.Load() {
		return false
	}
	a.mu.RLock()
	available := a.tray.available()
	a.mu.RUnlock()
	if available {
		wailsruntime.WindowHide(ctx)
		return true
	}
	// Never hide an inaccessible app when tray creation failed.
	go func() {
		if err := a.Quit(); err != nil {
			a.notifyError(err)
		}
	}()
	return true
}
func (a *App) shutdown(context.Context) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	a.tray.stop()
	if a.desktop != nil {
		_ = a.desktop.Disconnect()
	}
}
func (a *App) Bootstrap() (service.DesktopView, error) {
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Bootstrap(a.ctx)
}
func (a *App) Login(email, password string, remember bool) (service.DesktopView, error) {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Login(a.ctx, email, password, remember)
}
func (a *App) Logout() (service.DesktopView, error) {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Logout()
}
func (a *App) Refresh() (service.DesktopView, error) {
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Refresh(a.ctx)
}
func (a *App) Snapshot() (service.DesktopView, error) {
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Snapshot(), nil
}
func (a *App) DetectLANs() (service.LANDetection, error) {
	if err := a.ready(); err != nil {
		return service.LANDetection{}, err
	}
	return a.desktop.DetectLANs()
}
func (a *App) SaveDevice(id, targets string) (service.DesktopView, error) {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.SaveDevice(a.ctx, id, targets)
}
func (a *App) Connect(id, targets string) (service.DesktopView, error) {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Connect(a.ctx, id, targets)
}
func (a *App) Disconnect() error {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	return a.desktop.Disconnect()
}
func (a *App) Status() (service.Status, error) {
	if err := a.ready(); err != nil {
		return service.Status{}, err
	}
	return a.desktop.Status(a.ctx), nil
}

func (a *App) DownloadGatewaySetup(id string) (string, error) {
	a.operations.Lock()
	defer a.operations.Unlock()
	if err := a.ready(); err != nil {
		return "", err
	}
	script, err := a.desktop.GatewaySetup(a.ctx, id)
	if err != nil {
		return "", err
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{Title: "保存网关首次接入脚本", DefaultFilename: "wireguard-gateway-" + id + ".sh", Filters: []wailsruntime.FileFilter{{DisplayName: "Shell 脚本", Pattern: "*.sh"}}})
	if err != nil || path == "" {
		return "", err
	}
	if err = service.AtomicWrite(path, []byte(script)); err != nil {
		return "", err
	}
	return "网关接入脚本已保存；将它复制到目标网关并以 root 执行一次", nil
}
