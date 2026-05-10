package controllers

import (
	"api/database"
	"api/models"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// GenerateMobileToken creates a short-lived token for QR code login
func GenerateMobileToken(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// Generate 32-byte hex token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	token := hex.EncodeToString(b)

	// Expires in 60 seconds
	expiresAt := time.Now().Unix() + 60

	mobileAuthToken := models.MobileAuthToken{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}

	if err := database.DB.Create(&mobileAuthToken).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save token"})
	}

	log.Printf("[MobileAuth] Generated token for user %s, expires in 60s", userID)

	return c.JSON(fiber.Map{
		"token":      token,
		"expires_at": expiresAt,
	})
}

// ExchangeMobileToken exchanges a valid QR token for a standard JWT session
func ExchangeMobileToken(c *fiber.Ctx) error {
	token := c.Query("token")

	if token == "" {
		log.Printf("[MobileAuth] ❌ Missing token in query")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing token"})
	}

	if len(token) < 8 {
		log.Printf("[MobileAuth] ❌ Received invalid/too short token")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid token format"})
	}

	var authToken models.MobileAuthToken
	// Find the token
	log.Printf("[MobileAuth] Incoming GET Exchange Request. Token prefix: %s", token[:8])
	if err := database.DB.Where("token = ?", token).First(&authToken).Error; err != nil {
		log.Printf("[MobileAuth] ❌ Token not found or database error: %v", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired token"})
	}

	// Delete the token so it can only be used once (Unscoped for permanent deletion)
	database.DB.Unscoped().Delete(&authToken)

	// Check expiration
	if time.Now().Unix() > authToken.ExpiresAt {
		log.Printf("[MobileAuth] ❌ Token expired for user %s", authToken.UserID)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token has expired"})
	}

	// Find user
	var user models.User
	if err := database.DB.Where("id = ?", authToken.UserID).First(&user).Error; err != nil {
		log.Printf("[MobileAuth] ❌ User %s not found in database", authToken.UserID)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	log.Printf("[MobileAuth] ✓ Valid token for user: %s (%s)", user.Username, user.ID)

	// Sign a dynamic JWT token (same logic as normal Login)
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "fallback_secret_for_local_dev"
	}

	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"exp":      time.Now().Add(time.Hour * 72).Unix(), // 3 days
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	t, err := jwtToken.SignedString([]byte(secret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error generating session token"})
	}

	log.Printf("[MobileAuth] Successful token exchange for user %s", user.ID)

	return c.JSON(fiber.Map{
		"message": "Success",
		"token":   t,
		"user": fiber.Map{
			"id":           user.ID,
			"username":     user.Username,
			"email":        user.Email,
			"created_date": user.CreatedAt,
		},
	})
}
