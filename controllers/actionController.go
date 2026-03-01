package controllers

import (
	"api/database"
	"api/models"

	"github.com/gofiber/fiber/v2"
)

// --- History / AI Action Logs ---

func GetHistory(c *fiber.Ctx) error {
	var history []models.AIActionLog
	database.DB.Order("log_id desc").Limit(100).Find(&history)
	return c.JSON(history)
}

func AddHistory(c *fiber.Ctx) error {
	var history models.AIActionLog
	if err := c.BodyParser(&history); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	database.DB.Create(&history)
	return c.JSON(history)
}

func ClearHistory(c *fiber.Ctx) error {
	// GORM raw delete to clear the table
	database.DB.Exec("DELETE FROM ai_action_logs")
	return c.JSON(fiber.Map{"message": "History cleared"})
}

// --- Monitors (Now maps to UserAIConfig) ---

func GetMonitors(c *fiber.Ctx) error {
	var configs []models.UserAIConfig
	database.DB.Find(&configs)
	return c.JSON(configs)
}

func ToggleMonitor(c *fiber.Ctx) error {
	var input struct {
		OriginFolder      string `json:"origin_folder"`
		DestinationFolder string `json:"destination_folder"`
		Active            bool   `json:"active"` // we can map this to AutoSelectFolder or ignore if not needed
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	var config models.UserAIConfig
	// Find existing config for this origin, or create a new one
	result := database.DB.Where("origin_path = ?", input.OriginFolder).First(&config)

	config.OriginPath = input.OriginFolder
	config.DestinationPath = input.DestinationFolder
	// Assume Active mapping to AutoSelectFolder as a fallback if needed, or simply don't set it since it's not in the new model perfectly

	if result.RowsAffected == 0 {
		database.DB.Create(&config)
	} else {
		database.DB.Save(&config)
	}

	return c.JSON(config)
}
