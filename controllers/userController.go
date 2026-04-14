package controllers

import (
	"api/database"
	"api/models"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// GetUserID extracts user_id from JWT claims in the context
func GetUserID(c *fiber.Ctx) uint {
	user := c.Locals("user")
	if user == nil {
		// For demo/dev if middleware not fully enforced, try parsing manually or skip
		// In a real app, middleware would set this.
		return 0
	}
	token := user.(*jwt.Token)
	claims := token.Claims.(jwt.MapClaims)
	userID := uint(claims["user_id"].(float64))
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
		Username: username,
		Email:    email,
		Password: string(password),
		Role:     role,
	}

	result := database.DB.Create(&user)
	if result.Error != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Username or Email already exists or invalid data",
		})
	}

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

	if user.ID == 0 {
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
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{"message": "Unauthorized"})
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"message": "User not found"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(data["old_password"])); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "Incorrect old password",
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
	if userID == 0 {
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
	if err := database.DB.Find(&users).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"message": "Error fetching users"})
	}

	return c.JSON(users)
}

// DeleteUser removes a user from the database
func DeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"message": "Missing user ID"})
	}

	if err := database.DB.Delete(&models.User{}, id).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"message": "Error deleting user"})
	}

	return c.JSON(fiber.Map{
		"message": "User deleted successfully",
	})
}
