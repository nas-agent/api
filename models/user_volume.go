package models

import (
	"gorm.io/gorm"
)

type PermissionLevel string

const (
	PermissionLevelRead      PermissionLevel = "Read"
	PermissionLevelReadWrite PermissionLevel = "ReadWrite"
	PermissionLevelAdmin     PermissionLevel = "Admin"
)

type UserVolume struct {
	ID              string          `gorm:"primaryKey" json:"id"`
	UserID          string          `gorm:"index;not null" json:"user_id"`
	VolumeID        string          `gorm:"index;not null" json:"volume_id"`
	PermissionLevel PermissionLevel `gorm:"default:'ReadWrite'" json:"permission_level"`
	CreatedAt       int64           `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       int64           `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"index" json:"-"`

	// Relationships
	User   User   `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"user,omitempty"`
	Volume Volume `gorm:"foreignKey:VolumeID;constraint:OnDelete:CASCADE" json:"volume,omitempty"`
}

// TableName specifies the table name
func (UserVolume) TableName() string {
	return "user_volumes"
}

type UserGroup struct {
	ID          string         `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"not null;uniqueIndex" json:"name"`
	Description string         `json:"description"`
	CreatedAt   int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	Members []GroupMember `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"members,omitempty"`
}

type GroupMember struct {
	ID        string         `gorm:"primaryKey" json:"id"`
	GroupID   string         `gorm:"index;not null" json:"group_id"`
	UserID    string         `gorm:"index;not null" json:"user_id"`
	JoinedAt  int64          `gorm:"autoCreateTime" json:"joined_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	Group UserGroup `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"group,omitempty"`
	User  User      `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"user,omitempty"`
}

type SharePermission struct {
	ID              string          `gorm:"primaryKey" json:"id"`
	ShareID         string          `gorm:"index;not null" json:"share_id"`
	UserID          string          `gorm:"index" json:"user_id"`
	GroupID         string          `gorm:"index" json:"group_id"`
	PermissionLevel PermissionLevel `gorm:"default:'Read'" json:"permission_level"`
	CanShare        bool            `json:"can_share"`
	CreatedAt       int64           `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       int64           `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"index" json:"-"`

	// Relationships
	Share Share     `gorm:"foreignKey:ShareID;constraint:OnDelete:CASCADE" json:"share,omitempty"`
	User  User      `gorm:"foreignKey:UserID;constraint:OnDelete:SET NULL" json:"user,omitempty"`
	Group UserGroup `gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL" json:"group,omitempty"`
}

type StorageQuota struct {
	ID               string         `gorm:"primaryKey" json:"id"`
	UserID           string         `gorm:"index" json:"user_id"`
	ShareID          string         `gorm:"index" json:"share_id"`
	MaxBytes         int64          `gorm:"not null" json:"max_bytes"`           // Maximum storage in bytes
	WarningThreshold int            `gorm:"default:80" json:"warning_threshold"` // Alert at 80% usage
	CreatedAt        int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	User  User  `gorm:"foreignKey:UserID;constraint:OnDelete:SET NULL" json:"user,omitempty"`
	Share Share `gorm:"foreignKey:ShareID;constraint:OnDelete:SET NULL" json:"share,omitempty"`
}
