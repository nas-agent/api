package config

import (
	"api/entity"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDatabase() {
	var err error
	DB, err = gorm.Open(sqlite.Open("nasgent.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	log.Println("Database connected successfully")
}

func Migrate() {
	err := DB.AutoMigrate(
		&entity.AgentModel{},
		&entity.AgentDecision{},
		&entity.User{},
		&entity.UserUsage{},
		&entity.UserFavorite{},
		&entity.File{},
		&entity.AIConfig{},
		&entity.AILimit{},
		&entity.GlobalAIConfig{},
		&entity.UserActivity{},
	)
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	log.Println("Database migrated successfully")
}
