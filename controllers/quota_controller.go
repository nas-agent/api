package controllers

import (
	"api/database"
	"api/models"
	"github.com/gofiber/fiber/v2"
)

// UpdateAIQuota updates the daily AI file limit for a user
func UpdateAIQuota(c *fiber.Ctx) error {
	userID := c.Params("userId")
	if userID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing user ID"})
	}

	var input struct {
		DailyLimit int `json:"daily_limit"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid input"})
	}

	var usage models.UserUsage
	result := database.DB.Where("user_id = ?", userID).First(&usage)
	
	if result.Error != nil {
		// If not found, create one
		usage = models.UserUsage{
			UserID:           userID,
			AIFileLimitDaily: input.DailyLimit,
		}
		database.DB.Create(&usage)
	} else {
		usage.AIFileLimitDaily = input.DailyLimit
		database.DB.Save(&usage)
	}

	return c.JSON(usage)
}
