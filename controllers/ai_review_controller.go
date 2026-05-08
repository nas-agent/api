package controllers

import (
	"api/config"
	"api/database"
	"api/models"
	"api/services"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type ReviewRequest struct {
	Action     string `json:"action"`      // "accept", "reject"
	Target     string `json:"target"`      // "original", "manual"
	ManualPath string `json:"manual_path"` // Only for Target="manual"
}

// notifyAgentFeedback sends a correction signal to the Python agent's /api/analyze/feedback
// endpoint so the vector memory is updated for future RAG retrievals.
func notifyAgentFeedback(userID string, fileID uint, finalFolder string, outcome string) {
	aiConfig := config.GetAIServiceConfig()
	endpoint := aiConfig.Endpoint("/api/analyze/feedback")

	payload := map[string]interface{}{
		"user_id":      userID,
		"file_id":      fileID,
		"final_folder": finalFolder,
		"outcome":      outcome,
	}
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("[Review] Failed to build agent feedback request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", aiConfig.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Review] Agent feedback notification failed (agent may be offline): %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[Review] Agent feedback sent: user=%s file_id=%d folder=%s outcome=%s status=%d",
		userID, fileID, finalFolder, outcome, resp.StatusCode)
}

// regenerateFolderDescription asks the Python agent to regenerate the semantic description
// of a folder after a user correction, keeping folder profiles fresh and accurate.
func regenerateFolderDescription(userID, folderName, geminiKey, geminiModel string) {
	aiConfig := config.GetAIServiceConfig()
	endpoint := aiConfig.Endpoint("/api/analyze/folder")

	// Fetch current files in this folder to give context
	var files []models.FileMetadata
	database.DB.Where("owner_id = ? AND nas_path LIKE ?", userID, "%/"+folderName+"/%").Limit(10).Find(&files)

	type FileCtx struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	var fileContexts []FileCtx
	for _, f := range files {
		fileContexts = append(fileContexts, FileCtx{Name: f.FileName, Summary: f.Summary})
	}

	payload := map[string]interface{}{
		"folder_name":   folderName,
		"file_contexts": fileContexts,
		"gemini_api_key": geminiKey,
		"gemini_model":   geminiModel,
	}
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", aiConfig.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Review] Folder description regeneration failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	if desc, ok := result["description"]; ok && desc != "" {
		database.DB.Model(&models.UserFolderProfile{}).
			Where("user_id = ? AND folder_name = ?", userID, folderName).
			Update("description", desc)
		log.Printf("[Review] Updated folder description for '%s': %s", folderName, desc[:min(len(desc), 80)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	// Fetch user AI config for Gemini keys (used in description regen)
	var userAIConfig models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&userAIConfig)

	currentPath := metadata.NASPath

	if req.Action == "accept" {
		// 1. Determine destination path
		destPath := userAIConfig.DestinationPath
		if destPath == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "destination path not configured"})
		}

		targetDir := filepath.Join(destPath, logEntry.Folder)
		os.MkdirAll(targetDir, os.ModePerm)

		fileName := filepath.Base(currentPath)
		finalPath := filepath.Join(targetDir, services.EnsureUniqueName(targetDir, fileName))

		log.Printf("[Review] Accepting move. Relocating %s -> %s", currentPath, finalPath)

		// Perform the move
		err := os.Rename(currentPath, finalPath)
		if err != nil {
			// Try copy/delete if rename fails
			err = services.CopyFile(currentPath, finalPath)
			if err == nil {
				os.Remove(currentPath)
			}
		}

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("failed to relocate file: %v", err)})
		}

		// 2. Update Metadata
		metadata.NASPath = finalPath
		database.DB.Save(&metadata)

		// 3. Update Log
		logEntry.Status = "accepted"
		logEntry.IsMove = true
		database.DB.Save(&logEntry)

		// 4. Trigger personalization learning
		services.RecordDecisionEvent(services.DecisionEventInput{
			UserID:          userID,
			FileID:          logEntry.FileID,
			Source:          "watcher_review",
			Outcome:         "accepted",
			SuggestedFolder: logEntry.Folder,
			FinalFolder:     logEntry.Folder,
			ConfidenceScore: logEntry.Confidence,
		})

		// Notify Python agent so RAG memory also learns
		go notifyAgentFeedback(userID, logEntry.FileID, logEntry.Folder, "accepted")

		return c.JSON(fiber.Map{"message": "Action accepted and file moved", "status": "accepted", "new_path": finalPath})
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

		// Ensure we handle directory vs file path correctly
		targetDir := ""
		fileName := ""

		// Check if targetPath is a directory
		info, err := os.Stat(targetPath)
		if err == nil && info.IsDir() {
			// It's a directory, move file INSIDE it
			targetDir = targetPath
			fileName = filepath.Base(currentPath)
		} else {
			// It's a full path or doesn't exist yet (create subfolder)
			targetDir = filepath.Dir(targetPath)
			fileName = filepath.Base(targetPath)
		}

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

		// The "final folder" is the folder name (last path component) where the file landed
		finalFolderName := filepath.Base(targetDir)

		// Trigger personalization learning (negative feedback on wrong folder, positive on correct)
		services.RecordDecisionEvent(services.DecisionEventInput{
			UserID:          userID,
			FileID:          logEntry.FileID,
			Source:          "watcher_review",
			Outcome:         "rejected",
			ReasonCode:      "changed_folder",
			SuggestedFolder: logEntry.Folder,
			FinalFolder:     finalFolderName,
			ConfidenceScore: logEntry.Confidence,
		})

		// Notify Python agent: update vector memory with the correction
		go notifyAgentFeedback(userID, logEntry.FileID, finalFolderName, "rejected")

		// Regenerate folder description for the destination folder so it stays accurate
		go regenerateFolderDescription(userID, finalFolderName, userAIConfig.GeminiAPIKey, userAIConfig.GeminiModel)

		return c.JSON(fiber.Map{"message": "Action rejected and file relocated", "status": "rejected", "new_path": finalPath})
	}

	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unknown action"})
}
