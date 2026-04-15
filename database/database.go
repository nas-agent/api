package database

import (
	"log"
	"os"

	"api/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB
var ErrRecordNotFound = gorm.ErrRecordNotFound

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
		&models.DecisionEvent{},
		&models.UserSetting{},
		&models.FileMetadata{},
		&models.FileTag{},
		&models.FileEmbedding{},
		&models.FeedbackLog{},
		&models.UserFolderProfile{},
		&models.UserNamingProfile{},
		&models.Volume{},
		&models.UserVolume{},
		&models.Share{},
		// Phase 4A: Advanced Permissions
		&models.UserGroup{},
		&models.GroupMember{},
		&models.SharePermission{},
		&models.StorageQuota{},
		// Phase 4B: Volume Health & Monitoring
		&models.VolumeHealth{},
		&models.VolumeAlert{},
		&models.CleanupPolicy{},
	)
	if err != nil {
		log.Fatal("Failed to auto-migrate database. \n", err)
	}
}
