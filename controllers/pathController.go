package controllers

import (
	"api/services"
	"log"

	"github.com/gofiber/fiber/v2"
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

	translator := services.NewPathTranslator()
	translatedPath, err := translator.TranslatePath(req.Path)

	resp := TranslatePathResponse{
		OriginalPath: req.Path,
	}

	if err != nil {
		log.Printf("Path translation error for '%s': %v", req.Path, err)
		resp.Error = err.Error()
		return c.Status(400).JSON(resp)
	}

	resp.TranslatedPath = translatedPath
	log.Printf("Translated path: %s -> %s", req.Path, translatedPath)

	return c.JSON(resp)
}
