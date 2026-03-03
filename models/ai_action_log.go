package models

type AIActionLog struct {
	LogID       uint   `gorm:"primaryKey" json:"log_id"`
	UserID      uint   `gorm:"not null" json:"user_id"`
	Action      string `gorm:"type:text;not null" json:"action"`
	Description string `json:"description"`
	Folder      string `json:"folder"`
	Filename    string `json:"filename"`
	IsMove      bool   `json:"is_move"`
	CreatedAt   int64  `gorm:"autoCreateTime" json:"timestamp"`
}
