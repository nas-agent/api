package models

type AIActionLog struct {
	LogID       uint   `gorm:"primaryKey" json:"log_id"`
	UserID      string `gorm:"not null" json:"user_id"`
	FileID      uint   `gorm:"index" json:"file_id"`
	Action      string `gorm:"type:text;not null" json:"action"`
	Description string `json:"description"`
	Folder      string `json:"folder"`
	Filename     string `json:"filename"`
	OriginalPath string `json:"original_path"`
	IsMove       bool   `json:"is_move"`
	Confidence   int    `json:"confidence"`
	Status       string `gorm:"type:string;default:pending" json:"status"`
	CreatedAt    int64  `gorm:"autoCreateTime" json:"timestamp"`
}
