package models

type ActivityLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Category   string `gorm:"size:16;index;not null" json:"category"`
	UserID     string `gorm:"index" json:"user_id"`
	Username   string `gorm:"index" json:"username"`
	Action     string `gorm:"size:64;index" json:"action"`
	Source     string `gorm:"size:64;index" json:"source"`
	Message    string `gorm:"type:text" json:"message"`
	Method     string `gorm:"size:10" json:"method"`
	Path       string `gorm:"size:255;index" json:"path"`
	StatusCode int    `json:"status_code"`
	CreatedAt  int64  `gorm:"autoCreateTime" json:"created_at"`
}
