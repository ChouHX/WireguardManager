package models

import "time"

// Setting 运行时配置项（键值对）。
// config.yaml 只保留无法在运行时决定的内容（监听端口、数据库路径、JWT 密钥等），
// 其余可在管理界面调整，改动即时生效或在下一次重载时生效。
type Setting struct {
	Key       string    `json:"key" gorm:"primaryKey;size:64"`
	Value     string    `json:"value" gorm:"type:text;not null"`
	UpdatedAt time.Time `json:"updated_at"`
}
