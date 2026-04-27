package main

import (
	"api/database"
	"api/models"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	_ = godotenv.Load()
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Bangkok",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
