package controllers

import (
	"api/models"
	"api/services"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// GetCloudSyncConfig returns the current user's cloud sync configuration
func GetCloudSyncConfig(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", userID).First(&config).Error; err != nil {
		// Return default config (not yet configured)
		return c.JSON(fiber.Map{
			"user_id":        userID,
			"enabled":        false,
			"schedule":       "daily",
			"mock_mode":      true,
			"drive_email":    "",
			"last_sync_at":   0,
			"next_sync_at":   0,
			"connected":      false,
		})
	}

	return c.JSON(fiber.Map{
		"id":             config.ID,
		"user_id":        config.UserID,
		"enabled":        config.Enabled,
		"schedule":       config.Schedule,
		"mock_mode":      config.MockMode,
		"drive_email":    config.DriveEmail,
		"drive_folder_id": config.DriveFolderID,
		"last_sync_at":   config.LastSyncAt,
		"next_sync_at":   config.NextSyncAt,
		"sync_time":      config.SyncTime,
		"connected":      config.DriveEmail != "" || config.MockMode,
	})
}

// UpdateCloudSyncConfig upserts the user's cloud sync configuration
func UpdateCloudSyncConfig(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var input struct {
		Enabled      bool   `json:"enabled"`
		Schedule     string `json:"schedule"`
		MockMode     bool   `json:"mock_mode"`
		DriveEmail   string `json:"drive_email"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		SyncTime     string `json:"sync_time"`
	}
	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Validate schedule
	validSchedules := map[string]bool{"daily": true, "weekly": true, "hourly": true}
	if input.Schedule == "" {
		input.Schedule = "daily"
	} else if !validSchedules[input.Schedule] {
		return c.Status(400).JSON(fiber.Map{"error": "schedule must be 'daily', 'weekly', or 'hourly'"})
	}

	now := time.Now().Unix()

	var config models.CloudSyncConfig
	isNew := database.DB.Where("user_id = ?", userID).First(&config).Error != nil

	config.UserID = userID
	config.Enabled = input.Enabled
	config.Schedule = input.Schedule
	config.MockMode = input.MockMode
	if input.DriveEmail != "" {
		config.DriveEmail = input.DriveEmail
	}
	if input.ClientID != "" {
		config.ClientID = input.ClientID
	}
	if input.ClientSecret != "" {
		config.ClientSecret = input.ClientSecret
	}
	if input.SyncTime != "" {
		config.SyncTime = input.SyncTime
	}

	// If enabling for the first time, set next sync
	if input.Enabled && config.NextSyncAt == 0 {
		config.NextSyncAt = now + 60 // Start first sync in 60 seconds
	}
	// If disabling, clear next sync
	if !input.Enabled {
		config.NextSyncAt = 0
	}

	if isNew {
		database.DB.Create(&config)
	} else {
		database.DB.Save(&config)
	}

	return c.JSON(fiber.Map{
		"message":  "Cloud sync configuration saved",
		"enabled":  config.Enabled,
		"schedule": config.Schedule,
	})
}

// TriggerCloudSync manually starts a sync job in the background
func TriggerCloudSync(c *fiber.Ctx) error {
	userID := GetUserID(c)

	// Check a sync isn't already running
	var runningLog models.CloudSyncLog
	if database.DB.Where("user_id = ? AND status = 'running'", userID).First(&runningLog).Error == nil {
		return c.Status(409).JSON(fiber.Map{
			"error":   "A sync is already in progress",
			"log_id":  runningLog.ID,
			"started": runningLog.StartedAt,
		})
	}

	// Run in background
	go func() {
		if _, err := services.RunCloudSync(userID); err != nil {
			// errors are already logged inside RunCloudSync
			_ = err
		}
	}()

	return c.JSON(fiber.Map{
		"message": "Cloud sync started in background",
		"status":  "running",
	})
}

// GetCloudSyncLogs returns recent sync history
func GetCloudSyncLogs(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var logs []models.CloudSyncLog
	database.DB.Where("user_id = ?", userID).Order("started_at desc").Limit(20).Find(&logs)

	return c.JSON(logs)
}

// GetCloudSyncStatus returns overall sync status and file-level stats
func GetCloudSyncStatus(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var totalFiles int64
	var syncedFiles int64
	var pendingFiles int64
	var failedFiles int64

	database.DB.Model(&models.FileMetadata{}).Where("owner_id = ?", userID).Count(&totalFiles)
	database.DB.Model(&models.CloudSyncFile{}).Where("user_id = ? AND sync_status = 'synced'", userID).Count(&syncedFiles)
	database.DB.Model(&models.CloudSyncFile{}).Where("user_id = ? AND sync_status = 'pending'", userID).Count(&pendingFiles)
	database.DB.Model(&models.CloudSyncFile{}).Where("user_id = ? AND sync_status = 'failed'", userID).Count(&failedFiles)

	// Last sync log
	var lastLog models.CloudSyncLog
	database.DB.Where("user_id = ?", userID).Order("started_at desc").First(&lastLog)

	// Active sync
	var activeLog models.CloudSyncLog
	isRunning := database.DB.Where("user_id = ? AND status = 'running'", userID).First(&activeLog).Error == nil

	return c.JSON(fiber.Map{
		"total_files":   totalFiles,
		"synced_files":  syncedFiles,
		"pending_files": pendingFiles,
		"failed_files":  failedFiles,
		"is_running":    isRunning,
		"last_sync":     lastLog,
	})
}

// ConnectMockGoogleAccount simulates OAuth connection with a mock account
func ConnectMockGoogleAccount(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var input struct {
		Email string `json:"email"`
	}
	if err := c.BodyParser(&input); err != nil || input.Email == "" {
		input.Email = "mock-user@gmail.com"
	}

	now := time.Now().Unix()

	var config models.CloudSyncConfig
	isNew := database.DB.Where("user_id = ?", userID).First(&config).Error != nil

	config.UserID = userID
	config.MockMode = true
	config.DriveEmail = input.Email
	config.AccessToken = "mock_access_token_" + userID
	config.RefreshToken = "mock_refresh_token_" + userID

	if isNew {
		config.Schedule = "daily"
		config.NextSyncAt = now + 60
		database.DB.Create(&config)
	} else {
		database.DB.Save(&config)
	}

	return c.JSON(fiber.Map{
		"message":     "Mock Google account connected successfully",
		"drive_email": config.DriveEmail,
		"mock_mode":   true,
	})
}

// DisconnectGoogleAccount removes the OAuth credentials
func DisconnectGoogleAccount(c *fiber.Ctx) error {
	userID := GetUserID(c)

	database.DB.Model(&models.CloudSyncConfig{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
		"drive_email":    "",
		"access_token":   "",
		"refresh_token":  "",
		"drive_folder_id": "",
		"enabled":        false,
		"next_sync_at":   0,
	})

	return c.JSON(fiber.Map{"message": "Google account disconnected"})
}

// getOAuthConfig returns the oauth2.Config for Google Drive
func getOAuthConfig(config models.CloudSyncConfig, c *fiber.Ctx) *oauth2.Config {
	// Determine redirect URL
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirectURL == "" {
		// Fallback to current host if not set
		redirectURL = fmt.Sprintf("%s://%s/api/cloud/sync/callback", c.Protocol(), c.Hostname())
	}

	return &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{drive.DriveFileScope, "https://www.googleapis.com/auth/userinfo.email"},
	}
}

// GetGoogleAuthURL returns the URL to start the OAuth flow
func GetGoogleAuthURL(c *fiber.Ctx) error {
	userID := GetUserID(c)

	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", userID).First(&config).Error; err != nil || config.ClientID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Please configure Client ID and Client Secret first"})
	}

	oauthConfig := getOAuthConfig(config, c)
	// state should include user ID so we know who is connecting
	state := userID
	url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	return c.JSON(fiber.Map{"url": url})
}

// GoogleAuthCallback handles the redirect from Google
func GoogleAuthCallback(c *fiber.Ctx) error {
	code := c.Query("code")
	state := c.Query("state") // state is our userID

	if code == "" || state == "" {
		return c.Status(400).SendString("Invalid callback parameters")
	}

	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", state).First(&config).Error; err != nil {
		return c.Status(404).SendString("User configuration not found")
	}

	oauthConfig := getOAuthConfig(config, c)
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		return c.Status(500).SendString("Failed to exchange token: " + err.Error())
	}

	// Update config with tokens
	config.AccessToken = token.AccessToken
	config.RefreshToken = token.RefreshToken
	config.MockMode = false

	// Get email from token info or API
	// For simplicity, we can just save it.
	// You might want to use the drive service here to verify.

	database.DB.Save(&config)

	// Redirect back to the UI
	uiURL := os.Getenv("UI_BASE_URL")
	if uiURL == "" {
		uiURL = "/" // Default relative
	}
	return c.Redirect(uiURL + "/setting/cloud-sync?success=true")
}
