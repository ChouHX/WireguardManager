package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIAuthenticationAndDeviceOperations(t *testing.T) {
	ctx := context.Background()
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/console/api/login" {
			if r.Header.Get("Authorization") != "" {
				t.Error("login carried old token")
			}
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			if input["email"] != "user@example.com" || input["password"] != "test-password" {
				t.Error("incorrect login body")
			}
			w.Write([]byte(`{"success":true,"data":{"token":"test-token","user":{"id":7,"email":"user@example.com"}}}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing authentication")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /console/api/me":
			w.Write([]byte(`{"success":true,"data":{"id":7}}`))
		case "GET /console/api/wireguard/peers":
			w.Write([]byte(`{"success":true,"data":[{"id":3,"comment":"PLC","peer_address":"10.100.1.3","allowed_ips":"192.168.1.0/24","private_key":"never-expose"}]}`))
		case "GET /console/api/wireguard/peers/3/config":
			w.Write([]byte(`{"success":true,"data":{"config":"private-config"}}`))
		case "PATCH /console/api/wireguard/peers/3":
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			if !reflect.DeepEqual(input, map[string]string{"allowed_ips": ""}) {
				t.Errorf("unexpected cloud mutation: %v", input)
			}
			w.Write([]byte(`{"success":true,"data":{"id":3,"allowed_ips":""}}`))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c, err := New(server.URL + "/console/api/")
	if err != nil {
		t.Fatal(err)
	}
	c.SetToken("old-token")
	if _, err = c.Login(ctx, " user@example.com ", "test-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Me(ctx); err != nil {
		t.Fatal(err)
	}
	devices, err := c.Devices(ctx)
	if err != nil || len(devices) != 1 || devices[0].Name != "PLC" {
		t.Fatal(devices, err)
	}
	raw, _ := json.Marshal(devices)
	if strings.Contains(string(raw), "never-expose") || strings.Contains(string(raw), "private_key") {
		t.Fatal("device DTO leaked key")
	}
	config, err := c.Config(ctx, 3)
	if err != nil || config != "private-config" {
		t.Fatal("config retrieval failed", err)
	}
	if _, err = c.SetLANs(ctx, 3, ""); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 5 {
		t.Fatal(calls)
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c, _ := New(origin.URL)
	if _, err := c.Login(context.Background(), "test@example.com", "password"); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected {
		t.Fatal("credentials were redirected")
	}
}

func TestDeviceLANFieldDoesNotIncludeTunnelAddress(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"current", `{"peer_address":"10.100.1.2","allowed_ips":"10.100.1.2/32,192.168.1.0/24","device_lan":"192.168.1.0/24"}`, "192.168.1.0/24"},
		{"cleared", `{"peer_address":"10.100.1.2","allowed_ips":"10.100.1.2/32","device_lan":""}`, ""},
		{"legacy LAN", `{"peer_address":"10.100.1.2","allowed_ips":"192.168.1.0/24"}`, "192.168.1.0/24"},
		{"legacy full routes", `{"peer_address":"10.100.1.2","allowed_ips":"10.100.1.2/32,192.168.1.0/24"}`, "192.168.1.0/24"},
		{"legacy tunnel only", `{"peer_address":"10.100.1.2","allowed_ips":"10.100.1.2/32"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dev Device
			if err := json.Unmarshal([]byte(tc.raw), &dev); err != nil || dev.LANs != tc.want {
				t.Fatalf("tunnel leaked into LAN or LAN lost: %q, %v", dev.LANs, err)
			}
		})
	}
}

func TestUnauthorizedAndInvalidURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"success":false,"error":{"message":"unauthorized"}}`))
	}))
	defer server.Close()
	c, _ := New(server.URL)
	_, err := c.Devices(context.Background())
	if !IsUnauthorized(err) {
		t.Fatal(err)
	}
	for _, raw := range []string{"example.com", "ftp://example.com", "https://user:pass@example.com", "https://example.com?token=x", "https://example.com/#x"} {
		if _, err := New(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestInterruptedReadRetriesGETOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "200")
			w.Write([]byte(`{"success":true,"data":`))
			return
		}
		w.Write([]byte(`{"success":true,"data":[{"id":3}]}`))
	}))
	defer server.Close()
	c, _ := New(server.URL)
	devices, err := c.Devices(context.Background())
	if err != nil || len(devices) != 1 || calls.Load() != 2 {
		t.Fatal("GET recovery failed", err, calls.Load())
	}
}

func TestInterruptedMutationIsNotReplayedOrExposed(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", "200")
		w.Write([]byte(`private-config-must-not-appear`))
	}))
	defer server.Close()
	c, _ := New(server.URL)
	_, err := c.SetLANs(context.Background(), 3, "")
	var detail *RequestError
	if !errors.As(err, &detail) || !errors.Is(err, io.ErrUnexpectedEOF) || detail.Stage != "read" || detail.Attempts != 1 {
		t.Fatal("lost network failure detail", err)
	}
	if calls.Load() != 1 || !strings.Contains(err.Error(), "设置可能已保存") || !strings.Contains(err.Error(), "保存网关转发目标") {
		t.Fatal("mutation replayed or ambiguous save not explained", err)
	}
	if strings.Contains(err.Error(), "private-config") {
		t.Fatal("error leaked response")
	}
}

func TestSlowBodyReportsTimeoutWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"success":`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c, _ := New(server.URL)
	c.http.Timeout = 80 * time.Millisecond
	_, err := c.Config(context.Background(), 3)
	var detail *RequestError
	if !errors.As(err, &detail) || detail.Stage != "read" || !strings.Contains(err.Error(), "读取响应超时") || !strings.Contains(err.Error(), "获取设备配置") {
		t.Fatal("unhelpful timeout", err)
	}
	if detail.Elapsed <= 0 || calls.Load() != 1 {
		t.Fatal("timeout budget reset by retry")
	}
}

func TestTruncatedUnauthorizedResponseStillExpiresLogin(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(401)
		w.Write([]byte(`{"success":false`))
	}))
	defer server.Close()
	c, _ := New(server.URL)
	_, err := c.Devices(context.Background())
	if !IsUnauthorized(err) || calls.Load() != 1 {
		t.Fatal("unauthorized response retried or hidden", err)
	}
}

func TestAccessRegistrationAPI(t *testing.T) {
	for _, mode := range []string{"ok", "wrong-identity", "old-server"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer access-test" {
					t.Error("missing auth")
				}
				if r.Method != "POST" || r.URL.Path != "/api/wireguard/access" {
					t.Error("wrong access endpoint")
				}
				var input map[string]string
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(input, map[string]string{"public_key": "desktop-public", "name": "Laptop"}) {
					t.Error("registration sent unexpected fields")
				}
				if mode == "old-server" {
					w.WriteHeader(404)
					return
				}
				key := "desktop-public"
				if mode == "wrong-identity" {
					key = "other-public"
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"peer": map[string]any{"id": 3, "device_role": "access", "public_key": key, "peer_address": "10.100.1.3"}, "config": "[Interface]\nAddress = 10.100.1.3/32"}})
			}))
			defer server.Close()
			c, _ := New(server.URL)
			c.SetToken("access-test")
			access, err := c.Access(context.Background(), "desktop-public", "Laptop")
			if mode == "ok" {
				if err != nil || access.Peer.Role != "access" || access.Peer.ID != 3 {
					t.Fatal("registration failed", err)
				}
			} else if err == nil {
				t.Fatal("incompatible registration accepted")
			} else if mode == "old-server" && !strings.Contains(err.Error(), "更新服务端") {
				t.Fatal("missing upgrade instruction")
			}
		})
	}
}
