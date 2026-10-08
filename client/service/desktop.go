package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	Config(context.Context, uint) (string, error)
	SetLANs(context.Context, uint, string) (cloud.Device, error)
}
type DeviceView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	LANs        string `json:"lans"`
	Targets     string `json:"targets"`
	AutoTargets string `json:"autoTargets"`
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
func (d *Desktop) targetKey(id uint) string {
	return fmt.Sprintf("%x/%d/%d", sha256.Sum256([]byte(d.api.BaseURL())), d.user.ID, id)
}
func (d *Desktop) view(message string) DesktopView {
	view := DesktopView{ServerURL: d.api.BaseURL(), User: d.user, Devices: []DeviceView{}, Message: message}
	if d.user == nil {
		return view
	}
	for _, dev := range d.devices {
		name := dev.Name
		if name == "" {
			name = "设备 " + strconv.Itoa(int(dev.ID))
		}
		view.Devices = append(view.Devices, DeviceView{strconv.Itoa(int(dev.ID)), name, dev.Address, dev.LANs, d.data.Targets[d.targetKey(dev.ID)], d.autoTargets(dev.ID)})
	}
	return view
}
func (d *Desktop) autoTargets(id uint) string {
	var raw []string
	for _, dev := range d.devices {
		if dev.ID != id && dev.LANs != "" {
			raw = append(raw, dev.LANs)
		}
	}
	p, err := ParsePrefixes(strings.Join(raw, ","))
	if err != nil {
		return ""
	}
	return JoinPrefixes(p)
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
	return d.refresh(ctx)
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
		if strconv.Itoa(int(dev.ID)) == id {
			return dev, nil
		}
	}
	return cloud.Device{}, errors.New("所选设备已不存在，请刷新列表")
}
func (d *Desktop) prepare(ctx context.Context, id, lans, targets string) (Profile, cloud.Device, error) {
	dev, err := d.find(id)
	if err != nil {
		return Profile{}, dev, err
	}
	raw, err := d.api.Config(ctx, dev.ID)
	if err != nil {
		return Profile{}, dev, d.apiError(err)
	}
	return d.prepareConfig(raw, dev, lans, targets)
}
func (d *Desktop) prepareConfig(raw string, dev cloud.Device, lans, targets string) (Profile, cloud.Device, error) {
	cfg, err := ParseConfig(raw)
	if err != nil {
		return Profile{}, dev, err
	}
	dev.LANs = JoinPrefixes(cfg.DeviceLANs)
	for i := range d.devices {
		if d.devices[i].ID == dev.ID {
			d.devices[i].LANs = dev.LANs
		}
	}
	cfg.DeviceLANs, err = ParsePrefixes(lans)
	if err != nil {
		return Profile{}, dev, err
	}
	for _, lan := range cfg.DeviceLANs {
		for _, vpn := range cfg.BaseRoutes {
			if lan.Overlaps(vpn) {
				return Profile{}, dev, fmt.Errorf("填写的局域网 %s 与 WireGuard 虚拟网段 %s 重叠。这里应填写本机连接的真实现场局域网；仅访问远端时请留空", lan, vpn)
			}
		}
	}
	extra, err := ParsePrefixes(targets)
	if err != nil {
		return Profile{}, dev, err
	}
	combined := JoinPrefixes(extra)
	if len(extra) == 0 {
		combined = d.autoTargets(dev.ID)
	}
	all, err := ParsePrefixes(combined)
	if err != nil {
		return Profile{}, dev, err
	}
	p := Profile{ID: strconv.FormatUint(uint64(dev.ID), 10), Name: dev.Name, Config: cfg, Targets: JoinPrefixes(all)}
	if _, err = p.Routes(); err != nil {
		return p, dev, err
	}
	adapters, err := d.backend.Adapters()
	if err != nil {
		return p, dev, err
	}
	if err = ValidateLocalRouteTargets(p, adapters); err != nil {
		return p, dev, err
	}
	if _, err = DetectLANAdapters(cfg.DeviceLANs, adapters); err != nil {
		return p, dev, err
	}
	return p, dev, nil
}
func (d *Desktop) save(ctx context.Context, p Profile, dev cloud.Device, targets string) (bool, error) {
	lans := JoinPrefixes(p.Config.DeviceLANs)
	changed := lans != dev.LANs
	if changed {
		updated, err := d.api.SetLANs(ctx, dev.ID, lans)
		if err != nil {
			return false, d.apiError(err)
		}
		for i := range d.devices {
			if d.devices[i].ID == dev.ID {
				d.devices[i] = updated
			}
		}
	}
	extra, _ := ParsePrefixes(targets)
	key := d.targetKey(dev.ID)
	old, existed := d.data.Targets[key]
	normalized := JoinPrefixes(extra)
	if old == normalized {
		return changed, nil
	}
	d.data.Targets[key] = normalized
	if err := d.store.Save(d.data); err != nil {
		if existed {
			d.data.Targets[key] = old
		} else {
			delete(d.data.Targets, key)
		}
		return changed, fmt.Errorf("本机访问目标保存失败（云端局域网可能已更新）：%w", err)
	}
	return changed, nil
}
func (d *Desktop) SaveDevice(ctx context.Context, id, lans, targets string) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.manager.Active() == id {
		return d.view(""), errors.New("请先断开当前设备再修改下挂地址")
	}
	p, dev, err := d.prepare(ctx, id, lans, targets)
	if err != nil {
		return d.view(""), err
	}
	if _, err = d.save(ctx, p, dev, targets); err != nil {
		return d.view(""), err
	}
	return d.view("设备局域网已同步到云端"), nil
}
func (d *Desktop) Connect(ctx context.Context, id, lans, targets string) (DesktopView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.user == nil {
		return d.view(""), errors.New("请先登录")
	}
	dev, err := d.find(id)
	if err != nil {
		return d.view(""), err
	}
	// Validate syntax before any remote request. Route ownership is checked against
	// fresh server data below; private configurations are never cached offline.
	if _, err = ParsePrefixes(lans); err != nil {
		return d.view(""), err
	}
	if _, err = ParsePrefixes(targets); err != nil {
		return d.view(""), err
	}
	// These authenticated reads are independent. Both complete before handling a
	// possible 401, so clearing the shared API token cannot race an active request.
	var raw string
	var devices []cloud.Device
	var configErr, listErr error
	var reads sync.WaitGroup
	reads.Add(2)
	go func() { defer reads.Done(); raw, configErr = d.api.Config(ctx, dev.ID) }()
	go func() { defer reads.Done(); devices, listErr = d.api.Devices(ctx) }()
	reads.Wait()
	if cloud.IsUnauthorized(configErr) {
		return d.view(""), d.apiError(configErr)
	}
	if listErr != nil {
		return d.view(""), d.apiError(listErr)
	}
	if configErr != nil {
		return d.view(""), d.apiError(configErr)
	}
	d.devices = devices
	dev, err = d.find(id)
	if err != nil {
		return d.view(""), err
	}
	p, dev, err := d.prepareConfig(raw, dev, lans, targets)
	if err != nil {
		return d.view(""), err
	}
	changed, err := d.save(ctx, p, dev, targets)
	if err != nil {
		return d.view(""), err
	}
	// Only a cloud LAN mutation invalidates the configuration just fetched.
	if changed {
		raw, err = d.api.Config(ctx, dev.ID)
		if err != nil {
			return d.view(""), d.apiError(err)
		}
		updated, err := ParseConfig(raw)
		if err != nil {
			return d.view(""), err
		}
		if JoinPrefixes(updated.DeviceLANs) != JoinPrefixes(p.Config.DeviceLANs) {
			return d.view(""), errors.New("云端局域网配置已发生变化，请刷新设备后重试")
		}
		p.Config = updated
	}
	if err = d.manager.Connect(ctx, p); err != nil {
		return d.view(""), err
	}
	return d.view("已启动连接，正在等待云端握手"), nil
}
func (d *Desktop) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.manager.Disconnect()
}
func (d *Desktop) Status(ctx context.Context) Status { return d.manager.Status(ctx) }
