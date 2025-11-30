package controllers

import (
	"api/entity"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func ListFilesAndFolders(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		path = "./"
	}

	// Security: Clean the path to prevent directory traversal attacks
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var items []entity.FileItem

	for _, entry := range entries {
		// We removed the 'if entry.IsDir()' check so it captures everything
		fullPath := filepath.Join(path, entry.Name())

		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, entity.FileItem{
			Name:       entry.Name(),
			FullPath:   fullPath,
			ModifiedAt: info.ModTime(),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"path":  path,
		"items": items,
	})
}

type RenameInput struct {
	OldPath string `json:"oldPath" binding:"required"`
	NewPath string `json:"newPath" binding:"required"`
}

func RenameItem(c *gin.Context) {
	var input RenameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Basic security check (very simple)
	input.OldPath = filepath.Clean(input.OldPath)
	input.NewPath = filepath.Clean(input.NewPath)

	if err := os.Rename(input.OldPath, input.NewPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Renamed successfully"})
}

func DeleteItem(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Path is required"})
		return
	}

	// Security check
	path = filepath.Clean(path)

	// RemoveAll deletes path and any children it contains.
	// It removes everything it can but returns the first error it encounters.
	// If the path does not exist, RemoveAll returns nil (no error).
	if err := os.RemoveAll(path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

func ServeFile(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Path is required"})
		return
	}

	// Security: Clean the path
	path = filepath.Clean(path)

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.File(path)
}
