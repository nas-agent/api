package main

import (
	"api/database"
	"api/routes"
	"api/services"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	// Initialize Database Connection and Auto-Migrate
	database.ConnectDB()

	// Initialize File Watcher Service
	services.InitFileWatcher()

	// Initialize Fiber App
	app := fiber.New()

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*", // Frontend Dev Server Route
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		ExposeHeaders:    "Content-Length, Content-Type",
		AllowCredentials: false,
	}))

	// Serve uploaded files statically
	app.Static("/uploads", "./data/uploads")

	// Setup Routes
	routes.SetupSetup(app)

	// Start Server
	log.Fatal(app.Listen(":3000"))
}
