package controllers

import (
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type FileSearchResult struct {
	Name       string    `json:"name"`
	FullPath   string    `json:"fullPath"`
	IsDir      bool      `json:"isDir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// SearchFiles - Search files with keyword matching
func SearchFiles(c *gin.Context) {
	query := c.Query("q")
	mode := c.DefaultQuery("mode", "normal")
	username := c.Query("username")

	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
		return
	}

	// AI mode is not implemented yet
	if mode == "ai" {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error":   "AI search not implemented yet",
			"message": "Please use normal mode for now",
		})
		return
	}

	// Search in user's NAS directory
	basePath := "/mnt/NAS"
	if username != "" {
		basePath = filepath.Join(basePath, username)
	}

	var results []FileSearchResult
	query = strings.ToLower(query)

	// Walk through directory and find matches
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Check if filename contains query
		if strings.Contains(strings.ToLower(info.Name()), query) {
			results = append(results, FileSearchResult{
				Name:       info.Name(),
				FullPath:   path,
				IsDir:      info.IsDir(),
				Size:       info.Size(),
				ModifiedAt: info.ModTime(),
			})
		}

		// Limit results to 50
		if len(results) >= 50 {
			return filepath.SkipAll
		}

		return nil
	})

	c.JSON(http.StatusOK, gin.H{
		"query":   query,
		"mode":    mode,
		"results": results,
		"count":   len(results),
	})
}

// GetRecommendedFolders - Get recommended folders from user's NAS directory
func GetRecommendedFolders(c *gin.Context) {
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username parameter is required"})
		return
	}

	basePath := filepath.Join("/mnt/NAS", username)

	// Check if directory exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "User directory not found"})
		return
	}

	// Read top-level folders
	entries, err := os.ReadDir(basePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read directory"})
		return
	}

	var folders []FileSearchResult
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			folders = append(folders, FileSearchResult{
				Name:       entry.Name(),
				FullPath:   filepath.Join(basePath, entry.Name()),
				IsDir:      true,
				Size:       0,
				ModifiedAt: info.ModTime(),
			})
		}

		// Limit to 6 folders for recommendations
		if len(folders) >= 6 {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"username": username,
		"folders":  folders,
		"count":    len(folders),
	})
}

// GetRecentViews - Get mocked recent views (random files from user directory)
func GetRecentViews(c *gin.Context) {
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username parameter is required"})
		return
	}

	basePath := filepath.Join("/mnt/NAS", username)

	// Check if directory exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "User directory not found"})
		return
	}

	// Collect all files and folders
	var allItems []FileSearchResult
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == basePath {
			return nil
		}

		allItems = append(allItems, FileSearchResult{
			Name:       info.Name(),
			FullPath:   path,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		})

		// Limit collection to 100 items
		if len(allItems) >= 100 {
			return filepath.SkipAll
		}

		return nil
	})

	// Randomly select up to 5 items
	rand.Seed(time.Now().UnixNano())
	var recentViews []FileSearchResult

	count := 5
	if len(allItems) < count {
		count = len(allItems)
	}

	// Shuffle and pick
	rand.Shuffle(len(allItems), func(i, j int) {
		allItems[i], allItems[j] = allItems[j], allItems[i]
	})

	for i := 0; i < count; i++ {
		recentViews = append(recentViews, allItems[i])
	}

	c.JSON(http.StatusOK, gin.H{
		"username": username,
		"items":    recentViews,
		"count":    len(recentViews),
	})
}
