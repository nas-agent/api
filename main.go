package main

import (
	controllers "api/controllers"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "API is running 🚀",
		})
	})

	r.GET("/folders", controllers.ListFoldersOnly)
	r.POST("/register", controllers.RegisterUser)
	r.GET("/monitor", controllers.GetNASMonitor)
	r.Run(":8080") // http://localhost:8080
}
