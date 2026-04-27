package main

import (
	"api/database"
	"api/models"
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	db, err := gorm.Open(sqlite.Open("./data/app.db"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	database.DB = db

	var logs []models.AIActionLog
	database.DB.Find(&logs)
	fmt.Printf("Total Logs: %d\n", len(logs))
	for _, l := range logs {
		fmt.Printf("ID: %d, User: %s, Action: %s, Time: %d\n", l.LogID, l.UserID, l.Action, l.CreatedAt)
	}
}
