//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"wireguardmanager/client/cloud"
	"wireguardmanager/client/service"
)

type App struct {
	mu           sync.RWMutex
	ctx          context.Context
	desktop      *service.Desktop
	startupError error
}

func newApp() *App { return &App{} }
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
	if a.startupError != nil {
		return a.startupError
	}
	if a.desktop == nil {
		return errors.New("客户端正在初始化")
	}
	return nil
}
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.desktop != nil {
		if err := a.desktop.Disconnect(); err != nil {
			wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{Type: wailsruntime.ErrorDialog, Title: "网络恢复未完成", Message: "请保持原网卡启用后再次关闭。\n" + err.Error()})
			return true
		}
	}
	return false
}
func (a *App) shutdown(context.Context) {
	a.mu.RLock()
	defer a.mu.RUnlock()
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
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Login(a.ctx, email, password, remember)
}
func (a *App) Logout() (service.DesktopView, error) {
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
func (a *App) SaveDevice(id, lans, targets string) (service.DesktopView, error) {
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.SaveDevice(a.ctx, id, lans, targets)
}
func (a *App) Connect(id, lans, targets string) (service.DesktopView, error) {
	if err := a.ready(); err != nil {
		return service.DesktopView{}, err
	}
	return a.desktop.Connect(a.ctx, id, lans, targets)
}
func (a *App) Disconnect() error {
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
