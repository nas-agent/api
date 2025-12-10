package controllers

import (
	"api/config"
	"api/entity"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetRecentActivity - GET /api/activity/recent
func GetRecentActivity(c *gin.Context) {
	db := config.DB
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username parameter required"})
		return
	}

	// Get user ID
	var user entity.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var activities []entity.UserActivity
	db.Where("user_id = ?", user.ID).Order("created_at DESC").Limit(10).Find(&activities)

	var result []entity.UserActivityDTO
	for _, activity := range activities {
		result = append(result, activity.ToDTO())
	}

	c.JSON(http.StatusOK, result)
}

// TrackActivity - POST /api/activity/track
func TrackActivity(c *gin.Context) {
	db := config.DB

	var dto struct {
		UserID       uint   `json:"user_id"`
		ActivityType string `json:"activity_type"`
		FilePath     string `json:"file_path"`
		FileName     string `json:"file_name"`
	}

	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	activity := entity.UserActivity{
		UserID:       dto.UserID,
		ActivityType: dto.ActivityType,
		FilePath:     dto.FilePath,
		FileName:     dto.FileName,
	}

	db.Create(&activity)

	c.JSON(http.StatusOK, activity.ToDTO())
}

// GetRecentFileViews - GET /api/files/recent-views (replaces mock)
func GetRecentFileViews(c *gin.Context) {
	db := config.DB
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username parameter required"})
		return
	}

	// Get user ID
	var user entity.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var activities []entity.UserActivity
	db.Where("user_id = ? AND activity_type = ?", user.ID, "file_view").
		Order("created_at DESC").
		Limit(5).
		Find(&activities)

	type RecentView struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		AccessedAt string `json:"accessedAt"`
	}

	var result []RecentView
	for _, activity := range activities {
		result = append(result, RecentView{
			Name:       activity.FileName,
			Path:       activity.FilePath,
			AccessedAt: formatTimeAgo(activity.CreatedAt),
		})
	}

	c.JSON(http.StatusOK, result)
}

// GetRecommendedFoldersFromActivity - GET /api/files/recommended (replaces mock)
func GetRecommendedFoldersFromActivity(c *gin.Context) {
	db := config.DB
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username parameter required"})
		return
	}

	// Get user ID
	var user entity.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Get most accessed folders
	type FolderCount struct {
		Path  string
		Count int
	}

	var results []FolderCount
	db.Raw(`
		SELECT file_path as path, COUNT(*) as count
		FROM user_activities
		WHERE user_id = ? AND activity_type = 'folder_access'
		GROUP BY file_path
		ORDER BY count DESC
		LIMIT 4
	`, user.ID).Scan(&results)

	type RecommendedFolder struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		FileCount int    `json:"fileCount"`
	}

	var folders []RecommendedFolder
	for _, result := range results {
		// Extract folder name from path
		folderName := result.Path
		if len(result.Path) > 20 {
			folderName = "..." + result.Path[len(result.Path)-20:]
		}

		folders = append(folders, RecommendedFolder{
			Name:      folderName,
			Path:      result.Path,
			FileCount: result.Count,
		})
	}

	c.JSON(http.StatusOK, folders)
}

// formatTimeAgo formats a timestamp into a human-readable relative time
func formatTimeAgo(t time.Time) string {
	elapsed := time.Since(t)

	if elapsed.Seconds() < 60 {
		return "Just now"
	} else if elapsed.Minutes() < 60 {
		mins := int(elapsed.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return string(rune(mins)) + " mins ago"
	} else if elapsed.Hours() < 24 {
		hours := int(elapsed.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return string(rune(hours)) + " hours ago"
	} else if elapsed.Hours() < 168 {
		days := int(elapsed.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return string(rune(days)) + " days ago"
	}

	return t.Format("Jan 2")
}
