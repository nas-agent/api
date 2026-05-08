package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// --- History / AI Action Logs ---

func GetHistory(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var history []models.AIActionLog
	database.DB.Where("user_id = ?", userID).Order("log_id desc").Limit(100).Find(&history)

	fmt.Printf("[DEBUG] GetHistory for userID: '%s', Found: %d logs\n", userID, len(history))

	type HistoryRow struct {
		models.AIActionLog
		FullPath      string `json:"full_path"`
		Summary       string `json:"summary"`
		SummaryTH     string `json:"summary_th"`
		Tags          string `json:"tags"`
		Description   string `json:"description"`
		DescriptionTH string `json:"description_th"`
	}

	fileIDs := make([]uint, 0, len(history))
	for _, h := range history {
		if h.FileID > 0 {
			fileIDs = append(fileIDs, h.FileID)
		}
	}

	fileDataByFileID := map[uint]struct{ path, summary, summaryTH, tags, description, descriptionTH string }{}
	if len(fileIDs) > 0 {
		var files []models.FileMetadata
		database.DB.Preload("Tags").Where("id IN ?", fileIDs).Find(&files)
		for _, file := range files {
			tagNames := make([]string, 0, len(file.Tags))
			for _, t := range file.Tags {
				tagNames = append(tagNames, t.TagName)
			}
			fileDataByFileID[file.ID] = struct{ path, summary, summaryTH, tags, description, descriptionTH string }{
				path:          file.NASPath,
				summary:       file.Summary,
				summaryTH:     file.SummaryTH,
				tags:          strings.Join(tagNames, ","),
				description:   file.Description,
				descriptionTH: file.DescriptionTH,
			}
		}
	}

	translator := services.NewPathTranslator()
	rows := make([]HistoryRow, 0, len(history))
	for _, h := range history {
		fData := fileDataByFileID[h.FileID]
		rawPath := fData.path
		translatedPath := rawPath
		if rawPath != "" {
			translatedPath = translator.ToWindowsPath(userID, rawPath)
		}

		rows = append(rows, HistoryRow{
			AIActionLog:   h,
			FullPath:      translatedPath,
			Summary:       fData.summary,
			SummaryTH:     fData.summaryTH,
			Tags:          fData.tags,
			Description:   fData.description,
			DescriptionTH: fData.descriptionTH,
		})
	}

	return c.JSON(rows)
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
