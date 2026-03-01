package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"api/database"
	"api/models"

	"github.com/fsnotify/fsnotify"
)

// AITriggerPayload represents the JSON payload sent to the Python AI node
type AITriggerPayload struct {
	FileID   uint   `json:"file_id"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	UserID   uint   `json:"user_id"`
}

type AIAnalysisResponse struct {
	SuggestedFolder string   `json:"suggested_folder"`
	Tags            []string `json:"tags"`
	ConfidenceScore int      `json:"confidence_score"`
}

var (
	watcher  *fsnotify.Watcher
	watchMap map[string]uint
	done     chan bool
)

// InitFileWatcher starts a background service monitoring origin paths
func InitFileWatcher() {
	RefreshFileWatcher()
}

// RefreshFileWatcher stops the current watcher and re-initializes from DB
func RefreshFileWatcher() {
	if watcher != nil {
		log.Println("Closing existing file watcher...")
		watcher.Close()
	}
	if done != nil {
		close(done)
	}

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to initialize file watcher: %v", err)
		return
	}

	// Fetch all User AI Configurations
	var configs []models.UserAIConfig
	database.DB.Find(&configs)

	watchMap = make(map[string]uint)
	done = make(chan bool)

	for _, config := range configs {
		if config.OriginPath != "" {
			// Ensure path exists
			os.MkdirAll(config.OriginPath, os.ModePerm)

			err = watcher.Add(config.OriginPath)
			if err != nil {
				log.Printf("Error adding watcher for user %d at %s: %v", config.UserID, config.OriginPath, err)
				continue
			}

			watchMap[filepath.Clean(config.OriginPath)] = config.UserID
			log.Printf("Successfully watching: %s (User ID: %d)", config.OriginPath, config.UserID)

			if config.DestinationPath != "" {
				os.MkdirAll(config.DestinationPath, os.ModePerm)
			}
		}
	}

	// Start the background event loop
	go watchEventLoop()
}

func watchEventLoop() {
	log.Println("Background file watcher event loop started.")
	for {
		select {
		case <-done:
			log.Println("Watcher event loop stopped.")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Detect file creation (new file moved/downloaded)
			if event.Has(fsnotify.Create) {
				log.Printf("Watcher event detected: %s", event.Name)

				fileName := filepath.Base(event.Name)
				if strings.HasPrefix(fileName, ".") || strings.HasSuffix(fileName, ".tmp") || strings.HasSuffix(fileName, ".crdownload") {
					continue
				}

				dir := filepath.Clean(filepath.Dir(event.Name))
				userID, exists := watchMap[dir]
				if !exists {
					continue
				}

				var config models.UserAIConfig
				database.DB.Where("user_id = ?", userID).First(&config)

				// Tiny delay for large file writes
				time.Sleep(500 * time.Millisecond)
				go processNewFile(event.Name, fileName, userID, config)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

func processNewFile(sourcePath, fileName string, userID uint, config models.UserAIConfig) {
	// Create Initial Metadata (Pending AI Analysis)
	fileInfo, _ := os.Stat(sourcePath)
	size := int64(0)
	if fileInfo != nil {
		size = fileInfo.Size()
	}

	metadata := models.FileMetadata{
		NASPath:       sourcePath, // Temporarily point to the origin path
		FileName:      fileName,
		FileType:      filepath.Ext(fileName),
		FileSizeBytes: size,
		OwnerID:       userID,
		LastAccessed:  time.Now().Unix(),
	}

	database.DB.Create(&metadata)
	log.Printf("Saved initial metadata for: %s (ID: %d)", fileName, metadata.ID)

	// Trigger Python & Wait for Response
	aiResp, err := triggerAIAnalysis(metadata.ID, sourcePath, fileName, userID)
	if err != nil {
		log.Printf("AI Analysis failed for %s: %v. File will remain in origin.", fileName, err)
		return
	}

	// Move the file based on AI suggestion
	finalPath := sourcePath
	if config.DestinationPath != "" && config.AutoSelectFolder {
		// Append AI suggested folder
		targetDir := filepath.Join(config.DestinationPath, aiResp.SuggestedFolder)
		os.MkdirAll(targetDir, os.ModePerm)

		destPath := filepath.Join(targetDir, fileName)

		err := copyFile(sourcePath, destPath)
		if err != nil {
			log.Printf("Error moving file to NAS: %v", err)
			return
		}

		os.Remove(sourcePath)
		finalPath = destPath
		log.Printf("Categorized and moved file to: %s", finalPath)

		// Update Metadata with final path
		metadata.NASPath = finalPath
		database.DB.Save(&metadata)

		// Create Tags in DB
		for _, tagStr := range aiResp.Tags {
			tag := models.FileTag{
				FileID:  metadata.ID,
				TagName: tagStr,
			}
			database.DB.Create(&tag)
		}
	}
}

func triggerAIAnalysis(fileID uint, filePath string, fileName string, userID uint) (*AIAnalysisResponse, error) {
	payload := AITriggerPayload{
		FileID:   fileID,
		FilePath: filePath,
		FileName: fileName,
		UserID:   userID,
	}

	jsonData, _ := json.Marshal(payload)
	aiEndpoint := "http://localhost:8000/api/analyze/file"

	resp, err := http.Post(aiEndpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("AI node returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var aiResponse AIAnalysisResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode AI JSON response: %v", err)
	}

	log.Printf("AI analysis logic complete for %s. Suggested folder: %s", fileName, aiResponse.SuggestedFolder)
	return &aiResponse, nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
