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
	cfg, err := ParseConfig(raw)
	if err != nil {
		return Profile{}, dev, err
	}
	cfg.DeviceLANs, err = ParsePrefixes(lans)
	if err != nil {
		return Profile{}, dev, err
	}
	for _, lan := range cfg.DeviceLANs {
		for _, vpn := range cfg.BaseRoutes {
			if lan.Overlaps(vpn) {
				return Profile{}, dev, errors.New("下挂设备 / 局域网不能与 WireGuard 网段重叠")
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
	p := Profile{ID: id, Name: dev.Name, Config: cfg, Targets: JoinPrefixes(all)}
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
func (d *Desktop) save(ctx context.Context, p Profile, dev cloud.Device, targets string) error {
	lans := JoinPrefixes(p.Config.DeviceLANs)
	if lans != dev.LANs {
		updated, err := d.api.SetLANs(ctx, dev.ID, lans)
		if err != nil {
			return d.apiError(err)
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
	d.data.Targets[key] = JoinPrefixes(extra)
	if err := d.store.Save(d.data); err != nil {
		if existed {
			d.data.Targets[key] = old
		} else {
			delete(d.data.Targets, key)
		}
		return fmt.Errorf("云端局域网已保存，但本机访问目标保存失败：%w", err)
	}
	return nil
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
	if err = d.save(ctx, p, dev, targets); err != nil {
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
	// Recheck authorization and route ownership on every connection; cached credentials
	// are never used to bypass server-side device removal or tenant disablement.
	if _, err := d.refresh(ctx); err != nil {
		return d.view(""), err
	}
	p, dev, err := d.prepare(ctx, id, lans, targets)
	if err != nil {
		return d.view(""), err
	}
	if err = d.save(ctx, p, dev, targets); err != nil {
		return d.view(""), err
	}
	raw, err := d.api.Config(ctx, dev.ID)
	if err != nil {
		return d.view(""), d.apiError(err)
	}
	p.Config, err = ParseConfig(raw)
	if err != nil {
		return d.view(""), err
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
