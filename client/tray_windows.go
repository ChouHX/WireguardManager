//go:build windows

package main

import (
	_ "embed"
	"runtime"
	"sync"

	"github.com/energye/systray"
)

//go:embed build/windows/icon.ico
var applicationIcon []byte

type trayActions struct{ Show, Disconnect, Quit func() }

type systemTray struct {
	ready    chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// A separate, pinned OS thread owns both the tray window and its message loop.
// The Wails window keeps its own loop; neither loop blocks the other.
func startSystemTray(actions trayActions) *systemTray {
	t := &systemTray{ready: make(chan struct{}), done: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(t.done)
		systray.Run(func() {
			systray.SetIcon(applicationIcon)
			systray.SetTooltip("WireGuard Manager · 双击打开")
			systray.AddMenuItem("打开主窗口", "").Click(func() { go actions.Show() })
			systray.AddMenuItem("断开当前连接", "").Click(func() { go actions.Disconnect() })
			systray.AddSeparator()
			systray.AddMenuItem("退出", "断开连接并恢复网络后退出").Click(func() { go actions.Quit() })
			systray.SetOnDClick(func(systray.IMenu) { go actions.Show() })
			close(t.ready)
		}, func() {})
	}()
	return t
}
func (t *systemTray) available() bool {
	if t == nil {
		return false
	}
	select {
	case <-t.done:
		return false
	default:
	}
	select {
	case <-t.ready:
		return true
	default:
		return false
	}
}
func (t *systemTray) stop() {
	if t != nil {
		t.stopOnce.Do(systray.Quit)
	}
}
