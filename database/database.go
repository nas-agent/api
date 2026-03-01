package database

import (
	"log"
	"os"

	"api/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDB() {
	var err error

	// Create data directory if it doesn't exist
	os.MkdirAll("./data", os.ModePerm)

	DB, err = gorm.Open(sqlite.Open("./data/app.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database. \n", err)
	}

	log.Println("Connected to database successfully")

	// Auto Migrate schemas
	err = DB.AutoMigrate(
		&models.User{},
		&models.UserUsage{},
		&models.UserAIConfig{},
		&models.AIActionLog{},
		&models.UserSetting{},
		&models.FileMetadata{},
		&models.FileTag{},
		&models.FileEmbedding{},
		&models.FeedbackLog{},
	)
	if err != nil {
		log.Fatal("Failed to auto-migrate database. \n", err)
	}

	// Seed default data if needed
	seedData()
}

func seedData() {
	// For now, seed data logic is disabled because
	// settings are tied to a UserID
}
