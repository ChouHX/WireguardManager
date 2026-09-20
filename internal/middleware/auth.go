package middleware

import (
	"cloud-platform/internal/auth"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 用户快照缓存：RequireAuth 每个受保护请求都要读一次用户，属于热点路径。
// TTL 取 10 秒，在"角色/资料变更及时生效"与"减少数据库压力"之间折中；
// 变更用户的后台接口会主动调用 InvalidateUserCache 立即失效，无需等待 TTL。
const (
	userCacheTTL     = 10 * time.Second
	userCacheMaxSize = 4096
)

type cachedUser struct {
	user      *models.User
	expiresAt time.Time
}

var (
	userCacheMu sync.RWMutex
	userCache   = make(map[uint]*cachedUser)
)

// cloneUser 复制一份用户快照，避免调用方（或缓存）共享同一对象被互相改写。
func cloneUser(u *models.User) *models.User {
	cp := *u
	return &cp
}

// getCachedUser 读取未过期的用户快照；过期项顺手删除（惰性清理）。
func getCachedUser(userID uint) (*models.User, bool) {
	now := time.Now()

	userCacheMu.RLock()
	entry, ok := userCache[userID]
	userCacheMu.RUnlock()

	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		userCacheMu.Lock()
		// 二次确认，避免删除刚被其他请求刷新的条目
		if cur, exists := userCache[userID]; exists && now.After(cur.expiresAt) {
			delete(userCache, userID)
		}
		userCacheMu.Unlock()
		return nil, false
	}

	return cloneUser(entry.user), true
}

// setCachedUser 写入用户快照；容量超限时先清理过期项，仍超限则整体清空重建。
func setCachedUser(u *models.User) {
	if u == nil {
		return
	}

	now := time.Now()

	userCacheMu.Lock()
	defer userCacheMu.Unlock()

	if len(userCache) >= userCacheMaxSize {
		pruneExpiredUsersLocked(now)
		if len(userCache) >= userCacheMaxSize {
			userCache = make(map[uint]*cachedUser)
		}
	}

	userCache[u.ID] = &cachedUser{
		user:      cloneUser(u),
		expiresAt: now.Add(userCacheTTL),
	}
}

// InvalidateUserCache 主动失效某个用户的缓存（资料/角色/密码变更、删除用户时调用）。
func InvalidateUserCache(userID uint) {
	userCacheMu.Lock()
	delete(userCache, userID)
	userCacheMu.Unlock()
}

// pruneExpiredUsersLocked 删除所有已过期条目，调用方必须持有写锁。
func pruneExpiredUsersLocked(now time.Time) {
	for id, entry := range userCache {
		if now.After(entry.expiresAt) {
			delete(userCache, id)
		}
	}
}

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "Authorization header required")
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			response.Unauthorized(c, "Bearer token required")
			c.Abort()
			return
		}

		claims, err := auth.ValidateToken(tokenString)
		if err != nil {
			response.Unauthorized(c, "Invalid token")
			c.Abort()
			return
		}

		// 优先命中进程内缓存，未命中再查库
		user, ok := getCachedUser(claims.UserID)
		if !ok {
			var dbUser models.User
			if err := database.DB.First(&dbUser, claims.UserID).Error; err != nil {
				response.Unauthorized(c, "User not found")
				c.Abort()
				return
			}
			setCachedUser(&dbUser)
			user = cloneUser(&dbUser)
		}

		c.Set("user", user)
		c.Set("claims", claims)
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := c.Get("user")
		if !exists {
			response.Unauthorized(c, "User not found in context")
			c.Abort()
			return
		}

		u, ok := user.(*models.User)
		if !ok || u == nil {
			response.InternalError(c, "Invalid user type")
			c.Abort()
			return
		}

		if !u.IsAdmin() {
			response.InsufficientPermission(c, "Admin")
			c.Abort()
			return
		}

		c.Next()
	}
}
