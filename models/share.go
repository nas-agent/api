package models

import (
	"gorm.io/gorm"
)

type ShareType string

const (
	ShareTypePublic  ShareType = "Public"
	ShareTypePrivate ShareType = "Private"
)

type Share struct {
	ID         string         `gorm:"primaryKey" json:"id"`
	Name       string         `gorm:"not null" json:"name"`
	Path       string         `gorm:"not null" json:"path"`
	VolumeID   string         `json:"volume_id"`
	Type       ShareType      `gorm:"default:'Public'" json:"type"`
	OwnerID    string         `json:"owner_id"` // UUID of the user
	Status     string         `gorm:"default:'Active'" json:"status"`
	Protocol   string         `gorm:"default:'SMB'" json:"protocol"`
	CreatedAt  int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}
