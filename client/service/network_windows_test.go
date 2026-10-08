//go:build windows

package service

import (
	"fmt"
	"os"
	"testing"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Only opt in on an isolated elevated Windows runner. Normal unit tests never
// mutate the host firewall or NAT. Each integration test owns and removes its objects.
func requireNetworkIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("WGM_TEST_WINDOWS_NETWORK") != "1" {
		t.Skip("isolated Windows network integration disabled")
	}
}
func TestWindowsFirewallLifecycle(t *testing.T) {
	requireNetworkIntegration(t)
	adapters, err := (&WindowsBackend{}).Adapters()
	if err != nil || len(adapters) == 0 {
		t.Fatal("no usable adapter", err)
	}
	name := fmt.Sprintf("WGM-Desktop-test-%d", time.Now().UnixNano())
	rules := []firewallRule{{name + "-in", adapters[0].Name, "192.0.2.0/24", 1}, {name + "-out", adapters[0].Name, "192.0.2.0/24", 2}}
	t.Cleanup(func() {
		if err := removeFirewallRules(rules); err != nil {
			t.Error("cleanup", err)
		}
	})
	if err = addFirewallRules(rules); err != nil {
		t.Fatal(err)
	}
	if err = withFirewall(func(collection *ole.IDispatch) error {
		for _, def := range rules {
			v, err := oleutil.CallMethod(collection, "Item", def.Name)
			if err != nil {
				return err
			}
			defer v.Clear()
			group, err := comString(v.ToIDispatch(), "Grouping")
			if err != nil || group != firewallGroup {
				return fmt.Errorf("incorrect rule ownership: %s %v", group, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = removeFirewallRules(rules); err != nil {
		t.Fatal(err)
	}
	if err = withFirewall(func(collection *ole.IDispatch) error {
		for _, def := range rules {
			v, err := oleutil.CallMethod(collection, "Item", def.Name)
			if v != nil {
				v.Clear()
			}
			if err == nil {
				return fmt.Errorf("rule still present after disconnect: %s", def.Name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestWindowsNATLifecycle(t *testing.T) {
	requireNetworkIntegration(t)
	var before []natEntry
	err := withNAT(func(svc *ole.IDispatch) error { var err error; before, err = listNAT(svc); return err })
	if err != nil {
		t.Skipf("Windows NAT provider unavailable; client uses routed mode: %v", err)
	}
	name := fmt.Sprintf("WGM-Desktop-test-%d", time.Now().UnixNano())
	prefix := "192.0.2.0/24"
	armed := false
	t.Cleanup(func() {
		if armed {
			if err := removeNAT(name, prefix); err != nil {
				t.Error("cleanup", err)
			}
		}
	})
	err = createNAT(name, prefix, func() error { armed = true; return nil })
	if len(before) > 0 {
		if err == nil || armed {
			t.Fatal("existing NAT was not preserved")
		}
		t.Log("existing NAT preserved; client uses routed mode")
		return
	}
	if err != nil {
		t.Fatal("NAT provider exists but creation failed", err)
	}
	if !armed {
		t.Fatal("mutation not journaled")
	}
	if err = removeNAT(name, prefix); err != nil {
		t.Fatal(err)
	}
	if err = withNAT(func(svc *ole.IDispatch) error {
		entries, err := listNAT(svc)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.name == name {
				return fmt.Errorf("NAT still present after disconnect")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
