package controllers

import (
	"api/database"
	"api/models"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

type CreateShareRequest struct {
	FileID    uint  `json:"file_id"`
	ExpiresIn int64 `json:"expires_in"` // seconds from now, 0 for never
}

func GenerateToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func CreatePublicShare(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var req CreateShareRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	var metadata models.FileMetadata
	if err := database.DB.Where("id = ? AND owner_id = ?", req.FileID, userID).First(&metadata).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
	}

	token := GenerateToken(16)
	expiresAt := int64(0)
	if req.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + req.ExpiresIn
	}

	share := models.PublicShare{
		ID:        token,
		FileID:    metadata.ID,
		FileName:  metadata.FileName,
		FilePath:  metadata.NASPath,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}

	if err := database.DB.Create(&share).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create share"})
	}

	return c.JSON(fiber.Map{
		"token":      token,
		"share_url":  fmt.Sprintf("/s/%s", token),
		"expires_at": expiresAt,
	})
}

func GetPublicShares(c *fiber.Ctx) error {
	userID := GetUserID(c)
	var shares []models.PublicShare
	database.DB.Where("user_id = ?", userID).Find(&shares)
	return c.JSON(shares)
}

func DeletePublicShare(c *fiber.Ctx) error {
	userID := GetUserID(c)
	token := c.Params("token")
	if err := database.DB.Where("id = ? AND user_id = ?", token, userID).Delete(&models.PublicShare{}).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete share"})
	}
	return c.JSON(fiber.Map{"message": "share deleted"})
}

func ServePublicFile(c *fiber.Ctx) error {
	token := c.Params("token")
	var share models.PublicShare
	if err := database.DB.Where("id = ?", token).First(&share).Error; err != nil {
		return c.Status(fiber.StatusNotFound).SendString("File not found or link expired")
	}

	// Check expiration
	if share.ExpiresAt > 0 && time.Now().Unix() > share.ExpiresAt {
		database.DB.Delete(&share) // Cleanup expired link
		return c.Status(fiber.StatusGone).SendString("Link expired")
	}

	// Check if file exists
	if _, err := os.Stat(share.FilePath); os.IsNotExist(err) {
		return c.Status(fiber.StatusNotFound).SendString("File no longer exists on NAS")
	}

	// Set content-disposition
	disposition := "attachment"
	if c.Query("view") == "true" {
		disposition = "inline"
	}
	c.Set("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, share.FileName))
	
	// Serve the file
	return c.SendFile(share.FilePath)
}
