package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"
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
