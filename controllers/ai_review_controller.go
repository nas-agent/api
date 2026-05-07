package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type ReviewRequest struct {
	Action     string `json:"action"`      // "accept", "reject"
	Target     string `json:"target"`      // "original", "manual"
	ManualPath string `json:"manual_path"` // Only for Target="manual"
}

func HandleAIActionReview(c *fiber.Ctx) error {
	userID := GetUserID(c)
	logID := c.Params("id")

	var logEntry models.AIActionLog
	if err := database.DB.Where("log_id = ? AND user_id = ?", logID, userID).First(&logEntry).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "action log not found"})
	}

	var req ReviewRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	// 1. Fetch File Metadata
	if logEntry.FileID == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "log entry missing file reference (ID: 0)"})
	}
	var metadata models.FileMetadata
	if err := database.DB.Where("id = ?", logEntry.FileID).First(&metadata).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file metadata not found for ID " + fmt.Sprint(logEntry.FileID)})
	}

	currentPath := metadata.NASPath

	if req.Action == "accept" {
		// Just mark as accepted and learn
		logEntry.Status = "accepted"
		database.DB.Save(&logEntry)

		// Trigger personalization learning
		services.RecordDecisionEvent(services.DecisionEventInput{
			UserID:          userID,
			FileID:          logEntry.FileID,
			Source:          "watcher_review",
			Outcome:         "accepted",
			SuggestedFolder: logEntry.Folder,
			FinalFolder:     logEntry.Folder,
			ConfidenceScore: logEntry.Confidence,
		})

		return c.JSON(fiber.Map{"message": "Action accepted", "status": "accepted"})
	}

	if req.Action == "reject" {
		var err error
		translator := services.NewPathTranslator()
		targetPath := ""
		if req.Target == "original" {
			targetPath = logEntry.OriginalPath
		} else if req.Target == "manual" {
			if strings.TrimSpace(req.ManualPath) == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "manual path required"})
			}
			// Translate manual path if it's Windows style
			targetPath, err = translator.TranslatePath(userID, req.ManualPath)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid manual path: %v", err)})
			}
		}

		if targetPath == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid target path"})
		}

		// Ensure unique name in target directory
		targetDir := filepath.Dir(targetPath)
		fileName := filepath.Base(targetPath)
		os.MkdirAll(targetDir, os.ModePerm)

		finalPath := filepath.Join(targetDir, services.EnsureUniqueName(targetDir, fileName))

		log.Printf("[Review] Rejecting move. Relocating %s -> %s", currentPath, finalPath)

		// Perform the move
		err = os.Rename(currentPath, finalPath)
		if err != nil {
			// Try copy/delete if rename fails (e.g. cross-device)
			err = services.CopyFile(currentPath, finalPath)
			if err == nil {
				os.Remove(currentPath)
			}
		}

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to relocate file: %v", err)})
		}

		// Update Metadata
		metadata.NASPath = finalPath
		database.DB.Save(&metadata)

		// Update Log
		logEntry.Status = "rejected"
		database.DB.Save(&logEntry)

		// Trigger personalization learning (negative feedback)
		services.RecordDecisionEvent(services.DecisionEventInput{
			UserID:          userID,
			FileID:          logEntry.FileID,
			Source:          "watcher_review",
			Outcome:         "rejected",
			SuggestedFolder: logEntry.Folder,
			FinalFolder:     filepath.Base(targetDir),
			ConfidenceScore: logEntry.Confidence,
		})

		return c.JSON(fiber.Map{"message": "Action rejected and file relocated", "status": "rejected", "new_path": finalPath})
	}

	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unknown action"})
}
