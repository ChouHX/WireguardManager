package services

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"cloud-platform/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 可运行时调整的配置键。config.yaml 只作为这些值的初始默认来源，
// 一旦在管理界面修改过，就以数据库中的值为准。
const (
	SettingNetworkServerIP         = "network.server_ip"
	SettingNetworkOutInterface     = "network.out_interface"
	SettingNetworkBaseSubnet       = "network.base_subnet"
	SettingNetworkBasePort         = "network.base_port"
	SettingNetworkDNS              = "network.dns"
	SettingNetworkClientAllowedIPs = "network.client_allowed_ips"

	SettingMonitoringInterval  = "monitoring.interval_seconds"
	SettingMonitoringRetention = "monitoring.retention_hours"

	SettingLivenessEnabled          = "liveness.enabled"
	SettingLivenessInterval         = "liveness.interval_seconds"
	SettingLivenessProbeTimeout     = "liveness.probe_timeout_ms"
	SettingLivenessProbePort        = "liveness.probe_port"
	SettingLivenessTrafficStale     = "liveness.traffic_stale_seconds"
	SettingLivenessOfflineThreshold = "liveness.offline_threshold"

	SettingJWTExpireHours = "jwt.expire_hours"

	SettingPeerDefaultPSK = "wireguard.default_preshared_key"
)

// SettingDef 描述一个可配置项，供管理端渲染表单与校验。
type SettingDef struct {
	Key   string `json:"key"`
	Type  string `json:"type"`  // string | int | bool
	Group string `json:"group"` // network | monitoring | liveness | auth | wireguard
	Min   int    `json:"min,omitempty"`
	Max   int    `json:"max,omitempty"`
}

// deprecatedSettings 已从管理界面移除的配置键。
// 它们可能残留在旧数据库中，初始化时会被清理，否则前端提交整份配置时
// 会因白名单校验而报 "Unknown setting key"。
var deprecatedSettings = []string{
	"liveness.handshake_timeout_seconds",
}

// SettingDefs 全部可运行时调整的配置项定义。
var SettingDefs = []SettingDef{
	{Key: SettingNetworkServerIP, Type: "string", Group: "network"},
	{Key: SettingNetworkOutInterface, Type: "string", Group: "network"},
	{Key: SettingNetworkBaseSubnet, Type: "string", Group: "network"},
	{Key: SettingNetworkBasePort, Type: "int", Group: "network", Min: 1, Max: 65535},
	{Key: SettingNetworkDNS, Type: "string", Group: "network"},
	{Key: SettingNetworkClientAllowedIPs, Type: "string", Group: "network"},

	{Key: SettingMonitoringInterval, Type: "int", Group: "monitoring", Min: 1, Max: 3600},
	{Key: SettingMonitoringRetention, Type: "int", Group: "monitoring", Min: 1, Max: 8760},

	{Key: SettingLivenessEnabled, Type: "bool", Group: "liveness"},
	{Key: SettingLivenessInterval, Type: "int", Group: "liveness", Min: 1, Max: 300},
	{Key: SettingLivenessProbeTimeout, Type: "int", Group: "liveness", Min: 100, Max: 10000},
	{Key: SettingLivenessProbePort, Type: "int", Group: "liveness", Min: 1, Max: 65535},
	{Key: SettingLivenessTrafficStale, Type: "int", Group: "liveness", Min: 1, Max: 3600},
	{Key: SettingLivenessOfflineThreshold, Type: "int", Group: "liveness", Min: 1, Max: 60},

	{Key: SettingJWTExpireHours, Type: "int", Group: "auth", Min: 1, Max: 8760},

	{Key: SettingPeerDefaultPSK, Type: "bool", Group: "wireguard"},
}

// Settings 运行时配置：数据库持久化 + 内存缓存，读取零成本。
type Settings struct {
	db     *gorm.DB
	mu     sync.RWMutex
	values map[string]string
}

var appSettings *Settings

// InitSettings 载入运行时配置；defaults 通常来自 config.yaml，仅用于首次初始化。
func InitSettings(db *gorm.DB, defaults map[string]string) (*Settings, error) {
	store := &Settings{db: db, values: make(map[string]string)}

	var rows []models.Setting
	if err := db.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	stored := make(map[string]string, len(rows))
	for _, row := range rows {
		stored[row.Key] = row.Value
	}

	// 先铺默认值，再用数据库中的值覆盖
	for key, value := range defaults {
		store.values[key] = value
	}
	for key, value := range stored {
		store.values[key] = value
	}

	// 数据库里缺失的键补写一次，便于管理端展示与后续修改
	missing := make([]models.Setting, 0, len(defaults))
	for key, value := range defaults {
		if _, ok := stored[key]; !ok {
			missing = append(missing, models.Setting{Key: key, Value: value})
		}
	}
	if len(missing) > 0 {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error; err != nil {
			log.Printf("Warning: failed to persist initial settings: %v", err)
		}
	}

	// 清理已废弃的历史键
	if len(deprecatedSettings) > 0 {
		if err := db.Where("key IN ?", deprecatedSettings).Delete(&models.Setting{}).Error; err != nil {
			log.Printf("Warning: failed to clean up deprecated settings: %v", err)
		}
		for _, key := range deprecatedSettings {
			delete(store.values, key)
		}
	}

	appSettings = store
	return store, nil
}

// GetSettings 返回全局运行时配置（未初始化时返回 nil）。
func GetSettings() *Settings {
	return appSettings
}

// String 读取字符串值。
func (s *Settings) String(key, fallback string) string {
	if s == nil {
		return fallback
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if value, ok := s.values[key]; ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

// Int 读取整数值，非法或缺失时返回 fallback。
func (s *Settings) Int(key string, fallback int) int {
	raw := s.String(key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

// Bool 读取布尔值。
func (s *Settings) Bool(key string, fallback bool) bool {
	raw := s.String(key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

// All 返回当前全部配置的快照。
func (s *Settings) All() map[string]string {
	if s == nil {
		return map[string]string{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out
}

// Update 批量更新配置并持久化。
func (s *Settings) Update(values map[string]string) error {
	if s == nil {
		return fmt.Errorf("settings store is not initialized")
	}
	if len(values) == 0 {
		return nil
	}

	rows := make([]models.Setting, 0, len(values))
	for key, value := range values {
		if !IsManagedSetting(key) {
			// 客户端可能回传整份配置，其中夹带已下线的历史键。
			// 这类键直接忽略（它们已不再生效），不因此中断保存。
			log.Printf("Warning: ignoring unknown setting key %q", key)
			continue
		}
		rows = append(rows, models.Setting{Key: key, Value: value})
	}

	if len(rows) == 0 {
		return nil
	}

	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&rows).Error; err != nil {
		return fmt.Errorf("failed to persist settings: %w", err)
	}

	s.mu.Lock()
	for key, value := range values {
		s.values[key] = value
	}
	s.mu.Unlock()

	return nil
}

// SettingDefByKey 按键查找配置项定义。
func SettingDefByKey(key string) (SettingDef, bool) {
	for _, def := range SettingDefs {
		if def.Key == key {
			return def, true
		}
	}
	return SettingDef{}, false
}

// IsManagedSetting 判断键是否属于可运行时配置的白名单。
func IsManagedSetting(key string) bool {
	for _, def := range SettingDefs {
		if def.Key == key {
			return true
		}
	}
	return false
}
