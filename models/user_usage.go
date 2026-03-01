package models

type UserUsage struct {
	ID               uint    `gorm:"primaryKey" json:"id"`
	UserID           uint    `gorm:"uniqueIndex;not null" json:"user_id"`
	StorageMB        float64 `gorm:"default:0" json:"storage"`
	AIFileLimitDaily int     `gorm:"default:0" json:"ai_file_limit_daily"`
}
