package controllers

import (
	"api/config"
	"api/entity"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetSharedAIConfig - GET /api/settings/autosort
func GetSharedAIConfig(c *gin.Context) {
	db := config.DB

	var aiConfig entity.AIConfig
	result := db.Where("is_shared = ?", true).First(&aiConfig)

	// If no shared config exists, create default
	if result.Error != nil {
		aiConfig = entity.AIConfig{
			IsShared:            true,
			Enabled:             true,
			AutoRename:          true,
			IgnoreThreshold:     30,
			AutomationThreshold: 85,
			SourcePath:          "",
			DestinationPath:     "",
			DeleteAfterImport:   true,
			NamingConvention:    "category",
		}
		// Set defaults for JSON fields
		aiConfig.SetTargetExtensions([]string{"PDF", "DOCX", "JPG", "PNG"})
		aiConfig.SetExcludedFolders([]string{"/System", "/Trash"})

		db.Create(&aiConfig)
	}

	c.JSON(http.StatusOK, aiConfig.ToDTO())
}

// UpdateSharedAIConfig - POST /api/settings/autosort
func UpdateSharedAIConfig(c *gin.Context) {
	db := config.DB

	var dto entity.AIConfigDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find or create shared config
	var aiConfig entity.AIConfig
	result := db.Where("is_shared = ?", true).First(&aiConfig)

	if result.Error != nil {
		// Create new shared config
		aiConfig = entity.AIConfig{IsShared: true}
	}

	// Update fields
	aiConfig.Enabled = dto.Enabled
	aiConfig.AutoRename = dto.AutoRename
	aiConfig.IgnoreThreshold = dto.IgnoreThreshold
	aiConfig.AutomationThreshold = dto.AutomationThreshold
	aiConfig.SourcePath = dto.SourcePath
	aiConfig.DestinationPath = dto.DestinationPath
	aiConfig.DeleteAfterImport = dto.DeleteAfterImport
	aiConfig.NamingConvention = dto.NamingConvention
	aiConfig.SetTargetExtensions(dto.TargetExtensions)
	aiConfig.SetExcludedFolders(dto.ExcludedFolders)

	if aiConfig.ID == 0 {
		db.Create(&aiConfig)
	} else {
		db.Save(&aiConfig)
	}

	c.JSON(http.StatusOK, aiConfig.ToDTO())
}

// GetUserAIConfigs - GET /api/settings/autosort/users
func GetUserAIConfigs(c *gin.Context) {
	db := config.DB

	var configs []entity.AIConfig
	db.Preload("User").Where("is_shared = ?", false).Find(&configs)

	// Get all users to include those without configs
	var users []entity.User
	db.Find(&users)

	var result []entity.UserConfigDTO

	for _, user := range users {
		// Find config for this user
		var userConfig *entity.AIConfig
		for _, cfg := range configs {
			if cfg.UserID != nil && *cfg.UserID == user.ID {
				userConfig = &cfg
				break
			}
		}

		// If no config exists, create default
		if userConfig == nil {
			defaultConfig := entity.AIConfig{
				UserID:              &user.ID,
				IsShared:            false,
				Enabled:             true,
				AutoRename:          true,
				IgnoreThreshold:     30,
				AutomationThreshold: 85,
				NamingConvention:    "category",
			}
			defaultConfig.SetTargetExtensions([]string{"PDF", "DOCX", "JPG", "PNG"})
			defaultConfig.SetExcludedFolders([]string{})

			// Don't save to DB yet, just return for display
			dto := entity.UserConfigDTO{
				AIConfigDTO: defaultConfig.ToDTO(),
				UserName:    user.Username,
			}
			result = append(result, dto)
		} else {
			dto := entity.UserConfigDTO{
				AIConfigDTO: userConfig.ToDTO(),
				UserName:    user.Username,
			}
			result = append(result, dto)
		}
	}

	c.JSON(http.StatusOK, result)
}

// UpdateUserAIConfig - POST /api/settings/autosort/users/:userId
func UpdateUserAIConfig(c *gin.Context) {
	db := config.DB
	userID := c.Param("userId")

	var dto entity.AIConfigDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find or create user config
	var aiConfig entity.AIConfig
	var userIDUint uint
	fmt.Sscanf(userID, "%d", &userIDUint)

	result := db.Where("user_id = ? AND is_shared = ?", userIDUint, false).First(&aiConfig)

	if result.Error != nil {
		// Create new user config
		aiConfig = entity.AIConfig{
			UserID:   &userIDUint,
			IsShared: false,
		}
	}

	// Update fields
	aiConfig.Enabled = dto.Enabled
	aiConfig.AutoRename = dto.AutoRename
	aiConfig.IgnoreThreshold = dto.IgnoreThreshold
	aiConfig.AutomationThreshold = dto.AutomationThreshold
	aiConfig.SourcePath = dto.SourcePath
	aiConfig.DestinationPath = dto.DestinationPath
	aiConfig.DeleteAfterImport = dto.DeleteAfterImport
	aiConfig.NamingConvention = dto.NamingConvention
	aiConfig.SetTargetExtensions(dto.TargetExtensions)
	aiConfig.SetExcludedFolders(dto.ExcludedFolders)

	if aiConfig.ID == 0 {
		db.Create(&aiConfig)
	} else {
		db.Save(&aiConfig)
	}

	c.JSON(http.StatusOK, aiConfig.ToDTO())
}

// SyncAIConfig - POST /api/autosort/send
// Forwards configuration to AI service (agent-api)
func SyncAIConfig(c *gin.Context) {
	var config map[string]interface{}
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Forward to agent-api (assuming it runs on localhost:8000)
	agentAPIURL := "http://localhost:8000/config"

	jsonData, err := json.Marshal(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config"})
		return
	}

	resp, err := http.Post(agentAPIURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		// Log error but don't fail - config is saved to DB
		fmt.Printf("Warning: Failed to sync with AI service: %v\n", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "Configuration saved, but AI service sync failed",
			"warning": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{
			"message": "Configuration saved, but AI service returned error",
			"warning": string(body),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration saved and synced with AI service",
	})
}
