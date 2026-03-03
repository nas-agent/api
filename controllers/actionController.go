package controllers

import (
	"api/database"
	"api/models"

	"github.com/gofiber/fiber/v2"
)

// --- History / AI Action Logs ---

func GetHistory(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var history []models.AIActionLog
	database.DB.Where("user_id = ?", userID).Order("log_id desc").Limit(100).Find(&history)
	return c.JSON(history)
}

func AddHistory(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var history models.AIActionLog
	if err := c.BodyParser(&history); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	history.UserID = userID
	database.DB.Create(&history)
	return c.JSON(history)
}

func ClearHistory(c *fiber.Ctx) error {
	userID := GetUserID(c)
	database.DB.Where("user_id = ?", userID).Delete(&models.AIActionLog{})
	return c.JSON(fiber.Map{"message": "History cleared"})
}

// --- Monitors (Now maps to UserAIConfig) ---

func GetMonitors(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var configs []models.UserAIConfig
	database.DB.Where("user_id = ?", userID).Find(&configs)
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

	userID := GetUserID(c)
	var config models.UserAIConfig
	// Find existing config for this user and origin
	result := database.DB.Where("user_id = ? AND origin_path = ?", userID, input.OriginFolder).First(&config)

	config.UserID = userID
	config.OriginPath = input.OriginFolder
	config.DestinationPath = input.DestinationFolder

	if result.RowsAffected == 0 {
		database.DB.Create(&config)
	} else {
		database.DB.Save(&config)
	}

	return c.JSON(config)
}
