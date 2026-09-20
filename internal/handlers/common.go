package handlers

import (
	"cloud-platform/internal/models"

	"github.com/gin-gonic/gin"
)

// currentUser 从 gin 上下文安全取出当前登录用户。
// 之前各 handler 直接用 `user.(*models.User)`，一旦上下文缺失或类型不符就会 panic；
// 统一走这里，类型不符时返回 false 由调用方决定响应。
func currentUser(c *gin.Context) (*models.User, bool) {
	value, exists := c.Get("user")
	if !exists {
		return nil, false
	}

	user, ok := value.(*models.User)
	if !ok || user == nil {
		return nil, false
	}

	return user, true
}
