package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// 统一响应格式
type Response struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorInfo  `json:"error,omitempty"`
	Timestamp int64       `json:"timestamp"`
	RequestID string      `json:"request_id,omitempty"`
}

// 错误信息详情
type ErrorInfo struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

// 分页响应数据
type PaginatedData struct {
	Items      interface{}     `json:"items"`
	Pagination *PaginationInfo `json:"pagination"`
}

// 分页信息
type PaginationInfo struct {
	CurrentPage int   `json:"current_page"`
	PerPage     int   `json:"per_page"`
	TotalPages  int   `json:"total_pages"`
	TotalItems  int64 `json:"total_items"`
	HasNext     bool  `json:"has_next"`
	HasPrev     bool  `json:"has_prev"`
}

// 错误代码常量
const (
	// 通用错误
	ErrInvalidRequest     = "INVALID_REQUEST"
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrForbidden          = "FORBIDDEN"
	ErrNotFound           = "NOT_FOUND"
	ErrInternalError      = "INTERNAL_ERROR"
	ErrValidationFailed   = "VALIDATION_FAILED"
	ErrTooManyRequests    = "TOO_MANY_REQUESTS"
	ErrServiceUnavailable = "SERVICE_UNAVAILABLE"

	// 认证相关错误
	ErrInvalidCredentials = "INVALID_CREDENTIALS"
	ErrTokenExpired       = "TOKEN_EXPIRED"
	ErrTokenInvalid       = "TOKEN_INVALID"

	// 用户相关错误
	ErrUserExists      = "USER_EXISTS"
	ErrUserNotFound    = "USER_NOT_FOUND"
	ErrInvalidPassword = "INVALID_PASSWORD"

	// 权限相关错误
	ErrInsufficientPermission = "INSUFFICIENT_PERMISSION"
	ErrAPIAccessDenied        = "API_ACCESS_DENIED"
)

// 成功响应
func Success(c *gin.Context, message string, data interface{}) {
	response := Response{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Unix(),
		RequestID: getRequestID(c),
	}
	c.JSON(http.StatusOK, response)
}

// 创建成功响应
func Created(c *gin.Context, message string, data interface{}) {
	response := Response{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Unix(),
		RequestID: getRequestID(c),
	}
	c.JSON(http.StatusCreated, response)
}

// 分页成功响应
func SuccessWithPagination(c *gin.Context, message string, items interface{}, pagination *PaginationInfo) {
	data := PaginatedData{
		Items:      items,
		Pagination: pagination,
	}
	response := Response{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Unix(),
		RequestID: getRequestID(c),
	}
	c.JSON(http.StatusOK, response)
}

// 错误响应
func Error(c *gin.Context, statusCode int, errorCode, message string, details interface{}) {
	response := Response{
		Success: false,
		Message: "Request failed",
		Error: &ErrorInfo{
			Code:    errorCode,
			Message: message,
			Details: details,
		},
		Timestamp: time.Now().Unix(),
		RequestID: getRequestID(c),
	}
	c.JSON(statusCode, response)
}

// 常用错误响应快捷方法
func BadRequest(c *gin.Context, message string, details interface{}) {
	Error(c, http.StatusBadRequest, ErrInvalidRequest, message, details)
}

func Unauthorized(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, ErrUnauthorized, message, nil)
}

func Forbidden(c *gin.Context, message string) {
	Error(c, http.StatusForbidden, ErrForbidden, message, nil)
}

func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, ErrNotFound, message, nil)
}

func InternalError(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, ErrInternalError, message, nil)
}

func TooManyRequests(c *gin.Context, message string) {
	Error(c, http.StatusTooManyRequests, ErrTooManyRequests, message, nil)
}

func ServiceUnavailable(c *gin.Context, message string) {
	Error(c, http.StatusServiceUnavailable, ErrServiceUnavailable, message, nil)
}

func ValidationError(c *gin.Context, details interface{}) {
	Error(c, http.StatusBadRequest, ErrValidationFailed, "Validation failed", details)
}

// 特定业务错误
func InvalidCredentials(c *gin.Context) {
	Error(c, http.StatusUnauthorized, ErrInvalidCredentials, "Invalid email or password", nil)
}

func UserExists(c *gin.Context) {
	Error(c, http.StatusBadRequest, ErrUserExists, "User with this email already exists", nil)
}

func InsufficientPermission(c *gin.Context, required string) {
	Error(c, http.StatusForbidden, ErrInsufficientPermission, required+" permission required", nil)
}

func APIAccessDenied(c *gin.Context) {
	Error(c, http.StatusForbidden, ErrAPIAccessDenied, "Access to this API is disabled for your account", nil)
}

// 分页参数边界
const (
	// MinPerPage 每页最少返回条数
	MinPerPage = 1
	// MaxPerPage 每页最多返回条数，防止单次查询打爆内存
	MaxPerPage = 200
)

// NewPagination 构造分页信息，自动纠正非法入参：
// page < 1 归一为 1；perPage 收敛到 [MinPerPage, MaxPerPage]。
func NewPagination(page, perPage int, total int64) *PaginationInfo {
	if page < 1 {
		page = 1
	}
	if perPage < MinPerPage {
		perPage = MinPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	totalPages := int((total + int64(perPage) - 1) / int64(perPage))

	return &PaginationInfo{
		CurrentPage: page,
		PerPage:     perPage,
		TotalPages:  totalPages,
		TotalItems:  total,
		HasNext:     page < totalPages,
		HasPrev:     page > 1,
	}
}

// 获取请求ID（如果有的话）
func getRequestID(c *gin.Context) string {
	if requestID := c.GetHeader("X-Request-ID"); requestID != "" {
		return requestID
	}
	if requestID := c.GetString("request_id"); requestID != "" {
		return requestID
	}
	return ""
}
