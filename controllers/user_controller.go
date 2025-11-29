package controllers

import (
	"api/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetAllUsers - GET /users
func GetAllUsers(c *gin.Context) {
	userService := services.NewUserService()
	users, err := userService.GetAllUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	c.JSON(http.StatusOK, users)
}

// GetUser - GET /users/:id
func GetUser(c *gin.Context) {
	id := c.Param("id")
	userService := services.NewUserService()
	user, err := userService.GetUserByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, user)
}

// GetUserByUsername - GET /users/username/:username
func GetUserByUsername(c *gin.Context) {
	username := c.Param("username")
	userService := services.NewUserService()
	user, err := userService.GetUserByUsername(username)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, user)
}

// UpdateUser - PUT /users/:id
func UpdateUser(c *gin.Context) {
	id := c.Param("id")
	var input struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}

	if err := c.BindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userService := services.NewUserService()
	user, err := userService.UpdateUser(id, input.Email, input.Role)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found or update failed"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// DeleteUser - DELETE /users/:id
func DeleteUser(c *gin.Context) {
	id := c.Param("id")
	userService := services.NewUserService()
	if err := userService.DeleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// ChangePassword - POST /users/change-password
func ChangePassword(c *gin.Context) {
	var input struct {
		Username        string `json:"username" binding:"required"`
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userService := services.NewUserService()
	if err := userService.ChangePassword(input.Username, input.CurrentPassword, input.NewPassword); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "incorrect current password" {
			status = http.StatusUnauthorized
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}
