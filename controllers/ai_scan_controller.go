package controllers

import (
	"api/config"
	"api/database"
	"api/models"
	"api/services"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// TriggerManualScan handles the request to manually scan a user's origin folder.
func TriggerManualScan(c *fiber.Ctx) error {
	userID := ""

	// Get user ID from JWT token
	if raw := c.Locals("user"); raw != nil {
		if token, ok := raw.(*jwt.Token); ok {
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				if v, ok := claims["user_id"].(string); ok {
					userID = v
				}
			}
		}
	}

	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized: No user ID found in token"})
	}

	// Parse custom path from request body (optional)
	var req struct {
		Path       string `json:"path"`
		UserPrompt string `json:"user_prompt"`
	}
	c.BodyParser(&req)

	targetPath := ""
	if req.Path != "" {
		translator := services.NewPathTranslator()
		translated, err := translator.TranslatePath(userID, req.Path)
		if err == nil {
			targetPath = translated
		} else {
			// If it's already a Linux path, TranslatePath handles it,
			// but if it's a truly local Windows path, we might get an error.
			// For the API, we'll assume it's either translated or the user sent a Linux path.
			targetPath = req.Path
		}
	}

	// Trigger the scan
	count, err := services.ScanOrigin(userID, targetPath, req.UserPrompt)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to scan origin path",
			"details": err.Error(),
		})
	}

	// Record in history
	database.DB.Create(&models.AIActionLog{
		UserID:      userID,
		Action:      "manual_scan",
		Description: fmt.Sprintf("Manual scan of %s completed. Analyzed %d files.", targetPath, count),
		Filename:    targetPath,
		Status:      "success",
	})

	return c.JSON(fiber.Map{
		"message": "Manual scan completed",
		"scanned": count,
	})
}

// AutoSort proxies a request to the Python Agent's /autosort endpoint
func AutoSort(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req struct {
		SourcePath      string `json:"sourcePath"`
		DestinationPath string `json:"destinationPath"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Forward to Python Agent
	aiConfig := config.GetAIServiceConfig()
	aiEndpoint := aiConfig.Endpoint("/autosort")

	jsonData, _ := json.Marshal(req)

	// Create request with auth if needed (Go API -> Python Agent)
	proxyReq, err := http.NewRequest("POST", aiEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create proxy request"})
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("X-API-Key", aiConfig.APIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": "Failed to connect to AI Agent", "details": err.Error()})
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return c.Status(resp.StatusCode).Send(body)
	}

	return c.Status(200).Send(body)
}
