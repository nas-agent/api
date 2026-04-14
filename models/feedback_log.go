package models

type FeedbackLog struct {
	ID             uint   `gorm:"primaryKey" json:"feedback_id"`
	UserID         string `gorm:"index;not null" json:"user_id"`
	FileID         uint   `gorm:"index" json:"file_id"`
	FeedbackType   string `json:"feedback_type"`
	OriginalValue  string `json:"original_value"`
	CorrectedValue string `json:"corrected_value"`
	CreatedAt      int64  `gorm:"autoCreateTime" json:"created_at"`
}
