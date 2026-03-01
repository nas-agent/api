package models

type AIActionLog struct {
	LogID  uint   `gorm:"primaryKey" json:"log_id"`
	UserID uint   `gorm:"not null" json:"user_id"`
	Action string `gorm:"type:text;not null;check:action IN ('move_file', 'rename', 'scan', 'user_feedback')" json:"action"`
}
