package models

import (
	"time"
)

type UserSetting struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"uniqueIndex;not null" json:"user_id"`
	Language  string    `gorm:"default:'en'" json:"language"`
	Theme     string    `gorm:"default:'light'" json:"theme"`
	UpdatedAt time.Time `json:"updated_at"`
}
