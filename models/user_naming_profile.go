package models

type UserNamingProfile struct {
	ID                uint   `gorm:"primaryKey" json:"id"`
	UserID            uint   `gorm:"uniqueIndex;not null" json:"user_id"`
	PreferredStyle    string `gorm:"default:'opt2'" json:"preferred_style"`
	PreferredLanguage string `gorm:"default:'auto'" json:"preferred_language"`
	DateFormat        string `gorm:"default:'2006-01-02'" json:"date_format"`
	Separator         string `gorm:"default:'_'" json:"separator"`
	PatternScores     string `gorm:"type:text" json:"pattern_scores"`
	UpdatedAt         int64  `gorm:"autoUpdateTime" json:"updated_at"`
}
