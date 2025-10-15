package middleware

import (
	"cloud-platform/internal/auth"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"strings"

	"github.com/gin-gonic/gin"
)

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

		// Get user from database
		var user models.User
		if err := database.DB.First(&user, claims.UserID).Error; err != nil {
			response.Unauthorized(c, "User not found")
			c.Abort()
			return
		}

		c.Set("user", &user)
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
		if !ok {
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
