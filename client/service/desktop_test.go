package service

import (
	"context"
	"encoding/base64"
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
	accessKeys             []string
	denied                 bool
	onConfig               func()
	onDevices              func()
	listCalls              int
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
	m.listCalls++
	if m.onDevices != nil {
		m.onDevices()
	}
	if m.denied {
		return nil, &cloud.APIError{Status: 401}
	}
	return append([]cloud.Device{}, m.devices...), nil
}
func (m *mockCloud) Access(_ context.Context, publicKey, name string) (cloud.AccessConfig, error) {
	m.configCalls++
	m.accessKeys = append(m.accessKeys, publicKey)
	if m.onConfig != nil {
		m.onConfig()
	}
	if m.denied {
		return cloud.AccessConfig{}, &cloud.APIError{Status: 401}
	}
	lines := strings.Split(fixtureConfig(), "\n")
	var config []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "PrivateKey") {
			config = append(config, line)
		}
	}
	return cloud.AccessConfig{Peer: cloud.Device{ID: 99, Role: "access", PublicKey: publicKey, Address: "10.100.1.3"}, Config: strings.Join(config, "\n")}, nil
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
	api := &mockCloud{base: "https://one.example", user: cloud.User{ID: 7, Email: "test@example.com"}, devices: []cloud.Device{{ID: 1, Name: "OpenWrt", Address: "10.100.1.2", PublicKey: "gateway-one"}, {ID: 2, Name: "ARM Linux", Address: "10.100.1.4", PublicKey: "gateway-two", LANs: "192.168.2.0/24"}}}
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
func TestDesktopUsesIndependentIdentityAndCloudGatewayTargets(t *testing.T) {
	d, api, b, _ := desktopFixture(t)
	ctx := context.Background()
	view, err := d.Connect(ctx, "1", "192.168.9.100")
	if err != nil {
		t.Fatal(err)
	}
	if api.mutations != 1 || api.configCalls != 1 {
		t.Fatal("missing registration/cloud assignment")
	}
	p := b.profiles[0]
	if !p.AccessOnly || len(p.Config.DeviceLANs) != 0 || p.Config.Address.Addr().String() != "10.100.1.3" || p.Targets != "192.168.9.100/32" {
		t.Fatal("did not use independent access mode")
	}
	if api.devices[0].Address != "10.100.1.2" || api.devices[0].PublicKey != "gateway-one" {
		t.Fatal("gateway identity changed")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(p.Config.PrivateKey[:])) {
		t.Fatal("view leaked credentials")
	}
	if _, err = d.SaveDevice(ctx, "1", "192.168.8.0/24"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Connect(ctx, "2", "192.168.2.100"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.log, []string{"open 1", "close 1", "open 1", "close 1", "open 2"}) {
		t.Fatal(b.log)
	}
	if api.accessKeys[0] != api.accessKeys[1] || api.accessKeys[1] != api.accessKeys[2] {
		t.Fatal("gateway switching changed desktop identity")
	}
	if _, err = d.Logout(); err != nil {
		t.Fatal(err)
	}
	if d.manager.Active() != "" || api.token != "" {
		t.Fatal("logout did not disconnect")
	}
}
func TestSaveRemoteTargetsDoesNotRequireWindowsLAN(t *testing.T) {
	d, api, b, _ := desktopFixture(t)
	ctx := context.Background()
	// Cloud-only management must work even when the remote LAN overlaps Windows.
	if _, err := d.SaveDevice(ctx, "1", "192.168.1.0/24"); err != nil {
		t.Fatal(err)
	}
	if api.mutations != 1 || api.configCalls != 0 || len(b.profiles) != 0 {
		t.Fatal("cloud save opened a local tunnel")
	}
	before := api.mutations
	if _, err := d.Connect(ctx, "1", "192.168.1.0/24"); err == nil {
		t.Fatal("local IP conflict allowed")
	}
	if api.mutations != before || len(b.profiles) != 0 {
		t.Fatal("invalid local route changed gateway")
	}
	if _, err := d.Connect(ctx, "1", "192.168.1.200"); err != nil {
		t.Fatal(err)
	}
	api.denied = true
	if _, err := d.Refresh(ctx); !cloud.IsUnauthorized(err) {
		t.Fatal(err)
	}
	if d.user != nil || api.token != "" || d.manager.Active() != "" {
		t.Fatal("auth expiry kept connection")
	}
}
func TestSaveDoesNotSkipStaleCloudTargets(t *testing.T) {
	d, api, _, _ := desktopFixture(t)
	// Another administrator updates the server after our last refresh.
	api.devices[0].LANs = "192.168.9.0/24"
	if _, err := d.SaveDevice(context.Background(), "1", ""); err != nil {
		t.Fatal(err)
	}
	if api.devices[0].LANs != "" || api.mutations != 1 {
		t.Fatal("stale snapshot suppressed desired update")
	}
}
func TestCloudStoreIsolatesAccessKeys(t *testing.T) {
	d, api, b, store := desktopFixture(t)
	ctx := context.Background()
	if _, err := d.Connect(ctx, "1", ""); err != nil {
		t.Fatal(err)
	}
	first := api.accessKeys[0]
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(b.profiles[0].Config.PrivateKey[:])) {
		t.Fatal("plaintext credentials persisted")
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
	if err != nil || view.User == nil {
		t.Fatal("restore", err)
	}
	if _, err = restored.Connect(ctx, "1", ""); err != nil {
		t.Fatal(err)
	}
	if api.accessKeys[1] != first {
		t.Fatal("relaunch lost access identity")
	}
	otherAPI := &mockCloud{base: "https://two.example", user: api.user, devices: api.devices}
	other, err := NewDesktop(otherAPI, b, store)
	if err != nil {
		t.Fatal(err)
	}
	view, err = other.Bootstrap(ctx)
	if err != nil || view.User != nil || otherAPI.token != "" {
		t.Fatal("cross-deployment token reuse")
	}
	if _, err = other.Login(ctx, "test@example.com", "pass", false); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Connect(ctx, "1", ""); err != nil {
		t.Fatal(err)
	}
	if otherAPI.accessKeys[0] == first {
		t.Fatal("cross-deployment key reuse")
	}
	api.user.ID = 8
	if _, err = d.Login(ctx, "second@example.com", "pass", false); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Connect(ctx, "1", ""); err != nil {
		t.Fatal(err)
	}
	if api.accessKeys[len(api.accessKeys)-1] == first {
		t.Fatal("cross-account key reuse")
	}
}
func TestDisconnectWaitsForInFlightConnect(t *testing.T) {
	d, api, _, _ := desktopFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	api.onConfig = func() { close(entered); <-release }
	connected := make(chan error, 1)
	go func() { _, err := d.Connect(context.Background(), "1", ""); connected <- err }()
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
		t.Fatal("disconnect raced with connect")
	}
}
func TestDesktopHidesAccessPeersAndRejectsInvalidTargets(t *testing.T) {
	d, api, _, _ := desktopFixture(t)
	api.devices = append(api.devices, cloud.Device{ID: 99, Role: "access", Name: "Windows"})
	view, err := d.Refresh(context.Background())
	if err != nil || len(view.Devices) != 2 {
		t.Fatal("access terminal exposed as gateway")
	}
	if _, err = d.find("99"); err == nil {
		t.Fatal("access identity selectable as gateway")
	}
	before := api.listCalls
	for _, target := range []string{"invalid", "10.100.1.2", "10.0.0.0/8", "0.0.0.0/0"} {
		if _, err = d.Connect(context.Background(), "1", target); err == nil {
			t.Fatal("invalid/reserved route accepted")
		}
	}
	if api.configCalls != 0 || api.listCalls != before || api.mutations != 0 {
		t.Fatal("invalid input made network request")
	}
}
