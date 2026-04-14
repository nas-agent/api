package models

type DecisionEvent struct {
	ID                uint   `gorm:"primaryKey" json:"id"`
	UserID            string `gorm:"index;not null" json:"user_id"`
	FileID            uint   `gorm:"index" json:"file_id"`
	Source            string `gorm:"size:32;index" json:"source"`
	Outcome           string `gorm:"size:64;index" json:"outcome"`
	ReasonCode        string `gorm:"size:64;index" json:"reason_code"`
	SuggestedFolder   string `gorm:"index" json:"suggested_folder"`
	FinalFolder       string `gorm:"index" json:"final_folder"`
	SuggestedFileName string `json:"suggested_file_name"`
	FinalFileName     string `json:"final_file_name"`
	ConfidenceScore   int    `json:"confidence_score"`
	MetadataJSON      string `gorm:"type:text" json:"metadata_json"`
	CreatedAt         int64  `gorm:"autoCreateTime" json:"created_at"`
}
