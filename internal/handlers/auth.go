package handlers

import (
	"cloud-platform/internal/auth"
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token string              `json:"token"`
	User  models.UserResponse `json:"user"`
}

func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	// Check if user already exists
	var existingUser models.User
	if err := database.DB.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		response.UserExists(c)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		response.InternalError(c, "Failed to hash password")
		return
	}

	user := models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Name:         req.Name,
		Role:         models.RoleNormalUser,
	}

	if err := database.DB.Create(&user).Error; err != nil {
		response.InternalError(c, "Failed to create user")
		return
	}

	// Load user for response
	database.DB.First(&user, user.ID)

	// 为用户配置网络环境（命名空间 + WireGuard）
	networkService := services.NewUserNetworkService(
		config.AppConfig.Network.ConfigDir,
		config.AppConfig.Network.BaseSubnet,
		config.AppConfig.Network.BasePort,
		config.AppConfig.Network.OutInterface,
	)

	wgServer, err := networkService.ProvisionUserNetwork(&user)
	if err != nil {
		// 网络配置失败，回滚用户创建
		database.DB.Delete(&user)
		response.InternalError(c, "Failed to provision user network: "+err.Error())
		return
	}

	// 保存 WireGuard 服务器配置到数据库
	if err := database.DB.Create(wgServer).Error; err != nil {
		// 清理网络环境
		networkService.DestroyUserNetwork(wgServer, user.UserUID)
		database.DB.Delete(&user)
		response.InternalError(c, "Failed to save user network info")
		return
	}

	response.Created(c, "User registered successfully", user.ToResponse())
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	var user models.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		response.InvalidCredentials(c)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		response.InvalidCredentials(c)
		return
	}

	token, err := auth.GenerateToken(&user)
	if err != nil {
		response.InternalError(c, "Failed to generate token")
		return
	}

	response.Success(c, "Login successful", LoginResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

func GetMe(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*models.User)

	response.Success(c, "User information retrieved successfully", u.ToResponse())
}

type UpdateProfileRequest struct {
	Name     string `json:"name,omitempty"`
	Password string `json:"password,omitempty"`
}

func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	user, _ := c.Get("user")
	u := user.(*models.User)

	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
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

	if len(updates) == 0 {
		response.BadRequest(c, "No valid fields to update", nil)
		return
	}

	if err := database.DB.Model(u).Updates(updates).Error; err != nil {
		response.InternalError(c, "Failed to update profile")
		return
	}

	// Reload user
	database.DB.First(u, u.ID)

	response.Success(c, "Profile updated successfully", u.ToResponse())
}
