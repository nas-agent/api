package controllers

import (
	"api/config"
	"api/entity"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// DashboardStats represents the overall system statistics
type DashboardStats struct {
	TotalUsers      int64   `json:"totalUsers"`
	TotalFiles      int64   `json:"totalFiles"`
	TotalAgents     int64   `json:"totalAgents"`
	StorageUsed     int64   `json:"storageUsed"`
	StorageTotal    int64   `json:"storageTotal"`
	StoragePercent  float64 `json:"storagePercent"`
	AutosortEnabled bool    `json:"autosortEnabled"`
	FilesProcessed  int64   `json:"filesProcessed"`
}

// RecentActivity represents a single activity log
type RecentActivity struct {
	ID          uint      `json:"id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	User        string    `json:"user,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// GetDashboardStats - GET /api/dashboard/stats
func GetDashboardStats(c *gin.Context) {
	db := config.DB

	var stats DashboardStats

	// Count total users
	db.Model(&entity.User{}).Count(&stats.TotalUsers)

	// Count total files
	db.Model(&entity.File{}).Count(&stats.TotalFiles)

	// Count total agents
	db.Model(&entity.Agent{}).Count(&stats.TotalAgents)

	// Calculate storage usage (estimate based on root directory)
	storageInfo := calculateStorageInfo()
	stats.StorageUsed = storageInfo.Used
	stats.StorageTotal = storageInfo.Total
	if stats.StorageTotal > 0 {
		stats.StoragePercent = float64(stats.StorageUsed) / float64(stats.StorageTotal) * 100
	}

	// Check autosort status (check if any agent exists and is active)
	var activeAgents int64
	db.Model(&entity.Agent{}).Count(&activeAgents)
	stats.AutosortEnabled = activeAgents > 0

	// Files processed (using file count as proxy)
	stats.FilesProcessed = stats.TotalFiles

	c.JSON(http.StatusOK, stats)
}

// StorageInfo holds storage statistics
type StorageInfo struct {
	Used  int64
	Total int64
}

// calculateStorageInfo estimates storage usage
func calculateStorageInfo() StorageInfo {
	// Try to get storage info for current directory
	var info StorageInfo

	// Default values (100GB total, 50GB used for demo)
	info.Total = 107374182400
	info.Used = 53687091200

	// Try to calculate actual size of downloads directory
	downloadsPath := "./downloads"
	size, err := getDirSize(downloadsPath)
	if err == nil && size > 0 {
		info.Used = size
	}

	return info
}

// getDirSize calculates the size of a directory
func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// GetDashboardRecentActivity - GET /api/dashboard/recent-activity
func GetDashboardRecentActivity(c *gin.Context) {
	db := config.DB

	var activities []RecentActivity

	// Get recent file uploads (last 10)
	var files []entity.File
	db.Order("created_at desc").Limit(5).Find(&files)

	for _, file := range files {
		activities = append(activities, RecentActivity{
			ID:          file.ID,
			Type:        "file_upload",
			Description: "File uploaded: " + file.Name,
			Timestamp:   file.CreatedAt,
		})
	}

	// Get recent users (last 5)
	var users []entity.User
	db.Order("created_at desc").Limit(5).Find(&users)

	for _, user := range users {
		activities = append(activities, RecentActivity{
			ID:          user.ID,
			Type:        "user_registered",
			Description: "New user registered",
			User:        user.Username,
			Timestamp:   user.CreatedAt,
		})
	}

	// Sort activities by timestamp (most recent first)
	// Simple bubble sort for small dataset
	for i := 0; i < len(activities)-1; i++ {
		for j := 0; j < len(activities)-i-1; j++ {
			if activities[j].Timestamp.Before(activities[j+1].Timestamp) {
				activities[j], activities[j+1] = activities[j+1], activities[j]
			}
		}
	}

	// Limit to 10 most recent
	if len(activities) > 10 {
		activities = activities[:10]
	}

	c.JSON(http.StatusOK, gin.H{
		"activities": activities,
	})
}
