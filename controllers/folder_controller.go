package controllers

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

type FolderItem struct {
	Name       string    `json:"name"`
	FullPath   string    `json:"full_path"`
	ModifiedAt time.Time `json:"modified_at"`
}

func ListFoldersOnly(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		path = "./"
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var folders []FolderItem

	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(path, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			folders = append(folders, FolderItem{
				Name:       entry.Name(),
				FullPath:   fullPath,
				ModifiedAt: info.ModTime(),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"path":    path,
		"folders": folders,
	})
}
