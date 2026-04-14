package models

type UserFolderProfile struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	UserID         uint   `gorm:"index:idx_user_folder,unique;not null" json:"user_id"`
	FolderName     string `gorm:"index:idx_user_folder,unique;not null" json:"folder_name"`
	AcceptCount    int    `gorm:"default:0" json:"accept_count"`
	RejectCount    int    `gorm:"default:0" json:"reject_count"`
	KeywordWeights string `gorm:"type:text" json:"keyword_weights"`
	CentroidVector string `gorm:"type:text" json:"centroid_vector"`
	CentroidCount  int    `gorm:"default:0" json:"centroid_count"`
	Description    string `gorm:"type:text" json:"description"`
	LastUsedAt     int64  `gorm:"index" json:"last_used_at"`
	UpdatedAt      int64  `gorm:"autoUpdateTime" json:"updated_at"`
}
