package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"api/config"
	"api/database"
	"api/models"

	"github.com/fsnotify/fsnotify"
)

// AITriggerPayload represents the JSON payload sent to the Python AI node
type AITriggerPayload struct {
	FileID            uint              `json:"file_id"`
	FilePath          string            `json:"file_path"`
	FileName          string            `json:"file_name"`
	UserID            string            `json:"user_id"`
	ExistingFolders   []string          `json:"existing_folders"`
	AnalysisProvider  string            `json:"analysis_provider"`
	GeminiAPIKey      string            `json:"gemini_api_key"`
	GeminiModel       string            `json:"gemini_model"`
	FolderProfiles    map[string]string `json:"folder_profiles"`
	FileContentBase64 string            `json:"file_content_base64,omitempty"`
	MimeType          string            `json:"mime_type,omitempty"`
}

type AIAnalysisResponse struct {
	SuggestedFolder string              `json:"suggested_folder"`
	Tags            []string            `json:"tags"`
	ConfidenceScore int                 `json:"confidence_score"`
	Embedding       []float32           `json:"embedding"`
	Summary         string              `json:"summary"`
	Entities        map[string][]string `json:"entities"`
}

var (
	watcher     *fsnotify.Watcher
	watchMap    map[string]string
	done        chan bool
	aiSemaphore = make(chan struct{}, 2)
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

	watchMap = make(map[string]string)
	done = make(chan bool)

	for _, userAIConfig := range configs {
		if userAIConfig.OriginPath != "" && userAIConfig.Active {
			originPath := filepath.Clean(userAIConfig.OriginPath)

			// Attempt to translate the path to a Linux path
			if translator := NewPathTranslator(); translator != nil {
				if translated, err := translator.TranslatePath(originPath); err == nil {
					originPath = translated
				}
			}

			// Skip Windows-style paths (UNC mappings on client side only) if translation failed
			if strings.Contains(originPath, "\\") || (len(originPath) > 1 && originPath[1] == ':') {
				log.Printf("Skipping Windows path for user %s: %s (UNC mapping on client side only)", userAIConfig.UserID, originPath)
				continue
			}

			// Only process Linux-style absolute paths
			if !strings.HasPrefix(originPath, "/") {
				log.Printf("Skipping relative or invalid path for user %s: %s", userAIConfig.UserID, originPath)
				continue
			}

			// Ensure path exists
			os.MkdirAll(originPath, os.ModePerm)

			err = watcher.Add(originPath)
			if err != nil {
				log.Printf("Error adding watcher for user %s at %s: %v", userAIConfig.UserID, originPath, err)
				continue
			}

			watchMap[originPath] = userAIConfig.UserID
			log.Printf("Successfully watching: %s (User ID: %s)", originPath, userAIConfig.UserID)

			if userAIConfig.DestinationPath != "" {
				destPath := filepath.Clean(userAIConfig.DestinationPath)

				// Skip Windows paths for destination too
				if strings.Contains(destPath, "\\") || (len(destPath) > 1 && destPath[1] == ':') {
					log.Printf("Skipping Windows destination path for user %s: %s", userAIConfig.UserID, destPath)
				} else if strings.HasPrefix(destPath, "/") {
					os.MkdirAll(destPath, os.ModePerm)
				}
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

			// Detect file creation OR file rename/move (files moved to watched folder)
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				log.Printf("Watcher event detected [%s]: %s", eventTypeString(event.Op), event.Name)

				fileName := filepath.Base(event.Name)
				if strings.HasPrefix(fileName, ".") || strings.HasSuffix(fileName, ".tmp") || strings.HasSuffix(fileName, ".crdownload") {
					log.Printf("Skipping temporary/hidden file: %s", fileName)
					continue
				}

				// Check if file still exists (in case it was moved away)
				if _, err := os.Stat(event.Name); err != nil {
					log.Printf("File no longer exists or is not accessible: %s (error: %v)", event.Name, err)
					continue
				}

				dir := filepath.Clean(filepath.Dir(event.Name))
				userID, exists := watchMap[dir]
				if !exists {
					log.Printf("Watched path not found in map: %s", dir)
					continue
				}

				var userAIConfig models.UserAIConfig
				database.DB.Where("user_id = ?", userID).First(&userAIConfig)

				// Tiny delay for large file writes
				time.Sleep(500 * time.Millisecond)
				go processNewFile(event.Name, fileName, userID, userAIConfig)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// eventTypeString returns a human-readable string for fsnotify.Op
func eventTypeString(op fsnotify.Op) string {
	switch op {
	case fsnotify.Create:
		return "CREATE"
	case fsnotify.Write:
		return "WRITE"
	case fsnotify.Remove:
		return "REMOVE"
	case fsnotify.Rename:
		return "RENAME"
	case fsnotify.Chmod:
		return "CHMOD"
	default:
		return "UNKNOWN"
	}
}

func processNewFile(sourcePath, fileName string, userID string, userAIConfig models.UserAIConfig) {
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

	// Scan Existing Folders
	existingFolders := []string{}
	if userAIConfig.DestinationPath != "" {
		destPath := filepath.Clean(userAIConfig.DestinationPath)

		// Skip Windows-style paths
		if strings.Contains(destPath, "\\") || (len(destPath) > 1 && destPath[1] == ':') {
			log.Printf("Warning: Destination path is Windows format (UNC mapping): %s", destPath)
		} else if strings.HasPrefix(destPath, "/") {
			// Only process Linux-style absolute paths
			var scanFolders func(path string, currentDepth, maxDepth int)
			scanFolders = func(path string, currentDepth, maxDepth int) {
				if currentDepth > maxDepth {
					return
				}
				entries, err := os.ReadDir(path)
				if err != nil {
					log.Printf("Warning: Failed to scan directory %s: %v", path, err)
					return
				}
				for _, entry := range entries {
					if entry.IsDir() {
						if strings.HasPrefix(entry.Name(), ".") {
							continue // skip hidden folders
						}
						fullPath := filepath.Join(path, entry.Name())
						relPath, _ := filepath.Rel(destPath, fullPath)
						relPath = filepath.ToSlash(relPath) // Ensure standardized paths
						existingFolders = append(existingFolders, relPath)
						scanFolders(fullPath, currentDepth+1, maxDepth)
					}
				}
			}
			scanFolders(destPath, 1, 2)
		}
	}

	// Fetch Folder Descriptions for context
	folderProfiles := make(map[string]string)
	var dbProfiles []models.UserFolderProfile
	database.DB.Where("user_id = ?", userID).Find(&dbProfiles)
	for _, p := range dbProfiles {
		if strings.TrimSpace(p.Description) != "" {
			folderProfiles[p.FolderName] = p.Description
		}
	}

	// Trigger Python & Wait for Response (With Concurrency Control)
	// This prevents crashing the local Python GPU by blasting it with 50 simultaneous files
	startTime := time.Now()
	log.Printf("Queueing %s for AI analysis...", fileName)
	aiSemaphore <- struct{}{}
	aiResp, err := triggerAIAnalysis(metadata.ID, sourcePath, fileName, userID, existingFolders, folderProfiles, userAIConfig)
	<-aiSemaphore
	duration := time.Since(startTime)
	if err != nil {
		log.Printf("AI Analysis failed for %s: %v. File will remain in origin.", fileName, err)
		return
	}
	log.Printf("!!! TOTAL AI TURNAROUND TIME for %s: %v !!!", fileName, duration)

	personalizedFolder, folderProfileScore := SuggestFolderForFile(
		userID,
		aiResp.SuggestedFolder,
		existingFolders,
		fileName,
		aiResp.Summary,
		aiResp.Tags,
	)
	selectedFolder := aiResp.SuggestedFolder
	if strings.TrimSpace(personalizedFolder) != "" {
		selectedFolder = personalizedFolder
	}

	// OVERRIDE UNRELIABLE CONFIDENCE: LLMs are terrible at self-evaluating confidence.
	// We blend the LLM's guess with our robust mathematical Personalization Score.
	mathConfidence := folderProfileScore * 100.0
	if mathConfidence > 0 {
		aiResp.ConfidenceScore = int((mathConfidence * 0.75) + (float64(aiResp.ConfidenceScore) * 0.25))
	}

	suggestedFileName := fileName
	if userAIConfig.RenameFile {
		suggestedFileName = SuggestPersonalizedFileName(userID, fileName, aiResp.Tags, userAIConfig.RenameFormat)
	}

	// Save the summary generated by AI
	metadata.Summary = aiResp.Summary
	if entitiesBytes, err := json.Marshal(aiResp.Entities); err == nil {
		metadata.Entities = string(entitiesBytes)
	}
	database.DB.Save(&metadata)

	// Move the file based on AI suggestion and confidence thresholds
	finalPath := sourcePath

	// Convert thresholds to scale of 0-100 for comparison with AI response
	autoThreshold := userAIConfig.ConfidenceAuto
	rejectThreshold := userAIConfig.ConfidenceReject

	// If the DB stores them as 0.0-1.0, and AI returns 0-100, normalize autoThreshold and rejectThreshold
	if autoThreshold <= 1.0 && aiResp.ConfidenceScore > 1 {
		autoThreshold *= 100
	}
	if rejectThreshold <= 1.0 && aiResp.ConfidenceScore > 1 {
		rejectThreshold *= 100
	}

	log.Printf("Analyzing movement for %s: Confidence=%d, Auto=%f, Reject=%f",
		fileName, aiResp.ConfidenceScore, autoThreshold, rejectThreshold)

	// Prepare Log Entry (Always log what AI thought)
	logEntry := models.AIActionLog{
		UserID:   userID,
		FileID:   metadata.ID,
		Filename: fileName,
		Folder:   selectedFolder,
	}

	if float64(aiResp.ConfidenceScore) <= rejectThreshold {
		log.Printf("Confidence too low (%d <= %f). File will remain in origin.", aiResp.ConfidenceScore, rejectThreshold)
		logEntry.Action = "reject"
		logEntry.Description = fmt.Sprintf("Rejected (Low Confidence: %d%%)", aiResp.ConfidenceScore)
		logEntry.IsMove = false
		database.DB.Create(&logEntry)
		_ = RecordDecisionEvent(DecisionEventInput{
			UserID:            userID,
			FileID:            metadata.ID,
			Source:            "watcher",
			Outcome:           "rejected",
			ReasonCode:        "low_confidence",
			SuggestedFolder:   aiResp.SuggestedFolder,
			FinalFolder:       "",
			SuggestedFileName: suggestedFileName,
			FinalFileName:     fileName,
			ConfidenceScore:   aiResp.ConfidenceScore,
			Metadata: map[string]any{
				"profile_folder_score": folderProfileScore,
				"analysis_provider":    userAIConfig.AnalysisProvider,
			},
		})
		return
	}

	if userAIConfig.DestinationPath != "" && userAIConfig.AutoSelectFolder {
		destPath := filepath.Clean(userAIConfig.DestinationPath)

		// Skip file move if destination is Windows-style path
		if strings.Contains(destPath, "\\") || (len(destPath) > 1 && destPath[1] == ':') {
			logEntry.Action = "skip_move"
			logEntry.Description = fmt.Sprintf("Skipped: Destination path is Windows format (UNC mapping)")
			logEntry.IsMove = false
			database.DB.Create(&logEntry)
			log.Printf("Skipping file move - destination is Windows path: %s", destPath)
			_ = RecordDecisionEvent(DecisionEventInput{
				UserID:            userID,
				FileID:            metadata.ID,
				Source:            "watcher",
				Outcome:           "skipped",
				ReasonCode:        "windows_path",
				SuggestedFolder:   aiResp.SuggestedFolder,
				FinalFolder:       "",
				SuggestedFileName: suggestedFileName,
				FinalFileName:     fileName,
				ConfidenceScore:   aiResp.ConfidenceScore,
			})
			return
		}

		// Only proceed with move if destination is valid Linux path
		if !strings.HasPrefix(destPath, "/") {
			logEntry.Action = "skip_move"
			logEntry.Description = "Skipped: Invalid destination path (not absolute)"
			logEntry.IsMove = false
			database.DB.Create(&logEntry)
			log.Printf("Skipping file move - invalid destination path: %s", destPath)
			return
		}

		// Proceed with move
		targetDir := filepath.Join(destPath, selectedFolder)
		os.MkdirAll(targetDir, os.ModePerm)

		destFileName := fileName
		if userAIConfig.RenameFile {
			destFileName = EnsureUniqueName(targetDir, suggestedFileName)
		}

		finalDestPath := filepath.Join(targetDir, destFileName)

		err := copyFile(sourcePath, finalDestPath)
		if err != nil {
			log.Printf("Error moving file to NAS: %v", err)
			return
		}

		os.Remove(sourcePath)
		finalPath = finalDestPath
		log.Printf("Categorized and moved file to: %s (Confidence: %d)", finalPath, aiResp.ConfidenceScore)

		// Fetch User Settings to check if notifications are enabled
		var userSettings models.UserSetting
		database.DB.Where("user_id = ?", userID).First(&userSettings)

		// TRIGGER NOTIFICATION if user settings allow it
		if userSettings.AINotifications {
			log.Printf("Triggering SSE Notification for file: %s", fileName)
			NotifyFileMoved(fileName, aiResp.SuggestedFolder)
		}

		logEntry.Action = "move_file"
		logEntry.Description = fmt.Sprintf("Auto-organized into '%s' (Confidence: %d%%)", selectedFolder, aiResp.ConfidenceScore)
		logEntry.IsMove = true
		database.DB.Create(&logEntry)

		// Update Metadata with final path
		metadata.NASPath = finalPath
		metadata.FileName = destFileName
		database.DB.Save(&metadata)

		_ = RecordDecisionEvent(DecisionEventInput{
			UserID:            userID,
			FileID:            metadata.ID,
			Source:            "watcher",
			Outcome:           "auto_moved",
			ReasonCode:        "system_auto",
			SuggestedFolder:   aiResp.SuggestedFolder,
			FinalFolder:       selectedFolder,
			SuggestedFileName: suggestedFileName,
			FinalFileName:     destFileName,
			ConfidenceScore:   aiResp.ConfidenceScore,
			Metadata: map[string]any{
				"profile_folder_score": folderProfileScore,
				"analysis_provider":    userAIConfig.AnalysisProvider,
				"rename_enabled":       userAIConfig.RenameFile,
			},
		})
	} else {
		// Just log the suggestion without moving
		log.Printf("Auto-move disabled or no destination. Logging suggestion only.")

		var userSettings models.UserSetting
		database.DB.Where("user_id = ?", userID).First(&userSettings)
		if userSettings.AINotifications {
			NotifyApprovalNeeded(fileName, selectedFolder)
		}

		logEntry.Action = "suggestion"
		logEntry.Description = fmt.Sprintf("Suggested '%s' (Manual Mode)", selectedFolder)
		logEntry.IsMove = false
		database.DB.Create(&logEntry)
		_ = RecordDecisionEvent(DecisionEventInput{
			UserID:            userID,
			FileID:            metadata.ID,
			Source:            "watcher",
			Outcome:           "suggested",
			ReasonCode:        "manual_review",
			SuggestedFolder:   aiResp.SuggestedFolder,
			FinalFolder:       selectedFolder,
			SuggestedFileName: suggestedFileName,
			FinalFileName:     fileName,
			ConfidenceScore:   aiResp.ConfidenceScore,
			Metadata: map[string]any{
				"profile_folder_score": folderProfileScore,
				"analysis_provider":    userAIConfig.AnalysisProvider,
			},
		})
	}

	// Create Tags in DB (Always save tags even if not moved)
	for _, tagStr := range aiResp.Tags {
		tag := models.FileTag{
			FileID:  metadata.ID,
			TagName: tagStr,
		}
		database.DB.Create(&tag)
	}
	// Save Embedding Vector
	if len(aiResp.Embedding) > 0 {
		vectorBytes, err := json.Marshal(aiResp.Embedding)
		if err == nil {
			embeddingDoc := models.FileEmbedding{
				FileID:          metadata.ID,
				EmbeddingVector: string(vectorBytes),
			}
			database.DB.Create(&embeddingDoc)
			log.Printf("Saved vector embedding for %s (%d dims)", fileName, len(aiResp.Embedding))
		} else {
			log.Printf("Failed to marshal embeddings for %s: %v", fileName, err)
		}
	}
}

func triggerAIAnalysis(fileID uint, filePath string, fileName string, userID string, existingFolders []string, folderProfiles map[string]string, userAIConfig models.UserAIConfig) (*AIAnalysisResponse, error) {
	provider := strings.ToLower(strings.TrimSpace(userAIConfig.AnalysisProvider))
	if provider == "" {
		provider = "local"
	}

	payload := AITriggerPayload{
		FileID:           fileID,
		FilePath:         filePath,
		FileName:         fileName,
		UserID:           userID,
		ExistingFolders:  existingFolders,
		FolderProfiles:   folderProfiles,
		AnalysisProvider: provider,
		GeminiAPIKey:     userAIConfig.GeminiAPIKey,
		GeminiModel:      userAIConfig.GeminiModel,
	}

	if provider == "gemini" {
		fileBytes, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file for Gemini payload: %v", err)
		}
		payload.FileContentBase64 = base64.StdEncoding.EncodeToString(fileBytes)
		payload.MimeType = detectMimeType(fileName)
	}

	jsonData, _ := json.Marshal(payload)
	aiConfig := config.GetAIServiceConfig()
	aiEndpoint := aiConfig.Endpoint("/api/analyze/file")

	req, err := http.NewRequest("POST", aiEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", aiConfig.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
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

func detectMimeType(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
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
