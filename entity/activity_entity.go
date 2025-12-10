package entity

import "gorm.io/gorm"

// UserActivity tracks user file access and AI operations
type UserActivity struct {
	gorm.Model
	UserID       uint   `json:"user_id" gorm:"index"`
	ActivityType string `json:"activity_type"` // file_view, folder_access, ai_sort
	FilePath     string `json:"file_path"`
	FileName     string `json:"file_name"`
}

// UserActivityDTO for API responses
type UserActivityDTO struct {
	ID           uint   `json:"id"`
	UserID       uint   `json:"user_id"`
	ActivityType string `json:"activity_type"`
	FilePath     string `json:"file_path"`
	FileName     string `json:"file_name"`
	Timestamp    string `json:"timestamp"`
}

// ToDTO converts entity to DTO
func (a *UserActivity) ToDTO() UserActivityDTO {
	return UserActivityDTO{
		ID:           a.ID,
		UserID:       a.UserID,
		ActivityType: a.ActivityType,
		FilePath:     a.FilePath,
		FileName:     a.FileName,
		Timestamp:    a.CreatedAt.Format("2006-01-02 15:04:05"),
	}
}
