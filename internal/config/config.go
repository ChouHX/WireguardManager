package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// 内置默认值。当 config.yaml 缺失或缺少某个字段时使用，环境变量可覆盖。
const (
	DefaultServerPort     = 8080
	DefaultJWTExpireHours = 24

	// JWT secret 的最短长度：低于该长度视为非法配置（拒绝启动而不是静默使用弱密钥）。
	MinJWTSecretLength = 8
)

// 已知的示例/弱密钥，命中时只告警不拒绝，避免直接破坏现有部署。
var weakJWTSecrets = map[string]bool{
	"supersecretkey": true,
	"secret":         true,
	"changeme":       true,
	"password":       true,
}

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	JWT        JWTConfig        `yaml:"jwt"`
	Network    NetworkConfig    `yaml:"network"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Liveness   LivenessConfig   `yaml:"liveness"`
	Default    DefaultConfig    `yaml:"default"`
	RateLimit  RateLimitConfig  `yaml:"rate_limit"`
}

// RateLimitConfig 公开接口的访问限速。
//
// 这两个接口都不需要认证，却各自有实际代价：login 可被用来无限次猜密码，
// register 每成功一次就创建一个命名空间与一条隧道。限速表以来源地址为键，
// 因此必须配合 server.trusted_proxies 收窄可信代理，否则可被伪造来源绕过。
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"` // 总开关，默认开启

	// LoginPerMinute 单个来源每分钟允许的登录尝试次数。
	LoginPerMinute int `yaml:"login_per_minute"`
	// RegisterPerHour 单个来源每小时允许的注册次数。
	//
	// 比登录严格得多：注册要占用一个隧道网段（上限 254 个）并创建命名空间，
	// 属于典型的低频操作，正常用户不会在短时间内反复注册。
	RegisterPerHour int `yaml:"register_per_hour"`
}

type ServerConfig struct {
	Port        int      `yaml:"port"`
	Mode        string   `yaml:"mode"`         // gin 运行模式：debug / release / test，默认 release
	CorsOrigins []string `yaml:"cors_origins"` // 允许的跨域来源，默认 ["*"]

	// TrustedProxies 可信反向代理的网段/IP 列表，默认空表示不信任任何代理。
	//
	// 这项配置直接影响限速与访问日志的可信度：gin 的默认值是信任所有来源，
	// 于是任何客户端都可以用 X-Forwarded-For 伪造自己的地址，按 IP 限速形同虚设。
	// 只有在确知前面有反向代理时，才把该代理的地址填进来。
	TrustedProxies []string `yaml:"trusted_proxies"`
}

// DatabaseConfig 使用嵌入式 SQLite（纯 Go 驱动，无需 CGO）。
type DatabaseConfig struct {
	Path          string `yaml:"path"`            // 数据库文件路径，相对路径基于进程工作目录
	MaxOpenConns  int    `yaml:"max_open_conns"`  // 最大连接数，默认 1（串行写入，彻底避免 database is locked）
	BusyTimeoutMS int    `yaml:"busy_timeout_ms"` // 锁等待超时，默认 5000ms
	WAL           bool   `yaml:"wal"`             // 是否启用 WAL 日志模式，默认 true
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type NetworkConfig struct {
	ConfigDir    string `yaml:"config_dir"`    // WireGuard配置文件目录
	BaseSubnet   string `yaml:"base_subnet"`   // 基础子网，如 "10.200"
	BasePort     int    `yaml:"base_port"`     // WireGuard起始端口
	OutInterface string `yaml:"out_interface"` // 外网接口名称
	ServerIP     string `yaml:"server_ip"`     // 服务器公网IP地址
	DNS          string `yaml:"dns"`           // 客户端配置下发的 DNS，如 "1.1.1.1, 8.8.8.8"

	// ClientAllowedIPs 下发给客户端配置的 AllowedIPs，即客户端把哪些流量送进隧道。
	// 留空表示按 peer 所在网段自动推导：例如服务端接口 10.100.0.1/24、分配到该 peer 的
	// 地址为 10.100.0.2，则下发 10.100.0.0/24。
	//
	// 注意：账号命名空间内没有公网出口，写成 "0.0.0.0/0, ::/0" 时隧道能建立，
	// 但明文包在命名空间内查不到下一跳，访问不了公网。要保持原有转发语义需另行
	// 提供出口通路。
	ClientAllowedIPs string `yaml:"client_allowed_ips"`
}

type MonitoringConfig struct {
	IntervalSeconds      int `yaml:"interval_seconds"`       // 采样间隔
	RetentionHours       int `yaml:"retention_hours"`        // 监控记录保留时长
	CleanupIntervalHours int `yaml:"cleanup_interval_hours"` // 清理任务执行周期
}

// LivenessConfig 客户端在线判定。
//
// 判据来自 WireGuard 自身的握手状态：peer 的 last handshake 在阈值时间内
// 持续更新，即说明隧道处于活跃状态。
//
// 早期版本曾用「向 peer 隧道地址发 TCP SYN」的方式探测，但后端进程运行在
// 宿主机命名空间，而隧道网段只在各账号的 netns 内可达，探测包根本到不了
// 对端，导致设备明明在线却被判离线。
type LivenessConfig struct {
	// Enabled 是否启用在线判定。属于部署决策（探测会产生 TCP 连接），
	// 用 liveness.enabled / WM_LIVENESS_ENABLED 控制，不在管理界面暴露。
	Enabled bool `yaml:"enabled"`
	// ProbePort 探测端口：唯一需要人工介入的参数，可在管理界面调整。
	// 选高位空闲端口，避免与对端真实服务冲突或被安全策略拦截。
	ProbePort int `yaml:"probe_port"`
}

// 说明：判定节奏、超时、离线确认次数与流量窗口均与实现强相关，
// 由服务端内部决定（见 services/peer_liveness.go 顶部的常量），不再作为配置项。
// 这样用户只需关心一个端口，其余交给实现保证。

// DefaultConfig 平台默认管理员账号
type DefaultConfig struct {
	AdminEmail    string `yaml:"admin_email"`
	AdminPassword string `yaml:"admin_password"`
	AdminName     string `yaml:"admin_name"`

	// 兼容旧版 config.yaml.example 中的 username / password 字段
	LegacyUsername string `yaml:"username"`
	LegacyPassword string `yaml:"password"`
}

var AppConfig *Config

// defaultConfig 返回一份带完整默认值的配置，作为 yaml 与环境变量的底座。
func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:        DefaultServerPort,
			Mode:        "release",
			CorsOrigins: []string{"*"},
			// 默认不信任任何代理：gin 的默认值恰好相反（信任全部来源），
			// 那会让 X-Forwarded-For 可被任意伪造。确有反向代理时再显式填写。
			TrustedProxies: nil,
		},
		RateLimit: RateLimitConfig{
			Enabled:         true,
			LoginPerMinute:  10,
			RegisterPerHour: 5,
		},
		Database: DatabaseConfig{
			Path:          "./data/cloud_platform.db",
			MaxOpenConns:  1,
			BusyTimeoutMS: 5000,
			WAL:           true,
		},
		JWT: JWTConfig{
			Secret:      "", // 不提供默认密钥，缺失时由 Validate 拒绝启动
			ExpireHours: DefaultJWTExpireHours,
		},
		Network: NetworkConfig{
			ConfigDir:    "/etc/wg_config",
			BaseSubnet:   "10.200",
			BasePort:     51820,
			OutInterface: "eth0",
			ServerIP:     "",
			DNS:          "1.1.1.1, 8.8.8.8",
		},
		Monitoring: MonitoringConfig{
			IntervalSeconds:      10,
			RetentionHours:       168, // 7 天
			CleanupIntervalHours: 24,
		},
		Liveness: LivenessConfig{
			Enabled:   true,
			ProbePort: 49151, // 高位端口，多数情况下未监听，内核必回 RST
		},
		// Default 段留空，由 normalize 依次完成 legacy 字段兼容与内置默认值填充
		Default: DefaultConfig{},
	}
}

// LoadConfig 读取 yaml 配置，叠加 WM_* 环境变量覆盖。
// 文件不存在时退化为"默认值 + 环境变量"并打印告警；只有 JWT secret 为空或非法才返回 error。
func LoadConfig(configPath string) error {
	cfg := defaultConfig()

	data, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("failed to unmarshal config %s: %w", configPath, err)
		}
	case errors.Is(err, os.ErrNotExist):
		log.Printf("Warning: config file %s not found, falling back to built-in defaults overridden by WM_* environment variables", configPath)
	default:
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	cfg.normalize()
	applyEnvOverrides(cfg)
	cfg.normalize() // 环境变量可能引入空值，再兜底一次

	AppConfig = cfg

	// 未显式配置密钥时，从数据目录读取或自动生成一份并持久化，
	// 这样部署侧无需再关心 JWT 密钥。
	if err := cfg.ensureJWTSecret(); err != nil {
		return err
	}
	if err := cfg.validateJWT(); err != nil {
		return err
	}

	return nil
}

// ensureJWTSecret 保证存在可用的签名密钥，取值顺序：
//  1. 显式配置（config.yaml 的 jwt.secret 或 WM_JWT_SECRET）；
//  2. 数据目录下持久化的 jwt.secret 文件；
//  3. 自动生成 32 字节随机密钥并写入该文件（0600）。
//
// 第 3 步失败（目录不可写）时降级为进程内临时密钥并告警，不阻断启动。
func (c *Config) ensureJWTSecret() error {
	if strings.TrimSpace(c.JWT.Secret) != "" {
		return nil
	}

	keyPath := c.jwtSecretPath()

	if data, err := os.ReadFile(keyPath); err == nil {
		if secret := strings.TrimSpace(string(data)); len(secret) >= MinJWTSecretLength {
			c.JWT.Secret = secret
			log.Printf("Using the persisted JWT secret from %s", keyPath)
			return nil
		}
		log.Printf("Warning: persisted JWT secret in %s is too short, generating a new one", keyPath)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("failed to generate JWT secret: %w", err)
	}
	secret := hex.EncodeToString(buf)

	if dir := filepath.Dir(keyPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Printf("Warning: cannot create %s to persist JWT secret: %v; using an ephemeral key "+
				"(tokens will be invalidated on restart)", dir, err)
			c.JWT.Secret = secret
			return nil
		}
	}

	if err := os.WriteFile(keyPath, []byte(secret+"\n"), 0o600); err != nil {
		log.Printf("Warning: cannot write %s: %v; using an ephemeral key "+
			"(tokens will be invalidated on restart)", keyPath, err)
		c.JWT.Secret = secret
		return nil
	}

	log.Printf("Generated a JWT secret and saved it to %s (keep this file with your data)", keyPath)
	c.JWT.Secret = secret
	return nil
}

// jwtSecretPath 密钥文件的存放位置：与数据库同目录，随数据一起备份。
func (c *Config) jwtSecretPath() string {
	dir := filepath.Dir(strings.TrimSpace(c.Database.Path))
	if dir == "" || dir == "." {
		dir = "."
	}
	return filepath.Join(dir, "jwt.secret")
}

// normalize 填充空值，保证下游拿到的配置始终可用。
func (c *Config) normalize() {
	// default 段：新字段优先，旧字段名兼容
	if c.Default.AdminEmail == "" {
		c.Default.AdminEmail = c.Default.LegacyUsername
	}
	if c.Default.AdminEmail == "" {
		c.Default.AdminEmail = "admin@platform.com"
	}
	if c.Default.AdminPassword == "" {
		c.Default.AdminPassword = c.Default.LegacyPassword
	}
	if c.Default.AdminPassword == "" {
		c.Default.AdminPassword = "password"
	}
	if c.Default.AdminName == "" {
		c.Default.AdminName = "admin"
	}

	if c.Server.Mode == "" {
		c.Server.Mode = "release"
	}
	if len(c.Server.CorsOrigins) == 0 {
		c.Server.CorsOrigins = []string{"*"}
	}

	// SQLite 参数兜底。注意 wal 不在此处兜底：默认值已在 defaultConfig 注入，
	// 若在此强制置 true 会覆盖用户在 yaml / 环境变量中显式关闭 WAL 的意图。
	if strings.TrimSpace(c.Database.Path) == "" {
		c.Database.Path = "./data/cloud_platform.db"
	}
	if c.Database.MaxOpenConns <= 0 {
		c.Database.MaxOpenConns = 1
	}
	if c.Database.BusyTimeoutMS <= 0 {
		c.Database.BusyTimeoutMS = 5000
	}

	if c.Network.DNS == "" {
		c.Network.DNS = "1.1.1.1, 8.8.8.8"
	}

	if c.Monitoring.IntervalSeconds <= 0 {
		c.Monitoring.IntervalSeconds = 10
	}
	if c.Monitoring.RetentionHours <= 0 {
		c.Monitoring.RetentionHours = 168
	}
	if c.Monitoring.CleanupIntervalHours <= 0 {
		c.Monitoring.CleanupIntervalHours = 24
	}

	if c.Liveness.ProbePort <= 0 || c.Liveness.ProbePort > 65535 {
		c.Liveness.ProbePort = 49151
	}

	if c.JWT.ExpireHours <= 0 {
		c.JWT.ExpireHours = DefaultJWTExpireHours
	}
}

// applyEnvOverrides 用 WM_* 环境变量覆盖 yaml 中的值（环境变量优先级更高）。
func applyEnvOverrides(c *Config) {
	setString(&c.Server.Mode, "WM_SERVER_MODE")
	setInt(&c.Server.Port, "WM_SERVER_PORT")
	setStringSlice(&c.Server.CorsOrigins, "WM_CORS_ORIGINS")
	setStringSlice(&c.Server.TrustedProxies, "WM_TRUSTED_PROXIES")

	setBool(&c.RateLimit.Enabled, "WM_RATE_LIMIT_ENABLED")
	setInt(&c.RateLimit.LoginPerMinute, "WM_RATE_LIMIT_LOGIN_PER_MINUTE")
	setInt(&c.RateLimit.RegisterPerHour, "WM_RATE_LIMIT_REGISTER_PER_HOUR")
	if v := strings.TrimSpace(os.Getenv("WM_SERVER_CORS_ORIGINS")); v != "" {
		origins := make([]string, 0, 4)
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				origins = append(origins, part)
			}
		}
		if len(origins) > 0 {
			c.Server.CorsOrigins = origins
		}
	}

	setString(&c.Database.Path, "WM_DB_PATH")
	setInt(&c.Database.MaxOpenConns, "WM_DB_MAX_OPEN_CONNS")
	setInt(&c.Database.BusyTimeoutMS, "WM_DB_BUSY_TIMEOUT_MS")
	setBool(&c.Database.WAL, "WM_DB_WAL")

	setString(&c.JWT.Secret, "WM_JWT_SECRET")
	setInt(&c.JWT.ExpireHours, "WM_JWT_EXPIRE_HOURS")

	setString(&c.Network.ConfigDir, "WM_NETWORK_CONFIG_DIR")
	setString(&c.Network.BaseSubnet, "WM_NETWORK_BASE_SUBNET")
	setInt(&c.Network.BasePort, "WM_NETWORK_BASE_PORT")
	setString(&c.Network.OutInterface, "WM_NETWORK_OUT_INTERFACE")
	setString(&c.Network.ServerIP, "WM_NETWORK_SERVER_IP")
	setString(&c.Network.DNS, "WM_NETWORK_DNS")
	setString(&c.Network.ClientAllowedIPs, "WM_NETWORK_CLIENT_ALLOWED_IPS")

	setInt(&c.Monitoring.IntervalSeconds, "WM_MONITORING_INTERVAL_SECONDS")
	setInt(&c.Monitoring.RetentionHours, "WM_MONITORING_RETENTION_HOURS")
	setInt(&c.Monitoring.CleanupIntervalHours, "WM_MONITORING_CLEANUP_INTERVAL_HOURS")

	setBool(&c.Liveness.Enabled, "WM_LIVENESS_ENABLED")
	setInt(&c.Liveness.ProbePort, "WM_LIVENESS_PROBE_PORT")

	setString(&c.Default.AdminEmail, "WM_DEFAULT_ADMIN_EMAIL")
	setString(&c.Default.AdminPassword, "WM_DEFAULT_ADMIN_PASSWORD")
	setString(&c.Default.AdminName, "WM_DEFAULT_ADMIN_NAME")
}

func setString(dst *string, envKey string) {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		*dst = v
	}
}

func setInt(dst *int, envKey string) {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("Warning: ignoring invalid integer in %s=%q: %v", envKey, v, err)
		return
	}
	*dst = parsed
}

func setBool(dst *bool, envKey string) {
	v := strings.TrimSpace(os.Getenv(envKey))
	if v == "" {
		return
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("Warning: ignoring invalid boolean in %s=%q: %v", envKey, v, err)
		return
	}
	*dst = parsed
}

// setStringSlice 解析逗号分隔的列表型环境变量。
// 显式传入空字符串（WM_X=""）表示清空为无元素，而不是"未设置"。
func setStringSlice(dst *[]string, envKey string) {
	raw, ok := os.LookupEnv(envKey)
	if !ok {
		return
	}

	items := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	*dst = items
}

// validateJWT 只校验 JWT 相关配置，用于 LoadConfig 中的"致命错误"判定。
func (c *Config) validateJWT() error {
	secret := c.JWT.Secret
	if strings.TrimSpace(secret) == "" {
		// ensureJWTSecret 已保证有值，走到这里说明密钥来源异常
		return errors.New("jwt secret is unavailable: set WM_JWT_SECRET or ensure the data directory is writable")
	}
	if len(secret) < MinJWTSecretLength {
		return fmt.Errorf("jwt.secret is invalid: got %d characters, at least %d required", len(secret), MinJWTSecretLength)
	}
	if weakJWTSecrets[strings.ToLower(secret)] {
		log.Printf("Warning: jwt.secret looks like a well-known placeholder value, replace it before exposing this service")
	}
	return nil
}

// Validate 在启动阶段做完整配置校验，返回所有问题的汇总。
func (c *Config) Validate() error {
	var problems []string

	if err := c.validateJWT(); err != nil {
		problems = append(problems, err.Error())
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		problems = append(problems, fmt.Sprintf("server.port must be within 1..65535, got %d", c.Server.Port))
	}
	// 直接校验原始取值（GinMode() 会把非法值静默回退成 release，不能用于判定合法性）
	if mode := strings.ToLower(strings.TrimSpace(c.Server.Mode)); !isValidGinMode(mode) {
		problems = append(problems, fmt.Sprintf("server.mode must be one of debug/release/test, got %q", c.Server.Mode))
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		problems = append(problems, "database.path must not be empty")
	}
	if c.RateLimit.LoginPerMinute < 0 {
		problems = append(problems, fmt.Sprintf("rate_limit.login_per_minute must be >= 0, got %d", c.RateLimit.LoginPerMinute))
	}
	if c.RateLimit.RegisterPerHour < 0 {
		problems = append(problems, fmt.Sprintf("rate_limit.register_per_hour must be >= 0, got %d", c.RateLimit.RegisterPerHour))
	}
	if c.Database.MaxOpenConns < 1 {
		problems = append(problems, fmt.Sprintf("database.max_open_conns must be >= 1, got %d", c.Database.MaxOpenConns))
	}
	if c.Database.BusyTimeoutMS < 0 {
		problems = append(problems, fmt.Sprintf("database.busy_timeout_ms must be >= 0, got %d", c.Database.BusyTimeoutMS))
	}
	if strings.TrimSpace(c.Network.ConfigDir) == "" {
		problems = append(problems, "network.config_dir must not be empty")
	}
	if strings.TrimSpace(c.Network.BaseSubnet) == "" {
		problems = append(problems, "network.base_subnet must not be empty")
	}
	if c.Network.BasePort < 1 || c.Network.BasePort > 65535 {
		problems = append(problems, fmt.Sprintf("network.base_port must be within 1..65535, got %d", c.Network.BasePort))
	}
	if strings.TrimSpace(c.Network.DNS) == "" {
		problems = append(problems, "network.dns must not be empty")
	}
	if v := strings.TrimSpace(c.Network.ClientAllowedIPs); v != "" {
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, err := netip.ParsePrefix(item); err != nil {
				problems = append(problems,
					fmt.Sprintf("network.client_allowed_ips contains invalid CIDR %q: %v", item, err))
			}
		}
	}
	if strings.TrimSpace(c.Default.AdminEmail) == "" {
		problems = append(problems, "default.admin_email must not be empty")
	}
	if strings.TrimSpace(c.Default.AdminPassword) == "" {
		problems = append(problems, "default.admin_password must not be empty")
	}
	if c.Liveness.ProbePort < 1 || c.Liveness.ProbePort > 65535 {
		problems = append(problems, fmt.Sprintf("liveness.probe_port must be within 1..65535, got %d", c.Liveness.ProbePort))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// gin 模式白名单（此处的值需与 gin.DebugMode/ReleaseMode/TestMode 保持一致，
// 但 config 包不引入 gin，避免配置层与 Web 框架耦合）。
var ginModeAliases = map[string]struct{}{
	"debug":   {},
	"release": {},
	"test":    {},
}

func isValidGinMode(mode string) bool {
	_, ok := ginModeAliases[mode]
	return ok
}

// GinMode 返回合法的 gin 运行模式，非法值回退为 release。
func (c *Config) GinMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Server.Mode))
	if isValidGinMode(mode) {
		return mode
	}
	return "release"
}

func (m MonitoringConfig) Interval() time.Duration {
	return time.Duration(m.IntervalSeconds) * time.Second
}

func (m MonitoringConfig) Retention() time.Duration {
	return time.Duration(m.RetentionHours) * time.Hour
}

func (m MonitoringConfig) CleanupInterval() time.Duration {
	return time.Duration(m.CleanupIntervalHours) * time.Hour
}

// GetDSN 返回 SQLite 连接串（glebarez/sqlite 支持 _pragma= 形式的内联参数）。
func (c *Config) GetDSN() string {
	journalMode := "DELETE"
	if c.Database.WAL {
		journalMode = "WAL"
	}

	pragmas := []string{
		fmt.Sprintf("busy_timeout(%d)", c.Database.BusyTimeoutMS),
		fmt.Sprintf("journal_mode(%s)", journalMode),
		"foreign_keys(1)",
		// NORMAL 在 WAL 下兼顾持久性与写入性能（FULL 会显著放大 fsync 次数）
		"synchronous(NORMAL)",
	}

	var b strings.Builder
	b.WriteString("file:")
	b.WriteString(c.Database.Path)
	b.WriteString("?")
	for i, pragma := range pragmas {
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString("_pragma=")
		b.WriteString(pragma)
	}

	return b.String()
}

// DatabasePath 返回配置中的数据库文件路径，供调用方创建父目录等使用。
func (c *Config) DatabasePath() string {
	return c.Database.Path
}
