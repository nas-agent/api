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
	r.GET("/api/dashboard/recent-activity", controllers.GetRecentActivity)

	// File Operations Routes
	r.GET("/api/files/search", controllers.SearchFiles)
	r.GET("/api/files/recommended", controllers.GetRecommendedFolders)
	r.GET("/api/files/recent-views", controllers.GetRecentViews)

	// AI Activity Routes
	r.GET("/api/ai/recent-moves", controllers.GetRecentAIMoves)

	return r
}
