package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"log"

	"github.com/gofiber/fiber/v2"
)

// --- Settings ---

func GetSettings(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var setting models.UserSetting
	// Use Limit(1).Find to avoid First()'s automatic "record not found" log
	database.DB.Where("user_id = ?", userID).Limit(1).Find(&setting)
	return c.JSON(setting)
}

func UpdateSettings(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var setting models.UserSetting
	database.DB.Where("user_id = ?", userID).First(&setting)
	setting.UserID = userID // Ensure owner remains correct

	if err := c.BodyParser(&setting); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	database.DB.Save(&setting)
	return c.JSON(setting)
}

// --- AI Config ---

func GetAIConfig(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var config models.UserAIConfig
	database.DB.Where("user_id = ?", userID).Limit(1).Find(&config)
	return c.JSON(config)
}

func UpdateAIConfig(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var config models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&config)
	config.UserID = userID

	if err := c.BodyParser(&config); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	// Translate paths from any format (Windows drive letters, UNC, etc.) to Linux paths
	translator := services.NewPathTranslator()
	originalOriginPath := config.OriginPath
	originalDestPath := config.DestinationPath
	
	if config.OriginPath != "" {
		if translated, err := translator.TranslatePath(config.OriginPath); err == nil {
			config.OriginPath = translated
			log.Printf("Translated origin path: %s -> %s", originalOriginPath, config.OriginPath)
		} else {
			log.Printf("Warning: Could not translate origin path '%s': %v", config.OriginPath, err)
			// Keep the original path if translation fails
		}
	}
	
	if config.DestinationPath != "" {
		if translated, err := translator.TranslatePath(config.DestinationPath); err == nil {
			config.DestinationPath = translated
			log.Printf("Translated destination path: %s -> %s", originalDestPath, config.DestinationPath)
		} else {
			log.Printf("Warning: Could not translate destination path '%s': %v", config.DestinationPath, err)
			// Keep the original path if translation fails
		}
	}

	database.DB.Save(&config)

	// Refresh the file watcher background service to pick up new paths
	services.RefreshFileWatcher()
	log.Println("AI Configuration updated successfully. File Watcher refreshed.")

	return c.JSON(config)
}
