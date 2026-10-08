//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"wireguardmanager/client/service"
)

type App struct {
	mu           sync.Mutex
	ctx          context.Context
	manager      *service.Manager
	backend      *service.WindowsBackend
	store        service.Store
	profiles     []service.Profile
	startupError error
}

func newApp() *App { return &App{} }
func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx = ctx
	dir, err := os.UserConfigDir()
	if err != nil {
		a.startupError = err
		return
	}
	dir = filepath.Join(dir, "WireguardManagerDesktop")
	a.store = service.Store{Path: filepath.Join(dir, "profiles.dpapi"), Cipher: service.DPAPI{}}
	a.profiles, err = a.store.Load()
	if err != nil {
		a.startupError = err
		return
	}
	a.backend, err = service.NewWindowsBackend(dir)
	if err != nil {
		a.startupError = err
		return
	}
	a.manager = service.NewManager(a.backend)
}
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager != nil {
		if err := a.manager.Disconnect(); err != nil {
			wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{Type: wailsruntime.ErrorDialog, Title: "清理未完成", Message: "尚未恢复局域网卡状态，请保持网卡启用后再次关闭。\n" + err.Error()})
			return true
		}
	}
	return false
}
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager != nil {
		_ = a.manager.Disconnect()
	}
}
func (a *App) ready() error {
	if a.startupError != nil {
		return a.startupError
	}
	if a.manager == nil {
		return errors.New("客户端正在初始化")
	}
	return nil
}
func (a *App) Profiles() ([]service.ProfileView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return nil, err
	}
	result := make([]service.ProfileView, 0, len(a.profiles))
	for _, p := range a.profiles {
		result = append(result, p.View())
	}
	return result, nil
}
func (a *App) ImportConfig() ([]service.ProfileView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return nil, err
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "导入现场 WireGuard 配置", Filters: []wailsruntime.FileFilter{{DisplayName: "WireGuard 配置 (*.conf)", Pattern: "*.conf"}}})
	if err != nil {
		return nil, err
	}
	if len(a.profiles)+len(paths) > 100 {
		return nil, errors.New("最多保存 100 个现场配置")
	}
	next := append([]service.Profile{}, a.profiles...)
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil {
			return nil, errors.New("无法读取所选配置文件")
		}
		if stat.Size() > 64*1024 {
			return nil, errors.New("配置文件过大")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("无法读取所选配置文件")
		}
		cfg, err := service.ParseConfig(string(raw))
		clear(raw)
		if err != nil {
			return nil, err
		}
		for _, p := range next {
			if p.Config.PrivateKey == cfg.PrivateKey {
				return nil, errors.New("此设备配置已导入，请直接选择已有现场")
			}
		}
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return nil, err
		}
		next = append(next, service.Profile{ID: hex.EncodeToString(id[:]), Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Config: cfg})
	}
	if len(paths) > 0 {
		if err = a.store.Save(next); err != nil {
			return nil, err
		}
		a.profiles = next
	}
	result := make([]service.ProfileView, 0, len(a.profiles))
	for _, p := range a.profiles {
		result = append(result, p.View())
	}
	return result, nil
}
func (a *App) SaveProfile(id, name, targets, adapterID string) (service.ProfileView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return service.ProfileView{}, err
	}
	if a.manager.Active() == id {
		return service.ProfileView{}, errors.New("请先断开再修改当前连接")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return service.ProfileView{}, errors.New("现场名称应为 1–80 个字符")
	}
	prefixes, err := service.ParsePrefixes(targets)
	if err != nil {
		return service.ProfileView{}, err
	}
	for i, p := range a.profiles {
		if p.ID == id {
			p.Name = name
			p.Targets = service.JoinPrefixes(prefixes)
			p.AdapterID = adapterID
			if _, err = p.Routes(); err != nil {
				return service.ProfileView{}, err
			}
			next := append([]service.Profile{}, a.profiles...)
			next[i] = p
			if err = a.store.Save(next); err != nil {
				return service.ProfileView{}, err
			}
			a.profiles = next
			return p.View(), nil
		}
	}
	return service.ProfileView{}, errors.New("现场不存在")
}
func (a *App) RemoveProfile(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	if a.manager.Active() == id {
		return errors.New("请先断开当前连接")
	}
	next := make([]service.Profile, 0, len(a.profiles))
	for _, p := range a.profiles {
		if p.ID != id {
			next = append(next, p)
		}
	}
	if err := a.store.Save(next); err != nil {
		return err
	}
	a.profiles = next
	return nil
}
func (a *App) Adapters() ([]service.AdapterInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.backend.Adapters()
}
func (a *App) Connect(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	for _, p := range a.profiles {
		if p.ID == id {
			ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
			defer cancel()
			return a.manager.Connect(ctx, p)
		}
	}
	return errors.New("请先选择现场")
}
func (a *App) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	return a.manager.Disconnect()
}
func (a *App) Status() (service.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return service.Status{}, err
	}
	return a.manager.Status(a.ctx), nil
}
