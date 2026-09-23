package handlers

import (
	"sync"

	"cloud-platform/internal/auth"
	"cloud-platform/internal/database"
	"cloud-platform/internal/middleware"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// networkProvisionMu 串行化「读取既有分配 → 分配端口/网段 → 建网 → 落库」这一整段。
//
// 见 Register 中的说明：分配依赖数据库快照与宿主机端口探测，二者都无法表达
// 「正在被分配」的中间状态，故用进程级互斥消除并发注册的撞车窗口。
var networkProvisionMu sync.Mutex

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
	networkService := services.NewUserNetworkServiceFromRuntime()

	// 分配端口与网段前先取回已被占用的资源，避免与既有账号冲突。
	//
	// 从这次读取到下面建网完成，整段必须是原子的：每一步单独看都安全，组合起来却有窗口——
	// 并发注册会读到同一份分配快照、选中同一个监听端口与网段，随后在建网阶段撞车
	// （后者在宿主命名空间绑定同一端口失败，报出一个对用户毫无意义的错误）。
	// 分配依据是「数据库快照 + 宿主机端口占用探测」，两者都无法表达「正在被分配」
	// 这一瞬间状态，因此用进程级互斥把临界区串起来。本服务为单进程部署，进程内锁足够。
	networkProvisionMu.Lock()
	defer networkProvisionMu.Unlock()

	var existingServers []models.WireguardServer
	if err := database.DB.Find(&existingServers).Error; err != nil {
		database.DB.Delete(&user)
		response.InternalError(c, "Failed to read existing network allocations")
		return
	}

	wgServer, err := networkService.ProvisionUserNetwork(&user, services.AllocationsFromServers(existingServers))
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
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	response.Success(c, "User information retrieved successfully", u.ToResponse())
}

type UpdateProfileRequest struct {
	Name     string `json:"name,omitempty"`
	Password string `json:"password,omitempty"`
	// CurrentPassword 修改密码时必须提供。
	//
	// 仅凭持有 token 就能改密意味着：token 一旦泄漏（前端存在 localStorage 中），
	// 攻击者可以改掉密码完成账号接管，而真正的用户连「密码被改过」都不会察觉。
	CurrentPassword string `json:"current_password,omitempty"`
}

func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}

	if req.Password != "" {
		if len(req.Password) < 6 {
			response.BadRequest(c, "Password must be at least 6 characters", nil)
			return
		}
		// 改密必须验证旧密码，避免 token 泄漏直接升级为账号接管
		if req.CurrentPassword == "" {
			response.BadRequest(c, "Current password is required to change the password", nil)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			response.Unauthorized(c, "Current password is incorrect")
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

	// 资料/密码变更后立即失效用户缓存，避免 TTL 窗口内读到旧快照
	middleware.InvalidateUserCache(u.ID)

	// Reload user
	database.DB.First(u, u.ID)

	response.Success(c, "Profile updated successfully", u.ToResponse())
}
