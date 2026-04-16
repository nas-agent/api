package controllers

import (
	"api/database"
	"api/models"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type ActivityLogItem struct {
	ID         uint   `json:"id"`
	Category   string `json:"category"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Action     string `json:"action"`
	Source     string `json:"source"`
	Message    string `json:"message"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	CreatedAt  int64  `json:"created_at"`
}

func GetAdminLogs(c *fiber.Ctx) error {
	if !database.DB.Migrator().HasTable(&models.ActivityLog{}) {
		return c.JSON(fiber.Map{"logs": []ActivityLogItem{}})
	}

	category := strings.TrimSpace(strings.ToLower(c.Query("category")))
	search := strings.TrimSpace(strings.ToLower(c.Query("search")))
	limit := 100
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		if n, err := strconv.Atoi(rawLimit); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}

	query := database.DB.Model(&models.ActivityLog{}).Order("created_at desc").Limit(limit)
	if category == "system" || category == "user" {
		query = query.Where("category = ?", category)
	}
	if search != "" {
		like := "%" + search + "%"
		query = query.Where(
			"LOWER(COALESCE(message, '')) LIKE ? OR LOWER(COALESCE(source, '')) LIKE ? OR LOWER(COALESCE(action, '')) LIKE ? OR LOWER(COALESCE(username, '')) LIKE ? OR LOWER(COALESCE(path, '')) LIKE ?",
			like,
			like,
			like,
			like,
			like,
		)
	}

	var logs []models.ActivityLog
	if err := query.Find(&logs).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load logs"})
	}

	items := make([]ActivityLogItem, 0, len(logs))
	for _, log := range logs {
		items = append(items, ActivityLogItem{
			ID:         log.ID,
			Category:   log.Category,
			UserID:     log.UserID,
			Username:   log.Username,
			Action:     log.Action,
			Source:     log.Source,
			Message:    log.Message,
			Method:     log.Method,
			Path:       log.Path,
			StatusCode: log.StatusCode,
			CreatedAt:  log.CreatedAt,
		})
	}

	return c.JSON(fiber.Map{"logs": items})
}
