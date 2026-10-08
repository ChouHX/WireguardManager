package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NormalizeURL accepts a deployment root, including reverse-proxy subpaths.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("服务端地址必须是完整的 http/https 地址，不能包含账号、查询参数或片段")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	if strings.HasSuffix(u.Path, "/api") {
		u.Path = strings.TrimSuffix(u.Path, "/api")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

type User struct {
	ID    uint   `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Deliberately omit the peer private_key field returned by older server versions.
type Device struct {
	ID      uint   `json:"id"`
	Name    string `json:"comment"`
	Address string `json:"peer_address"`
	LANs    string `json:"device_lan"`
}

// Current servers expose editable LANs separately from full AllowedIPs. Older
// servers returned only LANs; also accept full legacy lists without putting the
// assigned tunnel address into either the LAN editor or automatic access targets.
func (d *Device) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ID         uint    `json:"id"`
		Name       string  `json:"comment"`
		Address    string  `json:"peer_address"`
		AllowedIPs string  `json:"allowed_ips"`
		LANs       *string `json:"device_lan"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	*d = Device{ID: wire.ID, Name: wire.Name, Address: wire.Address}
	if wire.LANs != nil {
		d.LANs = *wire.LANs
		return nil
	}
	self, _ := netip.ParseAddr(wire.Address)
	var lans []string
	for _, part := range strings.Split(wire.AllowedIPs, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == wire.Address {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err == nil && prefix.Contains(self) {
			continue
		}
		lans = append(lans, part)
	}
	d.LANs = strings.Join(lans, ", ")
	return nil
}

type LoginResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string   { return e.Message }
func IsUnauthorized(err error) bool { var e *APIError; return errors.As(err, &e) && e.Status == 401 }

// Keep diagnostic context without exposing response bodies, credentials or
// transport errors that may contain proxy credentials.
type RequestError struct {
	Operation string
	Stage     string
	Elapsed   time.Duration
	Attempts  int
	Cause     error
}

func (e *RequestError) Unwrap() error { return e.Cause }
func (e *RequestError) Error() string {
	reason := "网络连接失败"
	var networkError net.Error
	switch {
	case errors.Is(e.Cause, context.Canceled):
		reason = "请求已取消"
	case errors.Is(e.Cause, context.DeadlineExceeded) || errors.As(e.Cause, &networkError) && networkError.Timeout():
		if e.Stage == "read" {
			reason = "读取响应超时"
		} else {
			reason = "等待服务端响应超时"
		}
	case e.Stage == "read":
		reason = "服务端响应传输中断"
	}
	retry := ""
	if e.Attempts > 1 {
		retry = "，已重试一次"
	}
	hint := "请重试"
	if e.Operation == "保存设备局域网" {
		hint = "设置可能已保存，请刷新设备确认"
	}
	return fmt.Sprintf("%s失败：%s（%.1f 秒%s）；%s", e.Operation, reason, e.Elapsed.Seconds(), retry, hint)
}
func operationName(method, path string) string {
	switch {
	case path == "/api/login":
		return "登录"
	case path == "/api/me":
		return "恢复登录"
	case strings.HasSuffix(path, "/config"):
		return "获取设备配置"
	case method == "PATCH":
		return "保存设备局域网"
	default:
		return "获取设备列表"
	}
}

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(raw string) (*Client, error) {
	base, err := NormalizeURL(raw)
	if err != nil {
		return nil, err
	}
	return &Client{base: base, http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) BaseURL() string       { return c.base }
func (c *Client) SetToken(token string) { c.token = token }
func (c *Client) Login(ctx context.Context, email, password string) (LoginResult, error) {
	var result LoginResult
	// Never send an old account's token with a login attempt.
	c.token = ""
	err := c.request(ctx, "POST", "/api/login", map[string]string{"email": strings.TrimSpace(email), "password": password}, &result)
	if err != nil {
		return result, err
	}
	if result.Token == "" || result.User.ID == 0 {
		return LoginResult{}, errors.New("服务端返回了无效的登录信息")
	}
	c.token = result.Token
	return result, nil
}
func (c *Client) Me(ctx context.Context) (User, error) {
	var user User
	err := c.request(ctx, "GET", "/api/me", nil, &user)
	if err == nil && user.ID == 0 {
		err = errors.New("服务端返回了无效的用户信息")
	}
	return user, err
}
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	devices := []Device{}
	err := c.request(ctx, "GET", "/api/wireguard/peers", nil, &devices)
	if devices == nil {
		devices = []Device{}
	}
	return devices, err
}
func (c *Client) Config(ctx context.Context, id uint) (string, error) {
	var data struct {
		Config string `json:"config"`
	}
	err := c.request(ctx, "GET", devicePath(id)+"/config", nil, &data)
	if err == nil && data.Config == "" {
		err = errors.New("服务端未返回设备配置")
	}
	return data.Config, err
}
func (c *Client) SetLANs(ctx context.Context, id uint, lans string) (Device, error) {
	var device Device
	err := c.request(ctx, "PATCH", devicePath(id), map[string]string{"allowed_ips": lans}, &device)
	return device, err
}
func devicePath(id uint) string { return "/api/wireguard/peers/" + strconv.FormatUint(uint64(id), 10) }
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	started := time.Now()
	// A retry shares the original time budget. Never replay a login or mutation:
	// a dropped PATCH response does not mean the server failed to save it.
	timeout := c.http.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := c.requestOnce(ctx, method, path, input, output)
		var failure *RequestError
		if !errors.As(err, &failure) {
			return err
		}
		failure.Operation, failure.Elapsed, failure.Attempts = operationName(method, path), time.Since(started), attempt
		var networkError net.Error
		timedOut := errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout()
		if method != "GET" || attempt >= 2 || ctx.Err() != nil || timedOut || errors.Is(err, context.Canceled) {
			return failure
		}
		// Only retry transport failures, not authorization / validation responses.
	}
}
func (c *Client) requestOnce(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		defer clear(raw)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errors.New("无法创建服务端请求")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return &RequestError{Stage: "connect", Cause: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	defer clear(raw)
	if err != nil {
		// Authorization failure is actionable even if its explanatory body was cut off.
		if res.StatusCode == http.StatusUnauthorized {
			return &APIError{Status: 401, Message: "登录已失效，请重新登录"}
		}
		return &RequestError{Stage: "read", Cause: err}
	}
	if len(raw) > 2*1024*1024 {
		return errors.New("服务端响应过大")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   *struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	if res.StatusCode >= 200 && res.StatusCode < 300 && decodeErr != nil {
		return errors.New("服务端响应不是有效的 JSON，请确认管理后台 API 可用")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !envelope.Success {
		message := fmt.Sprintf("服务端请求失败（HTTP %d）", res.StatusCode)
		code := ""
		if envelope.Error != nil {
			code = envelope.Error.Code
			message = envelope.Error.Message
			if code == "VALIDATION_FAILED" {
				var details string
				if json.Unmarshal(envelope.Error.Details, &details) == nil && details != "" {
					message = details
				}
			}
		}
		if res.StatusCode == 401 {
			message = "登录已失效，请重新登录"
			if path == "/api/login" {
				message = "账号或密码不正确"
			}
		}
		if res.StatusCode == 429 {
			message = "请求过于频繁，请稍后重试"
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			message = "服务端地址发生重定向，请使用最终的管理后台地址重新构建客户端"
		}
		return &APIError{res.StatusCode, code, message}
	}
	if err = json.Unmarshal(envelope.Data, output); err != nil {
		return errors.New("无法解析服务端数据")
	}
	return nil
}
