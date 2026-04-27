package controllers

import (
	"api/database"
	"api/models"
	"sort"
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
	Type        string `json:"type" example:"ai_sort"`
	Description string `json:"description" example:"Organized file.jpg to /photos"`
	User        string `json:"user" example:"john_doe"`
	Timestamp   int64  `json:"timestamp" example:"1672531200000"`
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

	// 1. Get latest AI Actions with usernames
	type AIActionWithUsername struct {
		models.AIActionLog
		Username string
	}
	var aiLogs []AIActionWithUsername
	if database.DB.Migrator().HasTable(&models.AIActionLog{}) {
		database.DB.Table("ai_action_logs").
			Select("ai_action_logs.*, users.username").
			Joins("left join users on users.id = ai_action_logs.user_id").
			Order("ai_action_logs.created_at desc").
			Limit(10).
			Scan(&aiLogs)
	}

	for _, log := range aiLogs {
		activities = append(activities, AdminActivity{
			Type:        "ai_sort",
			Description: log.Action + " " + log.Filename,
			User:        log.Username,
			Timestamp:   log.CreatedAt,
		})
	}

	// 2. Get latest User Registrations
	var newUsers []models.User
	database.DB.Order("created_at desc").Limit(10).Find(&newUsers)
	for _, u := range newUsers {
		activities = append(activities, AdminActivity{
			Type:        "user_registered",
			Description: "New user registered: " + u.Username,
			User:        u.Username,
			Timestamp:   u.CreatedAt,
		})
	}

	// 3. Get latest System logs from ActivityLog
	var systemLogs []models.ActivityLog
	database.DB.Order("created_at desc").Limit(10).Find(&systemLogs)
	for _, l := range systemLogs {
		// Filter out duplicates that we already handled or redundant info
		if l.Action == "AI_HISTORY_ADD" || l.Action == "USER_REGISTER" || l.Action == "USER_LOGIN" {
			continue
		}
		
		activities = append(activities, AdminActivity{
			Type:        "system",
			Description: l.Action + ": " + l.Message,
			User:        l.Username,
			Timestamp:   l.CreatedAt,
		})
	}

	// Sort everything by timestamp descending
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].Timestamp > activities[j].Timestamp
	})

	// Take exactly 10 if we have more
	if len(activities) > 10 {
		activities = activities[:10]
	}

	// Fallback message
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
