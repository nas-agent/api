package controllers

import (
	"api/services"
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

	// Trigger the scan
	count, err := services.ScanOrigin(userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to scan origin path",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Manual scan completed",
		"scanned": count,
	})
}
