package controllers

import (
	"api/database"
	"api/models"
	"api/services"

	"github.com/gofiber/fiber/v2"
)

func ReconcileSystem(c *fiber.Ctx) error {
	// 1. Fetch all users from DB
	var users []models.User
	if err := database.DB.Find(&users).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch users from database"})
	}

	dbUsernames := make([]string, 0, len(users))
	for _, u := range users {
		dbUsernames = append(dbUsernames, u.Username)
	}

	// 2. Fetch all shares from DB
	var shares []models.Share
	if err := database.DB.Find(&shares).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch shares from database"})
	}

	dbShareNames := make([]string, 0, len(shares))
	for _, s := range shares {
		dbShareNames = append(dbShareNames, s.Name)
	}

	// 3. Trigger Pruning in Samba Service
	prunedShares, prunedUsers, err := services.Samba.PruneResources(dbShareNames, dbUsernames)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to prune system resources", "details": err.Error()})
	}

	return c.JSON(fiber.Map{
		"message": "System reconciliation complete",
		"stats": fiber.Map{
			"pruned_shares": prunedShares,
			"pruned_users":  prunedUsers,
		},
	})
}
