package handlers

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UpdateUserRequest struct {
	Name     string          `json:"name,omitempty"`
	Email    string          `json:"email,omitempty"`
	Password string          `json:"password,omitempty"`
	Role     models.UserRole `json:"role,omitempty"`
}

func GetAllUsers(c *gin.Context) {
	var users []models.User
	if err := database.DB.Order("created_at DESC").Find(&users).Error; err != nil {
		response.InternalError(c, "Failed to fetch users")
		return
	}

	var userResponses []models.UserResponse
	for _, user := range users {
		userResponses = append(userResponses, user.ToResponse())
	}

	response.Success(c, "Users retrieved successfully", userResponses)
}

func DeleteUser(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid user ID", nil)
		return
	}

	currentUser, _ := c.Get("user")
	cu := currentUser.(*models.User)

	// Cannot delete yourself
	if uint(userID) == cu.ID {
		response.BadRequest(c, "Cannot delete yourself", nil)
		return
	}

	var targetUser models.User
	if err := database.DB.First(&targetUser, userID).Error; err != nil {
		response.NotFound(c, "User not found")
		return
	}

	// Cannot delete other admins
	if targetUser.IsAdmin() {
		response.BadRequest(c, "Cannot delete admin", nil)
		return
	}

	// 清理用户的网络资源
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", targetUser.ID).First(&wgServer).Error; err == nil {
		networkService := services.NewUserNetworkService(
			config.AppConfig.Network.ConfigDir,
			config.AppConfig.Network.BaseSubnet,
			config.AppConfig.Network.BasePort,
			config.AppConfig.Network.OutInterface,
		)
		
		// 删除所有peers
		database.DB.Where("server_id = ?", wgServer.ID).Delete(&models.WireguardPeer{})
		
		// 清理网络环境（忽略错误）
		networkService.DestroyUserNetwork(&wgServer, targetUser.UserUID)
		
		// 删除服务器记录
		database.DB.Delete(&wgServer)
	}

	if err := database.DB.Delete(&targetUser).Error; err != nil {
		response.InternalError(c, "Failed to delete user")
		return
	}

	response.Success(c, "User deleted successfully", nil)
}

func UpdateUser(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid user ID", nil)
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	var targetUser models.User
	if err := database.DB.First(&targetUser, userID).Error; err != nil {
		response.NotFound(c, "User not found")
		return
	}

	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}

	if req.Email != "" {
		// Check if email is already taken
		var existingUser models.User
		if err := database.DB.Where("email = ? AND id != ?", req.Email, userID).First(&existingUser).Error; err == nil {
			response.BadRequest(c, "Email already exists", nil)
			return
		}
		updates["email"] = req.Email
	}

	if req.Password != "" {
		if len(req.Password) < 6 {
			response.BadRequest(c, "Password must be at least 6 characters", nil)
			return
		}
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			response.InternalError(c, "Failed to hash password")
			return
		}
		updates["password_hash"] = string(hashedPassword)
	}

	if req.Role != "" {
		// Cannot change role of admins
		if targetUser.IsAdmin() && req.Role != models.RoleAdmin {
			response.BadRequest(c, "Cannot change role of admin", nil)
			return
		}
		updates["role"] = req.Role
	}

	if len(updates) == 0 {
		response.BadRequest(c, "No valid fields to update", nil)
		return
	}

	if err := database.DB.Model(&targetUser).Updates(updates).Error; err != nil {
		response.InternalError(c, "Failed to update user")
		return
	}

	// Reload user
	database.DB.First(&targetUser, targetUser.ID)

	response.Success(c, "User updated successfully", targetUser.ToResponse())
}
