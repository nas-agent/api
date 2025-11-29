package entity

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Username string   `json:"username"`
	Password string   `json:"password"`
	Email    string   `json:"email"`
	Role     UserRole `json:"role" gorm:"default:'user'"`
}

type UserUsage struct {
	gorm.Model
	UserID       uint  `json:"user_id"`
	StorageUsed  int64 `json:"storage_used"`
	StorageLimit int64 `json:"storage_limit"`
	AiUsage      int64 `json:"ai_usage"`
	AiLimit      int64 `json:"ai_limit"`
}

type UserFavorite struct {
	gorm.Model
	UserID uint   `json:"user_id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

type CreateUserDTO struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}
