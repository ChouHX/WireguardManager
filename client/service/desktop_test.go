package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wireguardmanager/client/cloud"
)

type mockCloud struct {
	base, token            string
	user                   cloud.User
	devices                []cloud.Device
	configCalls, mutations int
	denied                 bool
	onConfig               func()
}

func (m *mockCloud) BaseURL() string       { return m.base }
func (m *mockCloud) SetToken(token string) { m.token = token }
func (m *mockCloud) Login(context.Context, string, string) (cloud.LoginResult, error) {
	m.token = "private-token"
	return cloud.LoginResult{Token: m.token, User: m.user}, nil
}
func (m *mockCloud) Me(context.Context) (cloud.User, error) {
	if m.denied {
		return cloud.User{}, &cloud.APIError{Status: 401}
	}
	return m.user, nil
}
func (m *mockCloud) Devices(context.Context) ([]cloud.Device, error) {
	if m.denied {
		return nil, &cloud.APIError{Status: 401}
	}
	return append([]cloud.Device{}, m.devices...), nil
}
func (m *mockCloud) Config(_ context.Context, id uint) (string, error) {
	m.configCalls++
	if m.onConfig != nil {
		m.onConfig()
	}
	for _, dev := range m.devices {
		if dev.ID == id {
			return "# WGM-Device-LAN = " + dev.LANs + "\n" + fixtureConfig(), nil
		}
	}
	return "", &cloud.APIError{Status: 404}
}
func (m *mockCloud) SetLANs(_ context.Context, id uint, lans string) (cloud.Device, error) {
	m.mutations++
	for i := range m.devices {
		if m.devices[i].ID == id {
			m.devices[i].LANs = lans
			return m.devices[i], nil
		}
	}
	return cloud.Device{}, &cloud.APIError{Status: 404}
}

type desktopBackend struct {
	fakeBackend
	adapters []AdapterInfo
	profiles []Profile
}

func (b *desktopBackend) Adapters() ([]AdapterInfo, error) { return b.adapters, nil }
func (b *desktopBackend) Open(ctx context.Context, p Profile) (Session, error) {
	b.profiles = append(b.profiles, p)
	return b.fakeBackend.Open(ctx, p)
}
func desktopFixture(t *testing.T) (*Desktop, *mockCloud, *desktopBackend, CloudStore) {
	t.Helper()
	api := &mockCloud{base: "https://one.example", user: cloud.User{ID: 7, Email: "test@example.com"}, devices: []cloud.Device{{ID: 1, Name: "PC"}, {ID: 2, Name: "PLC", LANs: "192.168.2.0/24"}}}
	backend := &desktopBackend{adapters: []AdapterInfo{lanAdapter("Ethernet", "192.168.1.10/24", 10)}}
	store := CloudStore{Path: filepath.Join(t.TempDir(), "cloud.dpapi"), Cipher: &testCipher{}}
	d, err := NewDesktop(api, backend, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Login(context.Background(), "test@example.com", "not-persisted-password", true); err != nil {
		t.Fatal(err)
	}
	return d, api, backend, store
}
func TestDesktopSyncsLANAndUsesFreshConfig(t *testing.T) {
	d, api, b, _ := desktopFixture(t)
	ctx := context.Background()
	view, err := d.Connect(ctx, "1", "192.168.1.100", "")
	if err != nil {
		t.Fatal(err)
	}
	if api.mutations != 1 || api.configCalls != 2 {
		t.Fatal("cloud sync/config reload missing")
	}
	p := b.profiles[0]
	if JoinPrefixes(p.Config.DeviceLANs) != "192.168.1.100/32" || p.Targets != "192.168.2.0/24" {
		t.Fatal("wrong effective LAN or automatic remote routes")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "PrivateKey") || strings.Contains(string(raw), "123456789") {
		t.Fatal("public view leaked secrets")
	}
	if _, err = d.SaveDevice(ctx, "1", "", ""); err == nil {
		t.Fatal("edited active config")
	}
	if _, err = d.Connect(ctx, "2", "", "192.168.9.100"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.log, []string{"open 1", "close 1", "open 2"}) {
		t.Fatal(b.log)
	}
	if b.profiles[1].Targets != "192.168.9.100/32" {
		t.Fatal("explicit target did not override auto targets")
	}
	if _, err = d.Logout(); err != nil {
		t.Fatal(err)
	}
	if d.manager.Active() != "" || api.token != "" {
		t.Fatal("logout left session active")
	}
}
func TestDesktopRejectsLocalConflictBeforeCloudWrite(t *testing.T) {
	d, api, b, _ := desktopFixture(t)
	api.devices[1].LANs = "192.168.1.0/24"
	ctx := context.Background()
	if _, err := d.Connect(ctx, "1", "192.168.1.100", ""); err == nil {
		t.Fatal("conflicting automatic routes accepted")
	}
	if api.mutations != 0 || len(b.profiles) != 0 {
		t.Fatal("validation failure mutated cloud or connected")
	}
	if _, err := d.Connect(ctx, "1", "", "192.168.1.200"); err != nil {
		t.Fatal(err)
	}
	api.denied = true
	if _, err := d.Refresh(ctx); !cloud.IsUnauthorized(err) {
		t.Fatal("expired auth not returned", err)
	}
	if d.user != nil || api.token != "" || d.manager.Active() != "" {
		t.Fatal("expired auth did not disconnect/clear token")
	}
}
func TestCloudStoreIsolatesAccountsAndDeployment(t *testing.T) {
	d, api, b, store := desktopFixture(t)
	ctx := context.Background()
	if _, err := d.SaveDevice(ctx, "1", "", "192.168.9.100"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "not-persisted-password") {
		t.Fatal("plaintext credentials persisted")
	}
	data, err := store.Load()
	if err != nil || data.Login == nil || data.Login.Token != "private-token" {
		t.Fatal("remembered login lost", err)
	}
	plain, _ := store.Cipher.Unprotect(raw)
	if strings.Contains(string(plain), "not-persisted-password") {
		t.Fatal("password persisted")
	}
	restored, err := NewDesktop(api, b, store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := restored.Bootstrap(ctx)
	if err != nil || view.User == nil || view.Devices[0].Targets != "192.168.9.100/32" {
		t.Fatal("restore failed", err)
	}
	otherAPI := &mockCloud{base: "https://two.example", user: api.user, devices: api.devices}
	other, err := NewDesktop(otherAPI, b, store)
	if err != nil {
		t.Fatal(err)
	}
	view, err = other.Bootstrap(ctx)
	if err != nil || view.User != nil || otherAPI.token != "" {
		t.Fatal("token reused for another deployment")
	}
	view, err = other.Login(ctx, "test@example.com", "pass", false)
	if err != nil || view.Devices[0].Targets != "" {
		t.Fatal("targets crossed deployment", err)
	}
	api.user.ID = 8
	view, err = d.Login(ctx, "second@example.com", "pass", false)
	if err != nil || view.Devices[0].Targets != "" {
		t.Fatal("targets crossed accounts", err)
	}
	data, err = store.Load()
	if err != nil || data.Login != nil {
		t.Fatal("non-remembered login persisted")
	}
}

func TestDisconnectWaitsForInFlightConnect(t *testing.T) {
	d, api, _, _ := desktopFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	api.onConfig = func() {
		if api.configCalls == 1 {
			close(entered)
			<-release
		}
	}
	connected := make(chan error, 1)
	go func() { _, err := d.Connect(context.Background(), "1", "", ""); connected <- err }()
	<-entered
	disconnected := make(chan error, 1)
	go func() { disconnected <- d.Disconnect() }()
	close(release)
	if err := <-connected; err != nil {
		t.Fatal(err)
	}
	if err := <-disconnected; err != nil {
		t.Fatal(err)
	}
	if d.manager.Active() != "" {
		t.Fatal("shutdown raced with in-flight connection")
	}
}
