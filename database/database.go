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
		&models.Share{},
	)
	if err != nil {
		log.Fatal("Failed to auto-migrate database. \n", err)
	}

	// Seed default data if needed
	seedData()
}

func seedData() {
	var user models.User
	result := DB.First(&user)
	if result.Error != nil {
		// No users found, create a default one
		user = models.User{
			Username: "faan2",
			Email:    "faan2@example.com",
			Password: "password", // This should be hashed in production
		}
		DB.Create(&user)
		log.Println("Seeded default user: faan2")
	}

	// Ensure AI Config exists
	var aiConfig models.UserAIConfig
	if err := DB.Where("user_id = ?", user.ID).First(&aiConfig).Error; err != nil {
		aiConfig = models.UserAIConfig{
			UserID:           user.ID,
			OriginPath:       "C:/Users/rudfa/Downloads",
			DestinationPath:  "C:/Users/rudfa/Documents/NAS",
			RenameFile:       true,
			RenameFormat:     "opt1",
			AnalysisProvider: "local",
			GeminiModel:      "gemini-2.0-flash",
		}
		DB.Create(&aiConfig)
		log.Println("Seeded default AI config for user", user.ID)
	}

	// Ensure Settings exist
	var settings models.UserSetting
	if err := DB.Where("user_id = ?", user.ID).First(&settings).Error; err != nil {
		settings = models.UserSetting{
			UserID:          user.ID,
			Language:        "th",
			Theme:           "dark",
			LaunchOnStartup: false,
			AINotifications: true,
		}
		DB.Create(&settings)
		log.Println("Seeded default settings for user", user.ID)
	}
}
