package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"
	"os"
	"os/exec"
	"time"

	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// GetUserID extracts user_id from JWT claims in the context
func GetUserID(c *fiber.Ctx) string {
	user := c.Locals("user")
	if user == nil {
		return ""
	}
	token := user.(*jwt.Token)
	claims := token.Claims.(jwt.MapClaims)
	userID, ok := claims["user_id"].(string)
	if !ok {
		return ""
	}
	return userID
}

// Register a new user
func Register(c *fiber.Ctx) error {
	var data map[string]interface{}

	if err := c.BodyParser(&data); err != nil {
		return err
	}

	passwordStr, ok := data["password"].(string)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"message": "Password is required"})
	}

	password, _ := bcrypt.GenerateFromPassword([]byte(passwordStr), 14)

	username, _ := data["username"].(string)
	email, _ := data["email"].(string)
	role, _ := data["role"].(string)
	if role == "" {
		role = "user"
	}

	user := models.User{
		Username:           username,
		Email:              email,
		Password:           string(password),
		Role:               role,
		PersonalFolderPath: fmt.Sprintf("%s/%s", services.Samba.HomeBase, username),
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}

	result := database.DB.Create(&user)
	if result.Error != nil {
		errorMessage := "Username or Email already exists"
		if strings.Contains(result.Error.Error(), "users.email") {
			errorMessage = "Email address is already registered"
		} else if strings.Contains(result.Error.Error(), "users.username") {
			errorMessage = "Username is already taken"
		}

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": errorMessage,
			"error":   result.Error.Error(),
		})
	}

	// Sync to Samba
	if err := services.Samba.SyncSambaUser(username, passwordStr); err != nil {
		database.DB.Unscoped().Where("id = ?", user.ID).Delete(&models.User{})
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to provision Samba account",
			"error":   err.Error(),
		})
	}

	// Initialize Usage
	storageLimit, _ := data["storage_limit"].(float64)
	if storageLimit == 0 {
		storageLimit = 10
	}
	aiLimit, _ := data["ai_limit"].(float64)
	if aiLimit == 0 {
		aiLimit = 100
	}

	usage := models.UserUsage{
		UserID:           user.ID,
		StorageMB:        0,
		StorageLimitGB:   storageLimit,
		AIFileLimitDaily: int(aiLimit),
	}
	database.DB.Create(&usage)

	return c.JSON(user)
}

// Login user and return a signed JWT token
func Login(c *fiber.Ctx) error {
	var data map[string]interface{}

	if err := c.BodyParser(&data); err != nil {
		return err
	}

	username, _ := data["username"].(string)
	password, _ := data["password"].(string)

	var user models.User

	// Check by username (could also support email)
	database.DB.Where("username = ?", username).First(&user)

	if user.ID == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "User not found",
		})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Incorrect password",
		})
	}

	// Sign a dynamic JWT token
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "fallback_secret_for_local_dev" // Only for fallback locally
	}

	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"exp":      time.Now().Add(time.Hour * 72).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	t, err := token.SignedString([]byte(secret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Error logging in",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Success",
		"token":   t,
		"user": fiber.Map{
			"id":           user.ID,
			"username":     user.Username,
			"email":        user.Email,
			"created_date": user.CreatedAt,
		},
	})
}

// ChangePassword updates the user's password
func ChangePassword(c *fiber.Ctx) error {
	var data map[string]string

	if err := c.BodyParser(&data); err != nil {
		return err
	}

	// Identify current user via JWT
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"message": "Unauthorized"})
	}

	var user models.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"message": "User not found"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(data["old_password"])); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Incorrect old password",
		})
	}

	if err := services.Samba.SetSambaPassword(user.Username, data["new_password"]); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to update Samba password",
			"error":   err.Error(),
		})
	}

	newHashedPassword, _ := bcrypt.GenerateFromPassword([]byte(data["new_password"]), 14)
	user.Password = string(newHashedPassword)
	database.DB.Save(&user)

	return c.JSON(fiber.Map{
		"message": "Password updated successfully",
	})
}

// GetProfile returns the current logged-in user profile
func GetProfile(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"message": "Unauthorized"})
	}

	var user models.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"message": "User not found"})
	}

	return c.JSON(fiber.Map{
		"id":           user.ID,
		"username":     user.Username,
		"email":        user.Email,
		"created_date": user.CreatedAt,
	})
}

// GetUsers returns a list of all users
func GetUsers(c *fiber.Ctx) error {
	var users []models.User
	if err := database.DB.Preload("Usage").Find(&users).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"message": "Error fetching users"})
	}

	// Calculate AI usage for today for each user
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()

	for i := range users {
		var count int64
		database.DB.Model(&models.AIActionLog{}).
			Where("user_id = ? AND created_at >= ?", users[i].ID, todayStart).
			Count(&count)
		users[i].Usage.AIUsedToday = int(count)
	}

	return c.JSON(users)
}

// DeleteUser removes a user from the database
func DeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"message": "Missing user ID"})
	}

	// Delete User from DB
	var user models.User
	if err := database.DB.Where("id = ?", id).First(&user).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"message": "User not found"})
	}

	// 1. Find all shares owned by this user
	var userShares []models.Share
	database.DB.Where("owner_id = ?", user.ID).Find(&userShares)

	// 2. Remove each share from Samba config and also physically wipe the folder
	for _, share := range userShares {
		// Clean up smb.conf
		services.Samba.UnregisterShare(share.Name)

		// Unscoped delete from the database
		database.DB.Unscoped().Delete(&share)

		// Optionally wipe the physical data folder
		if share.Path != "" {
			_ = exec.Command("sudo", "rm", "-rf", share.Path).Run()
		}
	}

	// 3. Delete user completely from the database
	if err := database.DB.Unscoped().Where("id = ?", id).Delete(&models.User{}).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"message": "Error deleting user"})
	}

	// 4. Remove from Samba Identity / Linux System
	if user.Username != "" {
		services.Samba.RemoveSambaUser(user.Username)
	}

	return c.JSON(fiber.Map{
		"message": "User deleted successfully",
	})
}
