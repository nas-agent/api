package models

import (
	"time"
)

type UserSetting struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	UserID          string      `gorm:"uniqueIndex;not null" json:"user_id"`
	Language        string    `gorm:"default:'en'" json:"language"`
	Theme           string    `gorm:"default:'light'" json:"theme"`
	LaunchOnStartup bool      `gorm:"default:false" json:"launch_on_startup"`
	AINotifications     bool      `gorm:"default:true" json:"ai_notifications"`
	RemoteAccessEnabled bool      `gorm:"default:false" json:"remote_access_enabled"`
	PublicDomain        string    `gorm:"type:text" json:"public_domain"`  // e.g. https://api.yournas.com
	MobileAppURL        string    `gorm:"type:text" json:"mobile_app_url"` // e.g. https://nas-mobile.vercel.app
	RegistryName        string    `gorm:"type:string;uniqueIndex" json:"registry_name"` // e.g. roodfaan-nas-01
	UpdatedAt           time.Time `json:"updated_at"`
}
