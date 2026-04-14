package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID                 string         `gorm:"primaryKey;type:uuid" json:"user_id"`
	Username           string         `gorm:"uniqueIndex;not null" json:"username"`
	Email              string         `gorm:"uniqueIndex;not null" json:"email"`
	Password           string         `gorm:"not null" json:"-"`
	Role               string         `gorm:"default:'user'" json:"role"`
	PersonalFolderPath string         `json:"personal_folder_path"`
	CreatedAt          int64          `gorm:"autoCreateTime" json:"created_date"`
	UpdatedAt          int64          `gorm:"autoUpdateTime" json:"updated_date"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_date"`

	// Relationships
	Usage        UserUsage      `gorm:"foreignKey:UserID" json:"usage"`
	AIConfig     UserAIConfig   `gorm:"foreignKey:UserID" json:"ai_config"`
	Setting      UserSetting    `gorm:"foreignKey:UserID" json:"setting"`
	Files        []FileMetadata `gorm:"foreignKey:OwnerID" json:"files"`
	FeedbackLogs []FeedbackLog  `gorm:"foreignKey:UserID" json:"feedback_logs"`
}

// BeforeCreate hook to generate a UUID before moving to DB
func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return
}
