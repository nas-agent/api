package entity

import (
	"time"

	"gorm.io/gorm"
)

// AILimit represents resource limits for users or folders
type AILimit struct {
	gorm.Model
	TargetType      string     `json:"target_type" gorm:"index"` // "user" or "folder"
	TargetID        string     `json:"target_id" gorm:"index"`
	TargetName      string     `json:"target_name"`
	DailyLimit      int        `json:"daily_limit" gorm:"default:200"`
	FilesProcessed  int        `json:"files_processed" gorm:"default:0"`
	LastProcessedAt *time.Time `json:"last_processed_at"`
	Status          string     `json:"status" gorm:"default:'Active'"` // Active, Throttled, Paused
}

// AILimitDTO for API responses
type AILimitDTO struct {
	ID             uint   `json:"id"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	FilesProcessed int    `json:"filesProcessed"`
	DailyLimit     int    `json:"dailyLimit"`
	LastActive     string `json:"lastActive"`
	Status         string `json:"status"`
}

// ToDTO converts entity to DTO
func (l *AILimit) ToDTO() AILimitDTO {
	lastActive := "Never"
	if l.LastProcessedAt != nil {
		elapsed := time.Since(*l.LastProcessedAt)
		if elapsed.Minutes() < 1 {
			lastActive = "Just now"
		} else if elapsed.Hours() < 1 {
			lastActive = time.Now().Sub(*l.LastProcessedAt).Round(time.Minute).String() + " ago"
		} else if elapsed.Hours() < 24 {
			hours := int(elapsed.Hours())
			lastActive = time.Now().Sub(*l.LastProcessedAt).Round(time.Hour).String() + " ago"
			if hours == 1 {
				lastActive = "1 hour ago"
			} else {
				lastActive = string(rune(hours)) + " hours ago"
			}
		} else {
			lastActive = l.LastProcessedAt.Format("2006-01-02")
		}
	}

	targetType := "User"
	if l.TargetType == "folder" {
		targetType = "Folder"
	}

	return AILimitDTO{
		ID:             l.ID,
		Type:           targetType,
		Name:           l.TargetName,
		FilesProcessed: l.FilesProcessed,
		DailyLimit:     l.DailyLimit,
		LastActive:     lastActive,
		Status:         l.Status,
	}
}

// GlobalAIConfig represents system-wide AI settings
type GlobalAIConfig struct {
	gorm.Model
	MaxConcurrentTasks int    `json:"max_concurrent_tasks" gorm:"default:2"`
	ProcessingWindow   string `json:"processing_window" gorm:"default:'Always'"`
	AutoPauseOnHighCPU bool   `json:"auto_pause_on_high_cpu" gorm:"default:true"`
}

// GlobalAIConfigDTO for API responses
type GlobalAIConfigDTO struct {
	MaxConcurrentTasks int    `json:"maxConcurrentTasks"`
	ProcessingWindow   string `json:"processingWindow"`
	AutoPauseOnHighCPU bool   `json:"autoPauseOnHighCpu"`
}

// ToDTO converts entity to DTO
func (g *GlobalAIConfig) ToDTO() GlobalAIConfigDTO {
	return GlobalAIConfigDTO{
		MaxConcurrentTasks: g.MaxConcurrentTasks,
		ProcessingWindow:   g.ProcessingWindow,
		AutoPauseOnHighCPU: g.AutoPauseOnHighCPU,
	}
}
