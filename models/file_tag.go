package models

type FileTag struct {
	ID        uint   `gorm:"primaryKey" json:"tag_id"`
	FileID    uint   `gorm:"index;not null" json:"file_id"`
	TagName   string `gorm:"not null" json:"tag_name"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"created_at"`
}
