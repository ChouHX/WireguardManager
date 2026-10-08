package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
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
