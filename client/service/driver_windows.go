//go:build windows

package service

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
)

// DriverCheck describes DLL readiness. It does not install a driver or create a
// tunnel, so the packaged executable can run this check before any configuration.
type DriverCheck struct {
	ResourceBytes int    `json:"resourceBytes"`
	DLLVersion    string `json:"dllVersion"`
	APICallable   bool   `json:"apiCallable"`
}

func CheckDriver() (check DriverCheck, err error) {
	// Use exactly the lookup used by the official embedded loader, including the
	// lowercase query. Windows uppercases the query, so the PE name must be uppercase.
	resource, err := windows.FindResource(0, "wireguard.dll", windows.RT_RCDATA)
	if err != nil {
		return check, fmt.Errorf("无法查找内置 WireGuardNT 资源：%w；请下载修复后的客户端", err)
	}
	data, err := windows.LoadResourceData(0, resource)
	if err != nil {
		return check, fmt.Errorf("读取内置 WireGuardNT 资源失败：%w", err)
	}
	check.ResourceBytes = len(data)
	if len(data) == 0 {
		return check, fmt.Errorf("内置 WireGuardNT 资源为空")
	}
	check.DLLVersion, err = probeDriverAPI(driver.RunningVersion, driver.Version)
	check.APICallable = err == nil
	return check, err
}

func probeDriverAPI(runningVersion func() (uint32, error), dllVersion func() string) (version string, err error) {
	// The official lazy loader panics on DLL loading / missing exports. Keep that
	// concrete cause rather than reducing every failure to Version() == "unknown".
	defer func() {
		if cause := recover(); cause != nil {
			if loadErr, ok := cause.(error); ok {
				err = fmt.Errorf("加载 WireGuardNT 动态库失败：%w", loadErr)
			} else {
				err = fmt.Errorf("加载 WireGuardNT 动态库失败：%v", cause)
			}
		}
	}()
	// RunningVersion calls the DLL's query API. An ordinary returned error is
	// expected on first run when the kernel driver is not installed yet; it still
	// proves DLL loading and symbol resolution succeeded. Adapter creation installs
	// the kernel driver later. This query does not alter the machine's networking.
	_, _ = runningVersion()
	// Version metadata is informational and may be unavailable independently of
	// whether the library's API can be called.
	return dllVersion(), nil
}
