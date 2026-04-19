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
		return c.JSON(fiber.Map{"logs": []ActivityLogItem{}, "total": 0})
	}

	category := strings.TrimSpace(strings.ToLower(c.Query("category")))
	search := strings.TrimSpace(strings.ToLower(c.Query("search")))
	level := strings.TrimSpace(strings.ToUpper(c.Query("level")))

	// Default pagination values
	page := 1
	if rawPage := c.Query("page"); rawPage != "" {
		if p, err := strconv.Atoi(rawPage); err == nil && p > 0 {
			page = p
		}
	}

	limit := 10
	if rawLimit := c.Query("limit"); rawLimit != "" {
		if l, err := strconv.Atoi(rawLimit); err == nil && l > 0 {
			if l > 500 {
				l = 500
			}
			limit = l
		}
	}

	offset := (page - 1) * limit

	// Build query
	query := database.DB.Model(&models.ActivityLog{})

	if category == "system" || category == "user" {
		query = query.Where("category = ?", category)
	}

	if level != "" && level != "ALL" {
		switch level {
		case "INFO":
			query = query.Where("status_code < 400")
		case "WARNING":
			query = query.Where("status_code >= 400 AND status_code < 500")
		case "ERROR":
			query = query.Where("status_code >= 500")
		}
	}

	if search != "" {
		like := "%" + search + "%"
		query = query.Where(
			"LOWER(COALESCE(message, '')) LIKE ? OR LOWER(COALESCE(source, '')) LIKE ? OR LOWER(COALESCE(action, '')) LIKE ? OR LOWER(COALESCE(username, '')) LIKE ? OR LOWER(COALESCE(path, '')) LIKE ?",
			like, like, like, like, like,
		)
	}

	// Get total count before applying limit/offset
	var total int64
	query.Count(&total)

	// Fetch logs with pagination
	var logs []models.ActivityLog
	if err := query.Order("created_at desc").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
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

	return c.JSON(fiber.Map{
		"logs":  items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
