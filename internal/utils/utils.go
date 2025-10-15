package utils

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// GenerateRandomCode 生成指定长度的随机码
func GenerateRandomCode(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return strings.ToUpper(hex.EncodeToString(bytes)[:length])
}
