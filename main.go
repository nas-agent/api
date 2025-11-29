package main

import (
	"api/config"
	"api/routes"
)

func main() {
	config.ConnectDatabase()
	config.Migrate()

	r := routes.SetupRouter()

	r.Run("0.0.0.0:8080") // http://localhost:8080
}
