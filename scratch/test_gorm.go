package main

import (
	"api/database"
	"api/models"
	"fmt"
)

func main() {
	database.ConnectDB()
	
	var logs []models.ActivityLog
	err := database.DB.Find(&logs).Error
	if err != nil {
		fmt.Printf("GORM Error: %v\n", err)
	} else {
		fmt.Printf("Found %d logs\n", len(logs))
	}
}
