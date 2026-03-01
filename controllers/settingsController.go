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
	var setting models.UserSetting
	// Since there's one global setting in this demo, just get the first one
	database.DB.First(&setting)
	return c.JSON(setting)
}

func UpdateSettings(c *fiber.Ctx) error {
	var setting models.UserSetting
	database.DB.First(&setting)

	if err := c.BodyParser(&setting); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	database.DB.Save(&setting)
	return c.JSON(setting)
}

// --- AI Config ---

func GetAIConfig(c *fiber.Ctx) error {
	var config models.UserAIConfig
	database.DB.First(&config)
	return c.JSON(config)
}

func UpdateAIConfig(c *fiber.Ctx) error {
	var config models.UserAIConfig
	database.DB.First(&config)

	if err := c.BodyParser(&config); err != nil {
		return c.Status(400).JSON(err.Error())
	}

	database.DB.Save(&config)

	// Refresh the file watcher background service to pick up new paths
	services.RefreshFileWatcher()
	log.Println("AI Configuration updated successfully. File Watcher refreshed.")

	return c.JSON(config)
}
