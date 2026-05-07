package routes

import (
	"api/controllers"
	"api/database"
	"api/models"
	"api/services"

	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func JWTMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Missing token"})
		}

		tokenString := strings.Split(authHeader, "Bearer ")
		if len(tokenString) < 2 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Invalid token format"})
		}

		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			secret = "fallback_secret_for_local_dev"
		}

		token, err := jwt.Parse(tokenString[1], func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Invalid or expired token"})
		}

		c.Locals("user", token)
		return c.Next()
	}
}

func SetupSetup(app *fiber.App) {
	api := app.Group("/api")

	api.Use(func(c *fiber.Ctx) error {
		err := c.Next()

		// Skip high-frequency polling/health checks
		path := c.Path()
		if strings.HasPrefix(path, "/api/health") || strings.HasPrefix(path, "/api/ai/notifications") {
			return err
		}

		// Noise reduction: Only log actions (POST, PUT, DELETE, PATCH, etc.)
		if c.Method() == "GET" {
			return err
		}

		// Determine descriptive action name
		actionName := "API_REQUEST"
		if manualAction := c.Locals("action_name"); manualAction != nil {
			actionName = manualAction.(string)
		} else {
			// Fallback to route-based mapping
			method := c.Method()
			routePath := c.Route().Path // e.g. /api/nas/shares/:id

			// Create a key like "POST /api/nas/shares" or "DELETE /api/nas/shares/:id"
			key := fmt.Sprintf("%s %s", method, routePath)

			// Simple mapping for common actions
			mapping := map[string]string{
				"POST /api/users/login":               "USER_LOGIN",
				"POST /api/users/register":            "USER_REGISTER",
				"POST /api/users/change-password":      "USER_PASSWORD_CHANGE",
				"POST /api/nas/storage/mount":         "STORAGE_MOUNT",
				"POST /api/nas/storage/unmount":       "STORAGE_UNMOUNT",
				"POST /api/nas/storage/format-and-mount": "STORAGE_FORMAT_MOUNT",
				"POST /api/nas/storage/raid1":         "RAID_CREATE",
				"DELETE /api/nas/raid/arrays/:raidName": "RAID_DELETE",
				"POST /api/nas/shares":                "SHARE_CREATE",
				"DELETE /api/nas/shares/:id":          "SHARE_DELETE",
				"POST /api/nas/groups":                "GROUP_CREATE",
				"POST /api/nas/groups/members":        "GROUP_MEMBER_ADD",
				"POST /api/nas/quotas":                "QUOTA_SET",
				"POST /api/admin/reconcile":           "SYSTEM_RECONCILE",
				"GET /api/admin/reconcile":            "SYSTEM_RECONCILE",
				"PUT /api/settings":                   "SETTINGS_UPDATE",
				"PUT /api/ai/config":                 "AI_CONFIG_UPDATE",
				"POST /api/ai/history":               "AI_HISTORY_ADD",
				"DELETE /api/ai/history":             "AI_HISTORY_CLEAR",
				"POST /api/ai/feedback":              "AI_FEEDBACK_SUBMIT",
				"POST /api/ai/monitors/toggle":       "AI_MONITOR_TOGGLE",
				"DELETE /api/users/:id":               "USER_DELETE",
			}

			if mapped, ok := mapping[key]; ok {
				actionName = mapped
			}
		}

		category := "system"
		userID := ""
		username := ""
		if raw := c.Locals("user"); raw != nil {
			if token, ok := raw.(*jwt.Token); ok {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					if v, ok := claims["user_id"].(string); ok {
						userID = v
					}
					if v, ok := claims["username"].(string); ok {
						username = v
					}
				}
			}
		}

		if userID != "" {
			category = "user"
		}

		if database.DB != nil {
			database.DB.Create(&models.ActivityLog{
				Category:   category,
				UserID:     userID,
				Username:   username,
				Action:     actionName,
				Source:     "api",
				Message:    c.Method() + " " + path,
				Method:     c.Method(),
				Path:       path,
				StatusCode: c.Response().StatusCode(),
				CreatedAt:  time.Now().UnixMilli(),
			})
		}

		return err
	})

	// Health check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"server": "go-fiber",
		})
	})

	// Folder Browser Test (debug/diagnostic)
	api.Post("/folders/test", controllers.TestFolderBrowser)

	// Public Users
	api.Post("/users/register", controllers.Register)
	api.Post("/users/login", controllers.Login)

	// Admin Reconciliation (Public for external triggers)
	api.Post("/admin/reconcile", controllers.ReconcileSystem)
	api.Get("/admin/reconcile", controllers.ReconcileSystem)
	
	// Public Share Link
	app.Get("/s/:token", controllers.ServePublicFile)

	// Protected Routes (Require Token)
	protected := api.Group("/", JWTMiddleware())

	// NAS
	protected.Get("/nas/storage/devices", controllers.GetStorageDevices)
	protected.Post("/nas/storage/mount", controllers.MountDevice)
	protected.Post("/nas/storage/format-and-mount", controllers.FormatAndMount)
	protected.Post("/nas/storage/unmount", controllers.UnmountDevice)
	protected.Post("/nas/storage/raid1", controllers.CreateRaid1)
	protected.Get("/nas/raid/arrays", controllers.GetRaidArrays)
	protected.Delete("/nas/raid/arrays/:raidName", controllers.DeleteRaidArray)
	protected.Get("/nas/volumes", controllers.GetVolumes)
	protected.Post("/nas/volumes", controllers.RegisterVolume)
	protected.Delete("/nas/volumes/:id", controllers.DeleteVolume)
	protected.Post("/nas/volumes/assign", controllers.AssignVolumeToUser)
	protected.Delete("/nas/volumes/:volumeId/users/:userId", controllers.RevokeVolumeFromUser)
	protected.Get("/nas/users/:userId/volumes", controllers.GetUserVolumes)
	protected.Get("/nas/volumes/:volumeId/users", controllers.GetVolumeUsers)
	protected.Get("/nas/network/interfaces", controllers.GetNetworkInterfaces)
	protected.Get("/system/security/status", controllers.GetSecurityStatus)
	protected.Put("/system/security/toggle", controllers.ToggleSecurityFeature)
	protected.Get("/system/firewall/rules", controllers.GetFirewallRules)
	protected.Get("/system/security/blocked-ips", controllers.GetBlockedIPs)

	// Share Management
	protected.Get("/nas/shares", controllers.GetShares)
	protected.Post("/nas/shares", controllers.CreateShare)
	protected.Get("/nas/shares/diagnostic/all", controllers.GetShareDiagnostics)
	protected.Get("/nas/shares/:id/diagnostic", controllers.DiagnosticShare)
	protected.Delete("/nas/shares/:id", controllers.DeleteShare)

	// Phase 4A: Advanced Permissions
	protected.Post("/nas/shares/permissions", controllers.SetSharePermission)
	protected.Get("/nas/shares/:shareId/permissions", controllers.GetSharePermissions)
	protected.Delete("/nas/permissions/:id", controllers.RevokeSharePermission)
	protected.Post("/nas/groups", controllers.CreateUserGroup)
	protected.Post("/nas/groups/members", controllers.AddUserToGroup)
	protected.Post("/nas/quotas", controllers.SetStorageQuota)
	protected.Put("/nas/quotas/ai/:userId", controllers.UpdateAIQuota)

	// Public Sharing Management
	protected.Post("/shares/public", controllers.CreatePublicShare)
	protected.Get("/shares/public", controllers.GetPublicShares)
	protected.Delete("/shares/public/:token", controllers.DeletePublicShare)

	// Phase 4B: Volume Health & Monitoring
	protected.Get("/nas/volumes/:volumeId/health", controllers.GetVolumeHealth)
	protected.Get("/nas/volumes/:volumeId/alerts", controllers.GetVolumeAlerts)
	protected.Put("/nas/alerts/:id/resolve", controllers.ResolveAlert)
	protected.Post("/nas/volumes/:volumeId/cleanup-policy", controllers.SetCleanupPolicy)
	protected.Get("/nas/volumes/:volumeId/cleanup-policy", controllers.GetCleanupPolicy)

	protected.Get("/monitor", controllers.GetSystemStats)
	protected.Get("/monitor/ai", controllers.GetAIMonitorStats)
	protected.Get("/system/disks", controllers.GetDiskStats)
	protected.Get("/users", controllers.GetUsers)
	protected.Delete("/users/:id", controllers.DeleteUser)

	// Admin & Dashboard
	protected.Get("/dashboard/summary", controllers.GetDashboardSummary)
	protected.Get("/dashboard/stats", controllers.GetAdminDashboardStats)
	protected.Get("/dashboard/recent-activity", controllers.GetAdminRecentActivity)
	protected.Get("/admin/logs", controllers.GetAdminLogs)

	protected.Post("/users/change-password", controllers.ChangePassword)
	protected.Get("/users/profile", controllers.GetProfile)

	// Settings
	protected.Get("/settings", controllers.GetSettings)
	protected.Put("/settings", controllers.UpdateSettings)

	// Folder Browser (for directory selection in UI)
	protected.Post("/folders", controllers.ListFolders)

	// AI config
	ai := api.Group("/ai") // Uses PUBLIC group
	ai.Get("/config", controllers.GetAIConfig)
	ai.Put("/config", controllers.UpdateAIConfig)
	ai.Get("/notifications", services.PollNotifications)

	// AI history (Protected)
	protectedAI := ai.Group("/", JWTMiddleware())
	protectedAI.Get("/history", controllers.GetHistory)
	protectedAI.Get("/debug-history", func(c *fiber.Ctx) error {
		userID := controllers.GetUserID(c)
		var count int64
		database.DB.Model(&models.AIActionLog{}).Where("user_id = ?", userID).Count(&count)
		return c.JSON(fiber.Map{
			"user_id": userID,
			"count":   count,
		})
	})
	protectedAI.Post("/history", controllers.AddHistory)
	protectedAI.Delete("/history", controllers.ClearHistory)
	protectedAI.Post("/feedback", controllers.SubmitPersonalizationFeedback)
	protectedAI.Post("/feedback/capture-manual-move", controllers.CaptureManualMoveFeedback)
	protectedAI.Post("/feedback/manual-relocate", controllers.ManualRelocateFeedback)
	protectedAI.Get("/personalization/profile", controllers.GetPersonalizationProfile)
	protectedAI.Post("/personalization/profile/generate-description", controllers.GenerateFolderDescription)
	protectedAI.Put("/personalization/folder-description", controllers.UpdateFolderProfileDescription)
	protectedAI.Delete("/personalization/reset", controllers.ResetPersonalization)
	protectedAI.Post("/naming/suggest", controllers.SuggestPersonalizedFilename)
	protectedAI.Post("/review/:id", controllers.HandleAIActionReview)

	// Monitors
	protectedAI.Get("/monitors", controllers.GetMonitors)
	protectedAI.Post("/monitors/toggle", controllers.ToggleMonitor)
	protectedAI.Post("/scan", controllers.TriggerManualScan)

	// Search
	protected.Post("/search/semantic", controllers.SemanticSearch)
	protected.Post("/search/by-path", controllers.FindFileByPath)

	// SMB Configuration and Path Translation
	protected.Get("/smb-config", controllers.GetSMBConfig)
	protected.Post("/translate-path", controllers.TranslatePath)
}
