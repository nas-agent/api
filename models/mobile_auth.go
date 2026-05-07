package models

import (
	"gorm.io/gorm"
)

type MobileAuthToken struct {
	Token     string         `gorm:"primaryKey;type:varchar(64)" json:"token"` // Randomly generated string
	UserID    string         `gorm:"not null;index" json:"user_id"`
	ExpiresAt int64          `gorm:"not null;index" json:"expires_at"` // Unix timestamp
	CreatedAt int64          `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
