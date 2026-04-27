package models

type UserUsage struct {
	ID               uint    `gorm:"primaryKey" json:"id"`
	UserID           string    `gorm:"uniqueIndex;not null" json:"user_id"`
	StorageMB        float64 `gorm:"default:0" json:"storage"`
	StorageLimitGB   float64 `gorm:"default:10" json:"storage_limit"`
	AIFileLimitDaily int     `gorm:"default:100" json:"ai_file_limit_daily"`
	AIUsedToday      int     `gorm:"-" json:"ai_used_today"`
}
