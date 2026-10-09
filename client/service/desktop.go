package service

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"

	"wireguardmanager/client/cloud"
)

type CloudAPI interface {
	BaseURL() string
	SetToken(string)
	Login(context.Context, string, string) (cloud.LoginResult, error)
	Me(context.Context) (cloud.User, error)
	Devices(context.Context) ([]cloud.Device, error)
	Access(context.Context, string, string) (cloud.AccessConfig, error)
	SetLANs(context.Context, uint, string) (cloud.Device, error)
}
type DeviceView struct {
	PublicKey       string `json:"publicKey"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Address         string `json:"address"`
	LANs            string `json:"lans"`
	LocalTargets    string `json:"localTargets"`
	AutomaticRoutes bool   `json:"automaticRoutes"`
}
type DesktopView struct {
	ServerURL string       `json:"serverURL"`
	User      *cloud.User  `json:"user"`
	Devices   []DeviceView `json:"devices"`
	Message   string       `json:"message"`
}
type LANDetection struct {
	SuggestedLANs string        `json:"suggestedLANs"`
	Adapters      []AdapterInfo `json:"adapters"`
}

type Desktop struct {
	mu      sync.Mutex
	api     CloudAPI
	backend Backend
	manager *Manager
	store   CloudStore
	data    CloudData
	user    *cloud.User
	devices []cloud.Device
}

func NewDesktop(api CloudAPI, backend Backend, store CloudStore) (*Desktop, error) {
	data, err := store.Load()
	if err != nil {
		return nil, err
	}
	return &Desktop{api: api, backend: backend, manager: NewManager(backend), store: store, data: data}, nil
}
func (d *Desktop) view(message string) DesktopView {
	view := DesktopView{ServerURL: d.api.BaseURL(), User: d.user, Devices: []DeviceView{}, Message: message}
	if d.user == nil {
		return view
	}
	for _, dev := range d.devices {
		if dev.Role == "access" {
			continue
		}
		name := dev.Name
		if name == "" {
			name = "设备 " + strconv.Itoa(int(dev.ID))
		}
		targets, automatic := d.localRoutes(dev)
		view.Devices = append(view.Devices, DeviceView{PublicKey: dev.PublicKey, ID: strconv.Itoa(int(dev.ID)), Name: name, Address: dev.Address, LANs: dev.LANs, LocalTargets: targets, AutomaticRoutes: automatic})
	}
	return view
}
func (d *Desktop) Bootstrap(ctx context.Context) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.user != nil {
		return d.refresh(ctx)
	}
	login := d.data.Login
	if login == nil || login.ServerURL != d.api.BaseURL() {
		return d.view(""), nil
	}
	d.api.SetToken(login.Token)
	user, err := d.api.Me(ctx)
	if err != nil {
		if cloud.IsUnauthorized(err) {
			return d.view("登录已过期，请重新登录"), d.clearLogin()
		}
		return d.view("暂时无法恢复登录，可重试或重新登录"), nil
	}
	if user.ID != login.User.ID {
		return d.view("用户身份已变化，请重新登录"), d.clearLogin()
	}
	d.user = &user
	return d.refresh(ctx)
}
func (d *Desktop) Login(ctx context.Context, email, password string, remember bool) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.manager.Disconnect(); err != nil {
		return d.view(""), err
	}
	if err := d.clearLogin(); err != nil {
		return d.view(""), err
	}
	result, err := d.api.Login(ctx, email, password)
	if err != nil {
		return d.view(""), err
	}
	d.user = &result.User
	if remember {
		d.data.Login = &CloudLogin{d.api.BaseURL(), result.Token, result.User}
	}
	if err = d.store.Save(d.data); err != nil {
		d.api.SetToken("")
		d.user = nil
		d.data.Login = nil
		return d.view(""), err
	}
	return d.refresh(ctx)
}
func (d *Desktop) Logout() (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.manager.Disconnect(); err != nil {
		return d.view(""), err
	}
	if err := d.clearLogin(); err != nil {
		return d.view(""), err
	}
	return d.view(""), nil
}
func (d *Desktop) clearLogin() error {
	d.api.SetToken("")
	d.user = nil
	d.devices = nil
	d.data.Login = nil
	return d.store.Save(d.data)
}
func (d *Desktop) apiError(err error) error {
	if cloud.IsUnauthorized(err) {
		return errors.Join(err, d.manager.Disconnect(), d.clearLogin())
	}
	return err
}
func (d *Desktop) refresh(ctx context.Context) (DesktopView, error) {
	if d.user == nil {
		return d.view(""), errors.New("请先登录")
	}
	devices, err := d.api.Devices(ctx)
	if err != nil {
		return d.view(""), d.apiError(err)
	}
	d.devices = devices
	return d.view(""), nil
}
func (d *Desktop) Refresh(ctx context.Context) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.refresh(ctx); err != nil {
		return d.view(""), err
	}
	if err := d.syncLocalRoutes(ctx); err != nil {
		return d.view(""), err
	}
	return d.view("设备与本机访问路由已同步"), nil
}

// Snapshot lets the UI reflect a cleared login or partial save after an error
// without starting another network request on an already failing connection.
func (d *Desktop) Snapshot() DesktopView {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.view("")
}
func (d *Desktop) DetectLANs() (LANDetection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	adapters, err := d.backend.Adapters()
	if err != nil {
		return LANDetection{}, err
	}
	return LANDetection{SuggestedLANs(adapters, nil), adapters}, nil
}
func (d *Desktop) find(id string) (cloud.Device, error) {
	if d.user == nil {
		return cloud.Device{}, errors.New("请先登录")
	}
	for _, dev := range d.devices {
		if dev.Role != "access" && strconv.Itoa(int(dev.ID)) == id {
			return dev, nil
		}
	}
	return cloud.Device{}, errors.New("所选设备已不存在，请刷新列表")
}

// One local identity per account and deployment, persisted before registration.
func (d *Desktop) accessKey() (string, string, error) {
	scope := fmt.Sprintf("%x/%d", sha256.Sum256([]byte(d.api.BaseURL())), d.user.ID)
	encoded := d.data.AccessKeys[scope]
	if encoded == "" {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return "", "", err
		}
		encoded = base64.StdEncoding.EncodeToString(key.Bytes())
		d.data.AccessKeys[scope] = encoded
		if err := d.store.Save(d.data); err != nil {
			delete(d.data.AccessKeys, scope)
			return "", "", err
		}
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", errors.New("本机访问密钥损坏")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	clear(raw)
	if err != nil {
		return "", "", errors.New("本机访问密钥损坏")
	}
	return encoded, base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}
func gatewayTargets(raw string) (string, error) {
	prefixes, err := ParsePrefixes(raw)
	if err != nil {
		return "", err
	}
	reserved := netip.MustParsePrefix("10.100.0.0/16")
	for _, p := range prefixes {
		if p.Overlaps(reserved) {
			return "", fmt.Errorf("转发目标 %s 不能覆盖 WireGuard 隧道网段", p)
		}
	}
	return JoinPrefixes(prefixes), nil
}
func (d *Desktop) accessProfile(ctx context.Context, dev cloud.Device, targets string) (Profile, error) {
	key, public, err := d.accessKey()
	if err != nil {
		return Profile{}, err
	}
	name, _ := os.Hostname()
	access, err := d.api.Access(ctx, public, name)
	if err != nil {
		return Profile{}, d.apiError(err)
	}
	raw := strings.Replace(access.Config, "[Interface]", "[Interface]\nPrivateKey = "+key, 1)
	cfg, err := ParseConfig(raw)
	if err != nil {
		return Profile{}, err
	}
	if cfg.Address.Addr().String() != access.Peer.Address || len(cfg.DeviceLANs) != 0 {
		return Profile{}, errors.New("本机访问配置与服务端身份不一致")
	}
	p := Profile{ID: strconv.Itoa(int(dev.ID)), Name: dev.Name, Config: cfg, Targets: targets, AccessOnly: true}
	if _, err = p.Routes(); err != nil {
		return p, err
	}
	adapters, err := d.backend.Adapters()
	if err != nil {
		return p, err
	}
	if err = ValidateLocalRouteTargets(p, adapters); err != nil {
		return p, err
	}
	return p, nil
}
func (d *Desktop) saveGateway(ctx context.Context, dev cloud.Device, targets string) error {
	updated, err := d.api.SetLANs(ctx, dev.ID, targets)
	if err != nil {
		return d.apiError(err)
	}
	if updated.ID != dev.ID || updated.PublicKey != dev.PublicKey || updated.Address != dev.Address {
		return errors.New("服务端返回了不同的网关身份，请刷新检查")
	}
	for i := range d.devices {
		if d.devices[i].ID == dev.ID {
			d.devices[i] = updated
		}
	}
	return nil
}

// Saving configures cloud forwarding; automatic local routes follow the active gateway.
func (d *Desktop) SaveDevice(ctx context.Context, id, targets string) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, err := d.find(id)
	if err != nil {
		return d.view(""), err
	}
	targets, err = gatewayTargets(targets)
	if err != nil {
		return d.view(""), err
	}
	if err = d.saveGateway(ctx, dev, targets); err != nil {
		return d.view(""), err
	}
	if d.manager.Active() == id {
		if err = d.syncLocalRoutes(ctx); err != nil {
			return d.view(""), fmt.Errorf("云端转发已更新：%w", err)
		}
	}
	return d.view("转发目标已保存到云端，网关身份与配置保持不变"), nil
}
func (d *Desktop) routeScope(dev cloud.Device) string {
	return fmt.Sprintf("%x/%d/%d", sha256.Sum256([]byte(d.api.BaseURL())), d.user.ID, dev.ID)
}
func (d *Desktop) localRoutes(dev cloud.Device) (string, bool) {
	if targets, ok := d.data.LocalRoutes[d.routeScope(dev)]; ok {
		return targets, false
	}
	return dev.LANs, true
}
func (d *Desktop) saveLocalRoutes(dev cloud.Device, targets string, automatic bool) error {
	scope := d.routeScope(dev)
	old, existed := d.data.LocalRoutes[scope]
	if automatic {
		delete(d.data.LocalRoutes, scope)
	} else {
		d.data.LocalRoutes[scope] = targets
	}
	if err := d.store.Save(d.data); err != nil {
		if existed {
			d.data.LocalRoutes[scope] = old
		} else {
			delete(d.data.LocalRoutes, scope)
		}
		return err
	}
	return nil
}
func (d *Desktop) syncLocalRoutes(ctx context.Context) error {
	id := d.manager.Active()
	if id == "" {
		return nil
	}
	dev, err := d.find(id)
	if err != nil {
		return errors.Join(err, d.manager.Disconnect())
	}
	targets, _ := d.localRoutes(dev)
	if err := d.manager.UpdateRoutes(ctx, id, dev.Name, targets); err != nil {
		// Do not leave stale automatic routes active after a cloud target removal.
		return errors.Join(fmt.Errorf("本机路由同步失败：%w", err), d.manager.Disconnect())
	}
	return nil
}

// Local route preferences never change a gateway's cloud AllowedIPs or keys.
func (d *Desktop) SaveLocalRoutes(ctx context.Context, id, targets string, automatic bool) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, err := d.find(id)
	if err != nil {
		return d.view(""), err
	}
	if automatic {
		targets = dev.LANs
	}
	targets, err = gatewayTargets(targets)
	if err != nil {
		return d.view(""), err
	}
	if err = d.saveLocalRoutes(dev, targets, automatic); err != nil {
		return d.view(""), err
	}
	if d.manager.Active() != "" {
		if err = d.manager.UpdateRoutes(ctx, id, dev.Name, targets); err != nil {
			return d.view(""), fmt.Errorf("本机路由设置已保存，但应用失败：%w", err)
		}
		return d.view("本机路由已应用，设备云端转发配置未修改"), nil
	}
	return d.view("本机路由已保存，连接时自动应用"), nil
}

func (d *Desktop) Connect(ctx context.Context, id, targets string, automatic bool) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.user == nil {
		return d.view(""), errors.New("请先登录")
	}
	targets, err := gatewayTargets(targets)
	if err != nil {
		return d.view(""), err
	}
	if _, err = d.refresh(ctx); err != nil {
		return d.view(""), err
	}
	dev, err := d.find(id)
	if err != nil {
		return d.view(""), err
	}
	if automatic {
		targets = dev.LANs
	}
	targets, err = gatewayTargets(targets)
	if err != nil {
		return d.view(""), err
	}
	if err = d.saveLocalRoutes(dev, targets, automatic); err != nil {
		return d.view(""), err
	}
	if d.manager.Active() != "" {
		if err = d.manager.UpdateRoutes(ctx, id, dev.Name, targets); err != nil {
			return d.view(""), err
		}
		return d.view("本机访问路由已切换，独立隧道保持连接"), nil
	}
	p, err := d.accessProfile(ctx, dev, targets)
	if err != nil {
		return d.view(""), err
	}
	if err = d.manager.Connect(ctx, p); err != nil {
		return d.view(""), err
	}
	return d.view("已使用本机独立身份连接，正在等待云端握手"), nil
}
func (d *Desktop) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.manager.Disconnect()
}
func (d *Desktop) Status(ctx context.Context) Status { return d.manager.Status(ctx) }
