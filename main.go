package main

import (
	"api/config"
	"api/routes"
	"api/services"
	"log"
)

func main() {
	config.ConnectDatabase()
	config.Migrate()

	r := routes.SetupRouter()

	// Start File Watcher
	go func() {
		watcherService, err := services.NewWatcherService()
		if err != nil {
			log.Panic(err)
		}

		// Configuration
		watchDir := "./downloads"

		// UPDATED: Pointing to localhost:8000
		// I added "/upload" to the path, as APIs usually have an endpoint.
		// If your API accepts it at the root, remove "/upload".
		apiURL := "http://localhost:8000/upload"

		log.Printf("Starting watcher on %s sending to %s", watchDir, apiURL)
		watcherService.StartWatcher(watchDir, apiURL)
	}()

	r.Run("0.0.0.0:8080")
	// r.Run(":8080")
}
