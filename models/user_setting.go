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
	AINotifications bool      `gorm:"default:true" json:"ai_notifications"`
	UpdatedAt       time.Time `json:"updated_at"`
}
