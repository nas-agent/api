package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	googleoauth "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

// GetCloudSyncConfig returns the current user's cloud sync configuration
func GetCloudSyncConfig(c *fiber.Ctx) error {
	userID := GetUserID(c)

	const defaultClientID = "1030741694147-05krmd0k7qf1juhs7n6ush0hi66s7bmc.apps.googleusercontent.com"
	const defaultClientSecret = "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"

	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", userID).First(&config).Error; err != nil {
		// Return default config (not yet configured)
		return c.JSON(fiber.Map{
			"user_id":       userID,
			"enabled":       false,
			"schedule":      "daily",
			"mock_mode":     false,
			"drive_email":   "",
			"last_sync_at":  0,
			"next_sync_at":  0,
			"connected":     false,
			"client_id":     defaultClientID,
			"client_secret": defaultClientSecret,
		})
	}

	// Use hardcoded defaults if empty in DB
	if config.ClientID == "" {
		config.ClientID = defaultClientID
	}
	if config.ClientSecret == "" {
		config.ClientSecret = defaultClientSecret
	}

	return c.JSON(fiber.Map{
		"id":              config.ID,
		"user_id":         config.UserID,
		"enabled":         config.Enabled,
		"schedule":        config.Schedule,
		"mock_mode":       config.MockMode,
		"drive_email":     config.DriveEmail,
		"drive_folder_id": config.DriveFolderID,
		"last_sync_at":    config.LastSyncAt,
		"next_sync_at":    config.NextSyncAt,
		"sync_time":       config.SyncTime,
		"connected":       config.DriveEmail != "" || config.MockMode,
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
		"drive_email":     "",
		"access_token":    "",
		"refresh_token":   "",
		"drive_folder_id": "",
		"enabled":         false,
		"next_sync_at":    0,
	})

	return c.JSON(fiber.Map{"message": "Google account disconnected"})
}

// getOAuthConfig returns the oauth2.Config for Google Drive
func getOAuthConfig(config models.CloudSyncConfig, c *fiber.Ctx, redirectBase string) *oauth2.Config {
	// Determine redirect URL
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirectURL == "" {
		if redirectBase != "" {
			redirectURL = fmt.Sprintf("%s/api/cloud/sync/callback", strings.TrimSuffix(redirectBase, "/"))
		} else {
			// Detect Cloudflare Tunnel URL or other proxy headers
			host := c.Get("X-Forwarded-Host")
			if host == "" {
				host = c.Hostname()
			}

			proto := c.Get("X-Forwarded-Proto")
			if proto == "" {
				proto = c.Protocol()
			}

			redirectURL = fmt.Sprintf("%s://%s/api/cloud/sync/callback", proto, host)
		}
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
	redirectBase := c.Query("redirect_base")

	const defaultClientID = "1030741694147-05krmd0k7qf1juhs7n6ush0hi66s7bmc.apps.googleusercontent.com"
	const defaultClientSecret = "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"

	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", userID).First(&config).Error; err != nil {
		config.UserID = userID
		config.ClientID = defaultClientID
		config.ClientSecret = defaultClientSecret
	}

	if config.ClientID == "" {
		config.ClientID = defaultClientID
	}
	if config.ClientSecret == "" {
		config.ClientSecret = defaultClientSecret
	}

	oauthConfig := getOAuthConfig(config, c, redirectBase)
	// state should include user ID so we know who is connecting
	state := userID

	url := oauthConfig.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)

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

	oauthConfig := getOAuthConfig(config, c, "")
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		return c.Status(500).SendString("Failed to exchange token: " + err.Error())
	}

	// Update config with tokens
	config.AccessToken = token.AccessToken
	config.RefreshToken = token.RefreshToken
	config.MockMode = false

	// Fetch user email using oauth2 service
	oauthService, err := googleoauth.NewService(context.Background(), option.WithTokenSource(oauthConfig.TokenSource(context.Background(), token)))
	if err == nil {
		userinfo, err := oauthService.Userinfo.Get().Do()
		if err == nil {
			config.DriveEmail = userinfo.Email
		}
	}

	// Fetch or Create "NAS-Agent Backup" folder
	driveService, err := drive.NewService(context.Background(), option.WithTokenSource(oauthConfig.TokenSource(context.Background(), token)))
	if err == nil {
		query := "name = 'NAS-Agent Backup' and mimeType = 'application/vnd.google-apps.folder' and trashed = false"
		fileList, err := driveService.Files.List().Q(query).Spaces("drive").Fields("files(id, name)").Do()
		if err == nil && len(fileList.Files) > 0 {
			config.DriveFolderID = fileList.Files[0].Id
		} else {
			// Create folder
			folder := &drive.File{
				Name:     "NAS-Agent Backup",
				MimeType: "application/vnd.google-apps.folder",
			}
			newFolder, err := driveService.Files.Create(folder).Fields("id").Do()
			if err == nil {
				config.DriveFolderID = newFolder.Id
			}
		}
	}

	database.DB.Save(&config)

	// Return a beautiful success page
	c.Set("Content-Type", "text/html")
	return c.SendString(`
		<!DOCTYPE html>
		<html>
		<head>
			<title>NAS Agent - Cloud Sync Success</title>
			<style>
				body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: #0f172a; color: white; text-align: center; }
				.card { background: rgba(255,255,255,0.05); padding: 3rem; border-radius: 2rem; border: 1px solid rgba(255,255,255,0.1); box-shadow: 0 25px 50px -12px rgba(0,0,0,0.5); max-width: 400px; }
				.icon { width: 80px; height: 80px; background: #22c55e; border-radius: 50%; display: flex; align-items: center; justify-content: center; margin: 0 auto 2rem; font-size: 40px; }
				h1 { margin: 0 0 1rem; font-size: 1.5rem; }
				p { color: #94a3b8; margin: 0 0 2rem; line-height: 1.6; }
				.btn { background: #3b82f6; color: white; text-decoration: none; padding: 0.8rem 2rem; border-radius: 1rem; font-weight: bold; display: inline-block; transition: transform 0.2s; }
				.btn:hover { transform: scale(1.05); }
			</style>
		</head>
		<body>
			<div class="card">
				<div class="icon">✓</div>
				<h1>Successfully Connected!</h1>
				<p>Your Google Drive has been linked to your NAS. You can now close this window and return to the NAS Agent app.</p>
				<button onclick="window.close()" class="btn">Close Window</button>
			</div>
		</body>
		</html>
	`)
}
