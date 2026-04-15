package routes

import (
	"api/controllers"
	"api/services"

	"os"
	"strings"

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

	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"server": "go-fiber",
		})
	})

	// Public Users
	api.Post("/users/register", controllers.Register)
	api.Post("/users/login", controllers.Login)

	// NAS
	api.Get("/nas/storage/devices", controllers.GetStorageDevices)
	api.Post("/nas/storage/mount", controllers.MountDevice)
	api.Post("/nas/storage/unmount", controllers.UnmountDevice)
	api.Get("/nas/network/interfaces", controllers.GetNetworkInterfaces)
	api.Get("/nas/shares", controllers.GetShares)
	api.Post("/nas/shares", controllers.CreateShare)
	api.Delete("/nas/shares/:id", controllers.DeleteShare)
	api.Get("/monitor", controllers.GetSystemStats)
	api.Get("/users", controllers.GetUsers)
	api.Delete("/users/:id", controllers.DeleteUser)

	// Protected Routes
	protected := api.Group("/", JWTMiddleware())
	protected.Post("/users/change-password", controllers.ChangePassword)
	protected.Get("/users/profile", controllers.GetProfile)

	// Settings
	protected.Get("/settings", controllers.GetSettings)
	protected.Put("/settings", controllers.UpdateSettings)

	// AI config
	ai := protected.Group("/ai")
	ai.Get("/config", controllers.GetAIConfig)
	ai.Put("/config", controllers.UpdateAIConfig)
	ai.Get("/notifications", services.PollNotifications)

	// AI history
	ai.Get("/history", controllers.GetHistory)
	ai.Post("/history", controllers.AddHistory)
	ai.Delete("/history", controllers.ClearHistory)
	ai.Post("/feedback", controllers.SubmitPersonalizationFeedback)
	ai.Post("/feedback/capture-manual-move", controllers.CaptureManualMoveFeedback)
	ai.Post("/feedback/manual-relocate", controllers.ManualRelocateFeedback)
	ai.Get("/personalization/profile", controllers.GetPersonalizationProfile)
	ai.Post("/personalization/profile/generate-description", controllers.GenerateFolderDescription)
	ai.Put("/personalization/folder-description", controllers.UpdateFolderProfileDescription)
	ai.Delete("/personalization/reset", controllers.ResetPersonalization)
	ai.Post("/naming/suggest", controllers.SuggestPersonalizedFilename)

	// Monitors
	ai.Get("/monitors", controllers.GetMonitors)
	ai.Post("/monitors/toggle", controllers.ToggleMonitor)

	// Search
	protected.Get("/dashboard/summary", controllers.GetDashboardSummary)
	api.Get("/dashboard/stats", controllers.GetAdminDashboardStats)
	api.Get("/dashboard/recent-activity", controllers.GetAdminRecentActivity)
	protected.Post("/search/semantic", controllers.SemanticSearch)
}
