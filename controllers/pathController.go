package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"log"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// GetSMBConfig returns SMB configuration for frontend path mapping
// This tells the frontend what the RPI's SMB host and base path are
// Endpoint: GET /api/smb-config
func GetSMBConfig(c *fiber.Ctx) error {
	translator := services.NewPathTranslator()
	config := translator.GetSMBConfig()

	return c.JSON(fiber.Map{
		"config": config,
	})
}

type TranslatePathRequest struct {
	Path string `json:"path"`
}

type TranslatePathResponse struct {
	OriginalPath   string `json:"original_path"`
	TranslatedPath string `json:"translated_path"`
	Error          string `json:"error,omitempty"`
}

// TranslatePath converts any path format to RPI local path
// Supports Windows drive letters (Z:\faan), UNC paths (\\192.168.100.192\faan), and Linux paths
// Endpoint: POST /api/translate-path
func TranslatePath(c *fiber.Ctx) error {
	var req TranslatePathRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	userID := ""
	if raw := c.Locals("user_id"); raw != nil {
		userID = raw.(string)
	} else if rawUser := c.Locals("user"); rawUser != nil {
		if token, ok := rawUser.(*jwt.Token); ok {
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				if id, ok := claims["user_id"].(string); ok {
					userID = id
				}
			}
		}
	}

	translatedPath := req.Path
	if len(req.Path) > 1 && req.Path[1] == ':' && userID != "" {
		var user models.User
		if err := database.DB.Where("id = ?", userID).First(&user).Error; err == nil && user.PersonalFolderPath != "" {
			parts := strings.SplitN(req.Path, ":", 2)
			if len(parts) == 2 {
				subPath := filepath.ToSlash(parts[1])
				subPath = strings.TrimPrefix(subPath, "/")
				translatedPath = user.PersonalFolderPath + "/" + subPath
				log.Printf("[Path Translator DB] Mapped Path: %s -> %s", req.Path, translatedPath)
			}
		}
	} else {
		translator := services.NewPathTranslator()
		if p, err := translator.TranslatePath(req.Path); err == nil {
			translatedPath = p
		}
	}

	resp := TranslatePathResponse{
		OriginalPath:   req.Path,
		TranslatedPath: translatedPath,
	}

	log.Printf("Translated path: %s -> %s", req.Path, translatedPath)

	return c.JSON(resp)
}
