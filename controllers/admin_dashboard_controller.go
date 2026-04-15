package controllers

import (
	"api/database"
	"api/models"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/shirou/gopsutil/v4/disk"
)

type AdminDashboardStats struct {
	TotalUsers      int64   `json:"totalUsers"`
	TotalFiles      int64   `json:"totalFiles"`
	TotalAgents     int64   `json:"totalAgents"`
	StorageUsed     uint64  `json:"storageUsed"`
	StorageTotal    uint64  `json:"storageTotal"`
	StoragePercent  float64 `json:"storagePercent"`
	AutosortEnabled bool    `json:"autosortEnabled"`
	FilesProcessed  int64   `json:"filesProcessed"`
}

type AdminActivity struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	User        string `json:"user"`
	Timestamp   int64  `json:"timestamp"`
}

func GetAdminDashboardStats(c *fiber.Ctx) error {
	stats := AdminDashboardStats{}

	// 1. Total Users
	var userCount int64
	database.DB.Model(&models.User{}).Count(&userCount)
	stats.TotalUsers = userCount

	// 2. Total Files
	var fileCount int64
	database.DB.Model(&models.FileMetadata{}).Count(&fileCount)
	stats.TotalFiles = fileCount

	// 3. User AI Configs (Agents Active)
	var activeConfigs int64
	database.DB.Model(&models.UserAIConfig{}).Where("active = ?", true).Count(&activeConfigs)
	stats.TotalAgents = activeConfigs
	stats.AutosortEnabled = activeConfigs > 0

	// 4. Files Processed (via DecisionEvent or ActionLog)
	var aiActionCount int64
	if database.DB.Migrator().HasTable(&models.AIActionLog{}) {
		database.DB.Model(&models.AIActionLog{}).Count(&aiActionCount)
	}
	stats.FilesProcessed = aiActionCount

	usage, err := disk.Usage("/")
	if err == nil {
		stats.StorageTotal = usage.Total
		stats.StorageUsed = usage.Used
		stats.StoragePercent = usage.UsedPercent
	}

	return c.JSON(stats)
}

func GetAdminRecentActivity(c *fiber.Ctx) error {
	var activities []AdminActivity

	// Get latest AI Actions
	var logs []models.AIActionLog
	if database.DB.Migrator().HasTable(&models.AIActionLog{}) {
		// Use limit and order, note the column in models.AIActionLog is 'created_at' according to autoCreateTime but json is 'timestamp'
		database.DB.Order("created_at desc").Limit(5).Find(&logs)
	}

	for _, log := range logs {
		timestamp := log.CreatedAt
		if timestamp == 0 {
			timestamp = time.Now().UnixMilli()
		}

		activities = append(activities, AdminActivity{
			Type:        "ai_sort",
			Description: log.Action + " " + log.Filename,
			User:        log.UserID, // Using UserID for now
			Timestamp:   timestamp,
		})
	}

	// Get latest User Registrations
	var newUsers []models.User
	database.DB.Order("created_at desc").Limit(5).Find(&newUsers)
	for _, u := range newUsers {
		timestamp := u.CreatedAt
		if timestamp == 0 {
			timestamp = time.Now().UnixMilli()
		}
		activities = append(activities, AdminActivity{
			Type:        "user_registered",
			Description: "New user registered: " + u.Username,
			User:        u.Username,
			Timestamp:   timestamp,
		})
	}

	// For now, if empty, send a default welcome message
	if len(activities) == 0 {
		activities = append(activities, AdminActivity{
			Type:        "system_start",
			Description: "System initialized and monitoring started.",
			User:        "Admin",
			Timestamp:   time.Now().UnixMilli(),
		})
	}

	return c.JSON(fiber.Map{
		"activities": activities,
	})
}
