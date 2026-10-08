//go:build windows

package service

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

const firewallGroup = "WireguardManager Desktop automatic forwarding"

type firewallRule struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Remote    string `json:"remote"`
	Direction int    `json:"direction"`
}

type comWork struct {
	fn     func() error
	result chan error
}

var comWorker struct {
	once sync.Once
	jobs chan comWork
}

// Keep one COM apartment alive for the process lifetime. Tearing down the last
// MTA between WMI calls races COM server shutdown (CO_E_SERVER_STOPPING).
// Objects are created and released inside each job, always on this thread.
func withCOM(fn func() error) error {
	comWorker.once.Do(func() {
		comWorker.jobs = make(chan comWork)
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
			var e *ole.OleError
			if errors.As(err, &e) && e.Code() == 1 {
				err = nil
			}
			if err == nil {
				defer ole.CoUninitialize()
			}
			for job := range comWorker.jobs {
				if err != nil {
					job.result <- err
				} else {
					job.result <- job.fn()
				}
			}
		}()
	})
	result := make(chan error, 1)
	comWorker.jobs <- comWork{fn, result}
	return <-result
}

var oleAutomation = windows.NewLazySystemDLL("oleaut32.dll")
var safeArrayCreateVector = oleAutomation.NewProc("SafeArrayCreateVector")
var safeArrayPutElement = oleAutomation.NewProc("SafeArrayPutElement")

// INetFwRule.Interfaces requires SAFEARRAY(VARIANT) containing BSTR elements,
// not the SAFEARRAY(BSTR) produced by go-ole's []string marshaler.
func putFirewallInterfaces(rule *ole.IDispatch, name string) error {
	array, _, _ := safeArrayCreateVector.Call(uintptr(ole.VT_VARIANT), 0, 1)
	if array == 0 {
		return errors.New("无法分配防火墙接口数组")
	}
	value := ole.NewVariant(ole.VT_ARRAY|ole.VT_VARIANT, int64(array))
	defer value.Clear()
	str := ole.SysAllocStringLen(name)
	if str == nil {
		return errors.New("无法分配防火墙接口名称")
	}
	element := ole.NewVariant(ole.VT_BSTR, int64(uintptr(unsafe.Pointer(str))))
	defer element.Clear()
	index := int32(0)
	hr, _, _ := safeArrayPutElement.Call(array, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&element)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return comPut(rule, "Interfaces", &value)
}

// Unlike oleutil.ForEach, check errors even when Next returns zero elements.
// A failed NAT query must never be mistaken for an empty list of existing NATs.
func comEach(objects *ole.IDispatch, visit func(*ole.VARIANT) error) error {
	newEnum, err := oleutil.GetProperty(objects, "_NewEnum")
	if err != nil {
		return err
	}
	defer newEnum.Clear()
	enum, err := newEnum.ToIUnknown().IEnumVARIANT(ole.IID_IEnumVariant)
	if err != nil {
		return err
	}
	defer enum.Release()
	for {
		item, count, err := enum.Next(1)
		if count == 0 {
			item.Clear()
			var e *ole.OleError
			if errors.As(err, &e) && e.Code() == 1 {
				return nil
			}
			return err
		}
		if err != nil {
			item.Clear()
			return err
		}
		err = visit(&item)
		item.Clear()
		if err != nil {
			return err
		}
	}
}
func comObject(name string) (*ole.IDispatch, error) {
	obj, err := oleutil.CreateObject(name)
	if err != nil {
		return nil, err
	}
	defer obj.Release()
	return obj.QueryInterface(ole.IID_IDispatch)
}
func comPut(obj *ole.IDispatch, name string, value any) error {
	v, err := oleutil.PutProperty(obj, name, value)
	if v != nil {
		v.Clear()
	}
	return err
}
func comCall(obj *ole.IDispatch, name string, args ...any) error {
	v, err := oleutil.CallMethod(obj, name, args...)
	if v != nil {
		v.Clear()
	}
	return err
}
func comString(obj *ole.IDispatch, name string) (string, error) {
	v, err := oleutil.GetProperty(obj, name)
	if err != nil {
		return "", err
	}
	defer v.Clear()
	return v.ToString(), nil
}
func withFirewall(fn func(*ole.IDispatch) error) error {
	return withCOM(func() error {
		policy, err := comObject("HNetCfg.FwPolicy2")
		if err != nil {
			return err
		}
		defer policy.Release()
		rules, err := oleutil.GetProperty(policy, "Rules")
		if err != nil {
			return err
		}
		defer rules.Clear()
		return fn(rules.ToIDispatch())
	})
}
func addFirewallRules(defs []firewallRule) error {
	return withFirewall(func(rules *ole.IDispatch) error {
		for _, def := range defs {
			if err := func() error {
				rule, err := comObject("HNetCfg.FWRule")
				if err != nil {
					return err
				}
				defer rule.Release()
				// Protocol before ports; addresses and interfaces restrict these rules to the
				// selected local networks and this tunnel. Never disable Windows Firewall.
				for _, prop := range []struct {
					name  string
					value any
				}{{"Name", def.Name}, {"Description", "Automatically removed when WireguardManager disconnects"}, {"Grouping", firewallGroup}, {"Protocol", 256}, {"Direction", def.Direction}, {"Action", 1}, {"Profiles", int32(0x7fffffff)}, {"RemoteAddresses", def.Remote}, {"Enabled", true}} {
					if err = comPut(rule, prop.name, prop.value); err != nil {
						return fmt.Errorf("Windows 防火墙 %s：%w", prop.name, err)
					}
				}
				if err = putFirewallInterfaces(rule, def.Interface); err != nil {
					return fmt.Errorf("Windows 防火墙 Interfaces：%w", err)
				}
				return comCall(rules, "Add", rule)
			}(); err != nil {
				return err
			}
		}
		return nil
	})
}
func removeFirewallRules(defs []firewallRule) error {
	if len(defs) == 0 {
		return nil
	}
	return withFirewall(func(rules *ole.IDispatch) error {
		owned := map[string]bool{}
		for _, r := range defs {
			owned[r.Name] = true
		}
		remove := []string{}
		if err := comEach(rules, func(v *ole.VARIANT) error {
			r := v.ToIDispatch()
			if r == nil {
				return nil
			}
			name, err := comString(r, "Name")
			if err != nil {
				return err
			}
			if !owned[name] {
				return nil
			}
			group, err := comString(r, "Grouping")
			if err != nil {
				return err
			}
			if group != firewallGroup {
				return fmt.Errorf("防火墙规则 %s 已被其他程序修改，未删除", name)
			}
			remove = append(remove, name)
			return nil
		}); err != nil {
			return err
		}
		var result error
		for _, name := range remove {
			result = errors.Join(result, comCall(rules, "Remove", name))
		}
		return result
	})
}

func withNAT(fn func(*ole.IDispatch) error) error {
	return withCOM(func() error {
		locator, err := comObject("WbemScripting.SWbemLocator")
		if err != nil {
			return err
		}
		defer locator.Release()
		services, err := oleutil.CallMethod(locator, "ConnectServer", ".", `ROOT\StandardCimv2`)
		if err != nil {
			return err
		}
		defer services.Clear()
		return fn(services.ToIDispatch())
	})
}

type natEntry struct{ name, prefix, path string }

func listNAT(svc *ole.IDispatch) ([]natEntry, error) {
	objects, err := oleutil.CallMethod(svc, "ExecQuery", "SELECT * FROM MSFT_NetNat")
	if err != nil {
		return nil, err
	}
	defer objects.Clear()
	entries := []natEntry{}
	err = comEach(objects.ToIDispatch(), func(v *ole.VARIANT) error {
		obj := v.ToIDispatch()
		if obj == nil {
			return errors.New("无效的 Windows NAT 对象")
		}
		name, err := comString(obj, "Name")
		if err != nil {
			return err
		}
		prefix, err := comString(obj, "InternalIPInterfaceAddressPrefix")
		if err != nil {
			return err
		}
		path, err := oleutil.GetProperty(obj, "Path_")
		if err != nil {
			return err
		}
		defer path.Clear()
		fullPath, err := comString(path.ToIDispatch(), "Path")
		if err != nil {
			return err
		}
		entries = append(entries, natEntry{name, prefix, fullPath})
		return nil
	})
	return entries, err
}
func createNAT(name, prefix string, beforeCreate func() error) error {
	return withNAT(func(svc *ole.IDispatch) error {
		entries, err := listNAT(svc)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return errors.New("系统已有 NAT（可能由 Docker / WSL 使用），未修改现有 NAT")
		}
		class, err := oleutil.CallMethod(svc, "Get", "MSFT_NetNat")
		if err != nil {
			return fmt.Errorf("读取 NAT 类：%w", err)
		}
		defer class.Clear()
		object, err := oleutil.CallMethod(class.ToIDispatch(), "SpawnInstance_")
		if err != nil {
			return fmt.Errorf("初始化 NAT：%w", err)
		}
		defer object.Clear()
		instance := object.ToIDispatch()
		if err = comPut(instance, "Name", name); err != nil {
			return err
		}
		if err = comPut(instance, "InternalIPInterfaceAddressPrefix", prefix); err != nil {
			return err
		}
		if err = beforeCreate(); err != nil {
			return err
		}
		if err = comCall(instance, "Put_", int32(2)); err != nil {
			return fmt.Errorf("创建 NAT：%w", err)
		} // wbemChangeFlagCreateOnly
		entries, err = listNAT(svc)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.name == name && e.prefix == prefix {
				return nil
			}
		}
		return errors.New("Windows 未确认自动 NAT 创建成功")
	})
}
func removeNAT(name, prefix string) error {
	if name == "" {
		return nil
	}
	if !strings.HasPrefix(name, "WGM-Desktop-") {
		return errors.New("无效的 NAT 恢复记录")
	}
	return withNAT(func(svc *ole.IDispatch) error {
		entries, err := listNAT(svc)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.name == name {
				if e.prefix != prefix {
					return errors.New("自动 NAT 已被其他程序修改，未删除")
				}
				return comCall(svc, "Delete", e.path)
			}
		}
		return nil
	})
}
