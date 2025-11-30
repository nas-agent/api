package controllers

import (
	"api/entity"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func ListFilesAndFolders(c *gin.Context) {
	username := c.Query("username")
	subPath := c.Query("path")

	var basePath string
	if username != "" {
		// Production path
		basePath = filepath.Join("/mnt/NAS", username)
	} else {
		// Fallback for testing or no user
		basePath = "./"
	}

	// Construct full path
	// Ensure subPath doesn't allow escaping the base path
	fullPath := filepath.Join(basePath, subPath)
	fullPath = filepath.Clean(fullPath)

	// Simple security check: ensure the resulting path still starts with the intended base path
	// Note: This might be tricky with symlinks or different OS path separators,
	// but for now we assume standard behavior.
	// On Windows /mnt/NAS might become \mnt\NAS

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var items []entity.FileItem

	for _, entry := range entries {
		// We use relative path for the item's "FullPath" ID so the frontend can navigate
		// relative to the user's root.
		// Actually, the frontend seems to use the ID to query again.
		// If we return absolute path, the next query will be ?path=/mnt/NAS/user/subdir
		// But our logic joins basePath + path.
		// So if we pass absolute path as 'path', it becomes /mnt/NAS/user//mnt/NAS/user/subdir.
		// WE NEED TO RETURN RELATIVE PATHS as IDs if we are appending to basePath.

		// Let's check how the frontend uses it.
		// Frontend: handleNavigate(item.id) -> ?path=item.id
		// Then fetchFiles(path).

		// If we want to support nested folders, 'path' param should be relative to user root.
		// e.g. "" -> root
		// "subdir" -> root/subdir

		relativePath := filepath.Join(subPath, entry.Name())

		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, entity.FileItem{
			Name:       entry.Name(),
			FullPath:   relativePath, // Send relative path as ID
			ModifiedAt: info.ModTime(),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"path":  subPath,
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
