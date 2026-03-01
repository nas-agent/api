package routes

import (
	"api/controllers"

	"github.com/gofiber/fiber/v2"
)

func SetupSetup(app *fiber.App) {
	api := app.Group("/api")

	// Users
	api.Post("/users/register", controllers.Register)
	api.Post("/users/login", controllers.Login)

	// Settings
	api.Get("/settings", controllers.GetSettings)
	api.Put("/settings", controllers.UpdateSettings)

	// AI config
	ai := api.Group("/ai")
	ai.Get("/config", controllers.GetAIConfig)
	ai.Put("/config", controllers.UpdateAIConfig)

	// AI history
	ai.Get("/history", controllers.GetHistory)
	ai.Post("/history", controllers.AddHistory)
	ai.Delete("/history", controllers.ClearHistory)

	// Monitors
	ai.Get("/monitors", controllers.GetMonitors)
	ai.Post("/monitors/toggle", controllers.ToggleMonitor)

	// Search
	api.Post("/search/semantic", controllers.SemanticSearch)
}
