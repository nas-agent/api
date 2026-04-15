package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func GetShares(c *fiber.Ctx) error {
	var shares []models.Share
	database.DB.Find(&shares)

	// In a real implementation, we might want to enrichment this data
	// with real-time status from Samba, but for now, we serve from DB.
	return c.JSON(shares)
}

func CreateShare(c *fiber.Ctx) error {
	var input struct {
		Name     string           `json:"name"`
		Type     models.ShareType `json:"type"`
		OwnerID  string           `json:"owner_id"`
		VolumeID string           `json:"volume_id"`
		IsPublic bool             `json:"is_public"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	// 1. Lookup Volume and get mount point
	var volume models.Volume
	if input.VolumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "volume_id is required"})
	}

	if err := database.DB.Where("id = ?", input.VolumeID).First(&volume).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Volume not found"})
	}

	// Validate volume is mounted
	if volume.Status != "Mounted" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume is not mounted"})
	}

	// 2. Determine Path using volume mount point
	var path string
	if input.Type == models.ShareTypePrivate {
		// Find username for the owner
		var user models.User
		if err := database.DB.Where("id = ?", input.OwnerID).First(&user).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "Owner not found"})
		}
		path = fmt.Sprintf("%s/homes/%s", volume.MountPoint, user.Username)
	} else {
		// Public share
		path = fmt.Sprintf("%s/shares/%s", volume.MountPoint, input.Name)
	}

	// 3. Create in DB
	share := models.Share{
		ID:       uuid.New().String(),
		Name:     input.Name,
		Path:     path,
		Type:     input.Type,
		OwnerID:  input.OwnerID,
		VolumeID: input.VolumeID,
		Status:   "Active",
		Protocol: "SMB",
	}

	if err := database.DB.Create(&share).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to save share to database"})
	}

	// 4. Sync to System
	var ownerName string
	if input.OwnerID != "" {
		var user models.User
		database.DB.Where("id = ?", input.OwnerID).First(&user)
		ownerName = user.Username
	}

	if err := services.Samba.RegisterShare(share.Name, share.Path, ownerName, input.IsPublic); err != nil {
		// Log error but don't fail the request completely if DB succeeded
		// In a production app, we'd want better rollback logic.
		fmt.Printf("Warning: Failed to sync share to Samba: %v\n", err)
	}

	return c.JSON(share)
}

func DeleteShare(c *fiber.Ctx) error {
	id := c.Params("id")
	var share models.Share
	if err := database.DB.Where("id = ?", id).First(&share).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Share not found"})
	}

	// 1. Unregister from Samba system
	if err := services.Samba.UnregisterShare(share.Name); err != nil {
		fmt.Printf("Warning: Failed to remove share from Samba config: %v\n", err)
	}

	// 2. Delete from DB
	database.DB.Unscoped().Delete(&share)

	return c.JSON(fiber.Map{"message": "Share removed from database and Samba config"})
}
