package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

type UserRole string

const (
	RoleAdmin      UserRole = "admin"
	RoleNormalUser UserRole = "normal_user"
)

type User struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	UserUID      string    `json:"user_uid" gorm:"uniqueIndex;size:16;not null"` // 用户唯一短ID
	Email        string    `json:"email" gorm:"uniqueIndex;not null"`
	PasswordHash string    `json:"-" gorm:"not null"`
	Name         string    `json:"name" gorm:"not null"`
	Role         UserRole  `json:"role" gorm:"default:'normal_user'"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserResponse struct {
	ID        uint      `json:"id"`
	UserUID   string    `json:"user_uid"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      UserRole  `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:        u.ID,
		UserUID:   u.UserUID,
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
	}
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.Role == "" {
		u.Role = RoleNormalUser
	}
	
	// 生成唯一的短ID
	if u.UserUID == "" {
		u.UserUID = generateUserUID()
	}
	
	return nil
}

// generateUserUID 生成8字符的唯一用户ID
func generateUserUID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}
