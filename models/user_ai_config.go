package models

type UserAIConfig struct {
	ID               uint    `gorm:"primaryKey" json:"id"`
	UserID           string    `gorm:"uniqueIndex;not null" json:"user_id"`
	ConfidenceAuto   float64 `gorm:"default:0.8" json:"confidence_auto"`
	ConfidenceReject float64 `gorm:"default:0.4" json:"confidence_reject"`
	OriginPath       string  `gorm:"not null" json:"origin_path"`
	DestinationPath  string  `gorm:"not null" json:"destination_path"`
	RenameFile       bool    `gorm:"default:false" json:"rename_file"`
	RenameFormat     string  `gorm:"default:'opt1'" json:"rename_format"`
	AutoSelectFolder bool    `gorm:"default:false" json:"auto_select_folder"`
	Active           bool    `gorm:"default:false" json:"active"`
	AnalysisProvider string  `gorm:"default:'local'" json:"analysis_provider"`
	GeminiAPIKey     string  `gorm:"type:text" json:"gemini_api_key"`
	GeminiModel      string  `gorm:"default:'gemini-2.0-flash'" json:"gemini_model"`
	WindowsDriveMapping string  `gorm:"type:varchar(10);default:''" json:"windows_drive_mapping"`
}
