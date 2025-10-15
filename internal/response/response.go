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
	ErrInvalidRequest   = "INVALID_REQUEST"
	ErrUnauthorized     = "UNAUTHORIZED"
	ErrForbidden        = "FORBIDDEN"
	ErrNotFound         = "NOT_FOUND"
	ErrInternalError    = "INTERNAL_ERROR"
	ErrValidationFailed = "VALIDATION_FAILED"

	// 认证相关错误
	ErrInvalidCredentials = "INVALID_CREDENTIALS"
	ErrTokenExpired       = "TOKEN_EXPIRED"
	ErrTokenInvalid       = "TOKEN_INVALID"

	// 用户相关错误
	ErrUserExists      = "USER_EXISTS"
	ErrUserNotFound    = "USER_NOT_FOUND"
	ErrInvalidPassword = "INVALID_PASSWORD"

	// 企业相关错误
	ErrCompanyNotFound = "COMPANY_NOT_FOUND"
	ErrCompanyDisabled = "COMPANY_DISABLED"
	ErrInvalidCode     = "INVALID_CODE"
	ErrCodeExpired     = "CODE_EXPIRED"
	ErrCodeExhausted   = "CODE_EXHAUSTED"

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

func CompanyDisabled(c *gin.Context) {
	Error(c, http.StatusForbidden, ErrCompanyDisabled, "Your company has been disabled", nil)
}

func InvalidCode(c *gin.Context, codeType string) {
	Error(c, http.StatusBadRequest, ErrInvalidCode, "Invalid "+codeType+" code", nil)
}

func CodeExpired(c *gin.Context) {
	Error(c, http.StatusBadRequest, ErrCodeExpired, "Code has expired", nil)
}

func CodeExhausted(c *gin.Context) {
	Error(c, http.StatusBadRequest, ErrCodeExhausted, "Code has reached maximum usage limit", nil)
}

func InsufficientPermission(c *gin.Context, required string) {
	Error(c, http.StatusForbidden, ErrInsufficientPermission, required+" permission required", nil)
}

func APIAccessDenied(c *gin.Context) {
	Error(c, http.StatusForbidden, ErrAPIAccessDenied, "Access to this API is disabled for your account", nil)
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
