package controllers

import (
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

type AIMove struct {
	ID         int       `json:"id"`
	FileName   string    `json:"fileName"`
	SourcePath string    `json:"sourcePath"`
	DestPath   string    `json:"destPath"`
	Timestamp  time.Time `json:"timestamp"`
	Confidence float64   `json:"confidence"`
	Category   string    `json:"category"`
}

// GetRecentAIMoves - Get mocked AI file movement logs
func GetRecentAIMoves(c *gin.Context) {
	username := c.Query("username")

	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username parameter is required"})
		return
	}

	// Mock AI move logs
	basePath := filepath.Join("/mnt/NAS", username)

	categories := []string{"Documents", "Images", "Videos", "Music", "Archives"}
	fileNames := []string{
		"report_2024.pdf", "vacation.jpg", "meeting_notes.docx",
		"presentation.pptx", "photo_001.png", "video_clip.mp4",
		"song.mp3", "backup.zip", "invoice.pdf", "screenshot.png",
	}

	rand.Seed(time.Now().UnixNano())

	var mockedMoves []AIMove
	count := 5 // Show last 5 AI moves

	for i := 0; i < count; i++ {
		category := categories[rand.Intn(len(categories))]
		fileName := fileNames[rand.Intn(len(fileNames))]

		// Random timestamp within last 24 hours
		hoursAgo := rand.Intn(24)
		timestamp := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)

		mockedMoves = append(mockedMoves, AIMove{
			ID:         i + 1,
			FileName:   fileName,
			SourcePath: filepath.Join(basePath, "downloads", fileName),
			DestPath:   filepath.Join(basePath, category, fileName),
			Timestamp:  timestamp,
			Confidence: 0.85 + rand.Float64()*0.15, // 85% - 100%
			Category:   category,
		})
	}

	// Sort by timestamp (newest first)
	for i := 0; i < len(mockedMoves)-1; i++ {
		for j := i + 1; j < len(mockedMoves); j++ {
			if mockedMoves[i].Timestamp.Before(mockedMoves[j].Timestamp) {
				mockedMoves[i], mockedMoves[j] = mockedMoves[j], mockedMoves[i]
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"username": username,
		"moves":    mockedMoves,
		"count":    len(mockedMoves),
		"message":  fmt.Sprintf("Showing %d recent AI file movements", len(mockedMoves)),
	})
}
