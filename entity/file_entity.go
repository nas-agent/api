package entity

import (
	"time"

	"gorm.io/gorm"
)

// File represents a file stored in the database
type File struct {
	gorm.Model
	Name     string `json:"name" gorm:"index"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	UserID   uint   `json:"user_id" gorm:"index"`
}

// FileItem represents a file item for API responses (non-database)
type FileItem struct {
	Name       string    `json:"name"`
	FullPath   string    `json:"full_path"`
	ModifiedAt time.Time `json:"modified_at"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
}
