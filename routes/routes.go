package routes

import (
	"api/controllers"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	// CORS Middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "API is running 🚀",
		})
	})

	r.GET("/folders", controllers.ListFilesAndFolders)
	r.GET("/files", controllers.ServeFile)
	r.POST("/rename", controllers.RenameItem)
	r.DELETE("/delete", controllers.DeleteItem)
	r.POST("/register", controllers.RegisterUser)
	r.POST("/login", controllers.Login)
	r.POST("/logout", controllers.Logout)
	r.GET("/monitor", controllers.GetNASMonitor)

	// User Management Routes
	r.GET("/users", controllers.GetAllUsers)
	r.GET("/users/:id", controllers.GetUser)
	r.GET("/users/username/:username", controllers.GetUserByUsername)
	r.POST("/users/change-password", controllers.ChangePassword)
	r.PUT("/users/:id", controllers.UpdateUser)
	r.DELETE("/users/:id", controllers.DeleteUser)

	// Dashboard Routes
	r.GET("/api/dashboard/stats", controllers.GetDashboardStats)
	r.GET("/api/dashboard/recent-activity", controllers.GetDashboardRecentActivity)

	// File Operations Routes
	r.GET("/api/files/search", controllers.SearchFiles)
	r.GET("/api/files/recommended", controllers.GetRecommendedFoldersFromActivity) // Updated
	r.GET("/api/files/recent-views", controllers.GetRecentFileViews)               // Updated

	// AI Activity Routes
	r.GET("/api/ai/recent-moves", controllers.GetRecentAIMoves)

	// AI Configuration Routes
	r.GET("/api/settings/autosort", controllers.GetSharedAIConfig)
	r.POST("/api/settings/autosort", controllers.UpdateSharedAIConfig)
	r.GET("/api/settings/autosort/users", controllers.GetUserAIConfigs)
	r.POST("/api/settings/autosort/users/:userId", controllers.UpdateUserAIConfig)
	r.POST("/api/autosort/send", controllers.SyncAIConfig)

	// AI Limits Routes
	r.GET("/api/ai/limits/users", controllers.GetUserLimits)
	r.PUT("/api/ai/limits/users/:id", controllers.UpdateUserLimit)
	r.GET("/api/ai/limits/folders", controllers.GetFolderLimits)
	r.PUT("/api/ai/limits/folders/:id", controllers.UpdateFolderLimit)
	r.GET("/api/ai/limits/global", controllers.GetGlobalAIConfig)
	r.PUT("/api/ai/limits/global", controllers.UpdateGlobalAIConfig)
	r.POST("/api/ai/limits/reset-daily", controllers.ResetDailyCounters)

	// Activity Routes
	r.GET("/api/activity/recent", controllers.GetRecentActivity)
	r.POST("/api/activity/track", controllers.TrackActivity)

	// Storage Routes (Real Linux monitoring)
	r.GET("/api/storage/devices", controllers.GetStorageDevices)

	return r
}
