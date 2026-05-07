package models

import (
	"gorm.io/gorm"
)

type PublicShare struct {
	ID        string         `gorm:"primaryKey" json:"id"` // Unique token
	FileID    uint           `json:"file_id"`
	FileName  string         `json:"file_name"`
	FilePath  string         `json:"file_path"`
	UserID    string         `json:"user_id"`
	ExpiresAt int64          `json:"expires_at"` // 0 for never
	CreatedAt int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
