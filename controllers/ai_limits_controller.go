package controllers

import (
	"api/config"
	"api/entity"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetUserLimits - GET /api/ai/limits/users
func GetUserLimits(c *gin.Context) {
	db := config.DB

	var limits []entity.AILimit
	db.Where("target_type = ?", "user").Find(&limits)

	// Get all users to create limits if they don't exist
	var users []entity.User
	db.Find(&users)

	// Create map of existing limits
	limitMap := make(map[string]*entity.AILimit)
	for i := range limits {
		limitMap[limits[i].TargetID] = &limits[i]
	}

	var result []entity.AILimitDTO
	for _, user := range users {
		userIDStr := fmt.Sprintf("%d", user.ID)

		if limit, exists := limitMap[userIDStr]; exists {
			result = append(result, limit.ToDTO())
		} else {
			// Create default limit for user
			newLimit := entity.AILimit{
				TargetType:     "user",
				TargetID:       userIDStr,
				TargetName:     user.Username,
				DailyLimit:     200,
				FilesProcessed: 0,
				Status:         "Active",
			}
			db.Create(&newLimit)
			result = append(result, newLimit.ToDTO())
		}
	}

	c.JSON(http.StatusOK, result)
}

// UpdateUserLimit - PUT /api/ai/limits/users/:id
func UpdateUserLimit(c *gin.Context) {
	db := config.DB
	limitID := c.Param("id")

	var dto struct {
		DailyLimit int `json:"dailyLimit"`
	}

	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var limit entity.AILimit
	if err := db.First(&limit, limitID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Limit not found"})
		return
	}

	limit.DailyLimit = dto.DailyLimit
	db.Save(&limit)

	c.JSON(http.StatusOK, limit.ToDTO())
}

// GetFolderLimits - GET /api/ai/limits/folders
func GetFolderLimits(c *gin.Context) {
	db := config.DB

	var limits []entity.AILimit
	db.Where("target_type = ?", "folder").Find(&limits)

	// If no folder limits exist, create some defaults
	if len(limits) == 0 {
		defaultFolders := []struct {
			name   string
			limit  int
			status string
		}{
			{"Public/Photos", 1000, "Active"},
			{"Public/Documents", 500, "Active"},
			{"Backups/Logs", 0, "Paused"},
		}

		for _, folder := range defaultFolders {
			newLimit := entity.AILimit{
				TargetType:     "folder",
				TargetID:       folder.name,
				TargetName:     folder.name,
				DailyLimit:     folder.limit,
				FilesProcessed: 0,
				Status:         folder.status,
			}
			db.Create(&newLimit)
			limits = append(limits, newLimit)
		}
	}

	var result []entity.AILimitDTO
	for _, limit := range limits {
		result = append(result, limit.ToDTO())
	}

	c.JSON(http.StatusOK, result)
}

// UpdateFolderLimit - PUT /api/ai/limits/folders/:id
func UpdateFolderLimit(c *gin.Context) {
	db := config.DB
	limitID := c.Param("id")

	var dto struct {
		DailyLimit int    `json:"dailyLimit"`
		Status     string `json:"status"`
	}

	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var limit entity.AILimit
	if err := db.First(&limit, limitID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Limit not found"})
		return
	}

	if dto.DailyLimit >= 0 {
		limit.DailyLimit = dto.DailyLimit
	}
	if dto.Status != "" {
		limit.Status = dto.Status
	}

	db.Save(&limit)

	c.JSON(http.StatusOK, limit.ToDTO())
}

// GetGlobalAIConfig - GET /api/ai/limits/global
func GetGlobalAIConfig(c *gin.Context) {
	db := config.DB

	var globalConfig entity.GlobalAIConfig
	result := db.First(&globalConfig)

	// If no global config exists, create default
	if result.Error != nil {
		globalConfig = entity.GlobalAIConfig{
			MaxConcurrentTasks: 2,
			ProcessingWindow:   "Always",
			AutoPauseOnHighCPU: true,
		}
		db.Create(&globalConfig)
	}

	c.JSON(http.StatusOK, globalConfig.ToDTO())
}

// UpdateGlobalAIConfig - PUT /api/ai/limits/global
func UpdateGlobalAIConfig(c *gin.Context) {
	db := config.DB

	var dto entity.GlobalAIConfigDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var globalConfig entity.GlobalAIConfig
	result := db.First(&globalConfig)

	if result.Error != nil {
		// Create new
		globalConfig = entity.GlobalAIConfig{}
	}

	globalConfig.MaxConcurrentTasks = dto.MaxConcurrentTasks
	globalConfig.ProcessingWindow = dto.ProcessingWindow
	globalConfig.AutoPauseOnHighCPU = dto.AutoPauseOnHighCPU

	if globalConfig.ID == 0 {
		db.Create(&globalConfig)
	} else {
		db.Save(&globalConfig)
	}

	c.JSON(http.StatusOK, globalConfig.ToDTO())
}

// IncrementProcessedFiles - Helper function to increment file processing count
func IncrementProcessedFiles(targetType, targetID string) {
	db := config.DB

	var limit entity.AILimit
	if err := db.Where("target_type = ? AND target_id = ?", targetType, targetID).First(&limit).Error; err != nil {
		return // Limit doesn't exist, skip
	}

	limit.FilesProcessed++
	now := time.Now()
	limit.LastProcessedAt = &now

	// Check if should throttle
	if limit.DailyLimit > 0 && limit.FilesProcessed >= limit.DailyLimit {
		limit.Status = "Throttled"
	}

	db.Save(&limit)
}

// ResetDailyCounters - Should be called daily via cron job
func ResetDailyCounters(c *gin.Context) {
	db := config.DB

	db.Model(&entity.AILimit{}).Where("files_processed > 0").Updates(map[string]interface{}{
		"files_processed": 0,
		"status":          "Active",
	})

	c.JSON(http.StatusOK, gin.H{"message": "Daily counters reset successfully"})
}
