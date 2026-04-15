package models

import (
	"gorm.io/gorm"
)

type VolumeStatus string

const (
	VolumeStatusMounted   VolumeStatus = "Mounted"
	VolumeStatusUnmounted VolumeStatus = "Unmounted"
	VolumeStatusError     VolumeStatus = "Error"
)

type Volume struct {
	ID         string         `gorm:"primaryKey" json:"id"`
	MountPoint string         `gorm:"uniqueIndex;not null" json:"mount_point"`
	DevicePath string         `gorm:"not null" json:"device_path"`
	FileSystem string         `json:"file_system"`
	TotalSize  int64          `json:"total_size"` // bytes
	UsedSize   int64          `json:"used_size"`  // bytes
	Status     VolumeStatus   `gorm:"default:'Mounted'" json:"status"`
	CreatedAt  int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	Shares []Share `gorm:"foreignKey:VolumeID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"shares"`
}
