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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	SuggestedName   string              `json:"suggested_name"`
	Tags            []string            `json:"tags"`
	ConfidenceScore int                 `json:"confidence_score"`
	Embedding       []float32           `json:"embedding"`
	Summary         string              `json:"summary"`
	Entities        map[string][]string `json:"entities"`
}

var (
	watcher         *fsnotify.Watcher
	watchMap        map[string]string
	done            chan bool
	aiSemaphore     = make(chan struct{}, 2)
	processingFiles map[string]time.Time
	processingMu    sync.Mutex
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
	processingFiles = make(map[string]time.Time)

	for _, userAIConfig := range configs {
		if userAIConfig.OriginPath != "" && userAIConfig.Active {
			originPath := filepath.Clean(userAIConfig.OriginPath)

			// Skip Windows-style paths (UNC mappings on client side only)
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
				if strings.Contains(err.Error(), "permission denied") {
					log.Printf("⚠️ Permission denied for %s. Applying ACL VIP Pass...", originPath)
					
					hostUser := os.Getenv("USER")
					if hostUser == "" {
						if out, err := exec.Command("whoami").Output(); err == nil {
							hostUser = strings.TrimSpace(string(out))
						}
					}
					
					if hostUser != "" {
						// Grant access to the target folder AND the parent folder
						parentDir := filepath.Dir(originPath)
						log.Printf("🔑 Granting ACL access to parent: %s", parentDir)
						exec.Command("sudo", "setfacl", "-m", "u:"+hostUser+":rwx", parentDir).Run()
						exec.Command("sudo", "setfacl", "-m", "u:"+hostUser+":rwx", originPath).Run()
						
						// Try adding again after the ACL fix
						err = watcher.Add(originPath)
					}
				}
			}

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

			// LOG EVERY EVENT for diagnosis
			log.Printf("🔍 Watcher RAW EVENT: %s (Op: %s)", event.Name, event.Op.String())

			// Detect file creation, rename, OR write completion
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Write) {
				// 1. Check if the "event" is a new directory
				fileInfo, err := os.Stat(event.Name)
				if err == nil && fileInfo.IsDir() {
					log.Printf("📂 New subfolder detected: %s. Adding to watcher...", event.Name)
					
					// Find the userID for this branch
					parentDir := filepath.Clean(filepath.Dir(event.Name))
					if userID, exists := watchMap[parentDir]; exists {
						watcher.Add(event.Name)
						watchMap[event.Name] = userID
					}
					continue // Don't process the folder itself as a file
				}

				// 2. Avoid processing temporary or hidden files
				fileName := filepath.Base(event.Name)
				if strings.HasPrefix(fileName, ".") || strings.HasSuffix(fileName, ".tmp") || strings.HasSuffix(fileName, ".crdownload") {
					log.Printf("Skipping temporary/hidden file: %s", fileName)
					continue
				}

				// 3. Debounce: If we already triggered this file in the last 2 seconds, skip
				processingMu.Lock()
				if lastTrigger, exists := processingFiles[event.Name]; exists {
					if time.Since(lastTrigger) < 2*time.Second {
						processingMu.Unlock()
						continue
					}
				}
				processingFiles[event.Name] = time.Now()
				processingMu.Unlock()

				// Small delay to ensure file is fully written if it was a large copy
				time.Sleep(1 * time.Second)

				// 4. Find associated User ID
				dir := filepath.Clean(filepath.Dir(event.Name))
				userID, exists := watchMap[dir]
				if !exists {
					// Fallback: Check if the name itself is the watched dir (rare but happens on some OS)
					if uid, ok := watchMap[filepath.Clean(event.Name)]; ok {
						userID = uid
						exists = true
					}
				}

				if !exists {
					log.Printf("Watched path not found in map: %s", dir)
					continue
				}

				log.Printf("🚀 Triggering AI Analysis for: %s (User: %s)", event.Name, userID)
				var userAIConfig models.UserAIConfig
				database.DB.Where("user_id = ?", userID).First(&userAIConfig)
				
				go func(p string, n string, uid string, cfg models.UserAIConfig) {
					processNewFile(p, n, uid, cfg)
					// Clean up processing map after some time
					time.Sleep(10 * time.Second)
					processingMu.Lock()
					delete(processingFiles, p)
					processingMu.Unlock()
				}(event.Name, fileName, userID, userAIConfig)
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

// MetadataOnlySummary represents a condensed file analysis for batch clustering
type MetadataOnlySummary struct {
	FileName string              `json:"file_name"`
	Summary  string              `json:"summary"`
	Tags     []string            `json:"tags"`
	Entities map[string][]string `json:"entities"`
}

// ClusterRequest is the payload sent to the AI Agent for batch grouping
type ClusterRequest struct {
	UserID          string                `json:"user_id"`
	Files           []MetadataOnlySummary `json:"files"`
	ExistingFolders []string              `json:"existing_folders"`
	FolderProfiles  map[string]string     `json:"folder_profiles"`
	GeminiAPIKey    string                `json:"gemini_api_key"`
	GeminiModel     string                `json:"gemini_model"`
	RenameFile      bool                  `json:"rename_file"`
	RenameFormat    string                `json:"rename_format"`
}

// ClusterResponse is the "Master Plan" returned by the AI Agent
type ClusterResponse struct {
	FolderMap map[string]string `json:"folder_map"`
	NameMap   map[string]string `json:"name_map"`
	Rationale string            `json:"rationale"`
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

	// Scan Existing Folders for context
	existingFolders := listExistingFolders(userID, userAIConfig)
	folderProfiles := getFolderProfiles(userID)

	// Trigger Python & Wait for Response (With Concurrency Control)
	startTime := time.Now()
	aiSemaphore <- struct{}{}
	aiResp, err := triggerAIAnalysis(metadata.ID, sourcePath, fileName, userID, existingFolders, folderProfiles, userAIConfig)
	<-aiSemaphore
	
	if err != nil {
		log.Printf("AI Analysis failed for %s: %v. File will remain in origin.", fileName, err)
		return
	}
	log.Printf("!!! TOTAL AI TURNAROUND TIME for %s: %v !!!", fileName, time.Since(startTime))

	personalizedFolder, _ := SuggestFolderForFile(
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

	// Save the summary generated by AI
	metadata.Summary = aiResp.Summary
	if entitiesBytes, err := json.Marshal(aiResp.Entities); err == nil {
		metadata.Entities = string(entitiesBytes)
	}
	database.DB.Save(&metadata)

	// Move the file based on AI suggestion and confidence thresholds
	if userAIConfig.DestinationPath != "" && userAIConfig.AutoSelectFolder {
		// SAFETY CHECK: Ensure the path is a Linux path, not a Windows drive letter
		if !strings.HasPrefix(userAIConfig.DestinationPath, "/") {
			log.Printf("⚠️ File NOT moved. DestinationPath '%s' is not a Linux path. Please use a folder on the NAS (Z:\\...).", userAIConfig.DestinationPath)
			return
		}

		destPath := filepath.Clean(userAIConfig.DestinationPath)
		// ... existing move logic ...
		targetDir := filepath.Join(destPath, selectedFolder)
		log.Printf("[Cleaner] Decision: Move %s to %s", fileName, targetDir)
		os.MkdirAll(targetDir, os.ModePerm)

		destFileName := fileName
		if userAIConfig.RenameFile && aiResp.SuggestedName != "" {
			destFileName = aiResp.SuggestedName
		}
		
		// Ensure unique name in target directory
		destFileName = EnsureUniqueName(targetDir, destFileName)
		finalDestPath := filepath.Join(targetDir, destFileName)

		err := copyFile(sourcePath, finalDestPath)
		if err != nil {
			log.Printf("❌ Error moving file to NAS: %v", err)
			return
		}

		os.Remove(sourcePath)
		
		// Update Metadata with final path
		metadata.NASPath = finalDestPath
		database.DB.Save(&metadata)

		log.Printf("✅ Categorized and moved file to: %s", finalDestPath)
		NotifyFileMoved(fileName, selectedFolder)
	} else {
		log.Printf("ℹ️ File NOT moved. DestinationPath='%s', AutoSelectFolder=%v", userAIConfig.DestinationPath, userAIConfig.AutoSelectFolder)
	}
}

func triggerAIAnalysis(fileID uint, filePath string, fileName string, userID string, existingFolders []string, folderProfiles map[string]string, userAIConfig models.UserAIConfig) (*AIAnalysisResponse, error) {
	provider := strings.ToLower(strings.TrimSpace(userAIConfig.AnalysisProvider))
	if provider == "" {
		provider = "local"
	}

	// Translate path to Windows format if it's a remote agent
	translator := NewPathTranslator()
	winPath := translator.ToWindowsPath(userID, filePath)
	log.Printf("[AI] Forwarding path to agent: %s", winPath)

	payload := AITriggerPayload{
		FileID:           fileID,
		FilePath:         winPath,
		FileName:         fileName,
		UserID:           userID,
		ExistingFolders:  existingFolders,
		FolderProfiles:   folderProfiles,
		AnalysisProvider: provider,
		GeminiAPIKey:     userAIConfig.GeminiAPIKey,
		GeminiModel:      userAIConfig.GeminiModel,
	}

	// 2. Decide if we should stream the file content
	fileInfo, err := os.Stat(filePath)
	if err == nil && fileInfo.Size() < 20*1024*1024 {
		fileBytes, err := os.ReadFile(filePath)
		if err != nil && strings.Contains(err.Error(), "permission denied") {
			// Try to fix permission on the file itself
			log.Printf("🔑 File locked/permission denied for streaming: %s. Applying ACL...", fileName)
			exec.Command("sudo", "setfacl", "-m", "u:pi:r", filePath).Run()
			fileBytes, err = os.ReadFile(filePath)
		}

		if err == nil && fileBytes != nil {
			payload.FileContentBase64 = base64.StdEncoding.EncodeToString(fileBytes)
			payload.MimeType = detectMimeType(fileName)
			log.Printf("[AI] Successfully streaming file content (%d bytes)", len(fileBytes))
		} else if err != nil {
			log.Printf("⚠️ Streaming skipped: %v", err)
		}
	} else if err != nil {
		log.Printf("⚠️ os.Stat failed for %s: %v", filePath, err)
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

	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI analysis request failed: %v", err)
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

	return &aiResponse, nil
}

func detectMimeType(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// ScanOrigin implements the Three-Phase Batch Processing for In-Place Clustering
func ScanOrigin(userID string, customPath string) (int, error) {
	var userAIConfig models.UserAIConfig
	if err := database.DB.Where("user_id = ?", userID).First(&userAIConfig).Error; err != nil {
		return 0, fmt.Errorf("user AI configuration not found: %v", err)
	}

	if userAIConfig.OriginPath == "" {
		return 0, fmt.Errorf("origin path not configured for user")
	}

	originPath := filepath.Clean(userAIConfig.OriginPath)
	
	// If a custom sub-path is provided, use it instead of the default origin
	if customPath != "" {
		// Basic security: if it's not absolute, join it with the base
		if !strings.HasPrefix(customPath, "/") {
			originPath = filepath.Join(originPath, customPath)
		} else {
			originPath = filepath.Clean(customPath)
		}
	}

	if _, err := os.Stat(originPath); os.IsNotExist(err) {
		return 0, fmt.Errorf("scan path does not exist: %s", originPath)
	}

	// 1. GATHER FILES
	entries, err := os.ReadDir(originPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read origin directory: %v", err)
	}

	var filesToProcess []struct{ Path, Name string }
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		filesToProcess = append(filesToProcess, struct{ Path, Name string }{
			filepath.Join(originPath, entry.Name()), entry.Name(),
		})
	}

	if len(filesToProcess) == 0 {
		return 0, nil
	}

	// 2. PHASE 1: PERCEPTION (Gather Summaries)
	log.Printf("[Batch Scan] Starting Phase 1: Analyzing %d files...", len(filesToProcess))
	var summaries []MetadataOnlySummary
	existingFolders := listExistingFolders(userID, userAIConfig)
	folderProfiles := getFolderProfiles(userID)

	for _, f := range filesToProcess {
		// Temporary metadata for AI context
		meta := models.FileMetadata{OwnerID: userID, FileName: f.Name, NASPath: f.Path}
		database.DB.Create(&meta)

		aiResp, err := triggerAIAnalysis(meta.ID, f.Path, f.Name, userID, existingFolders, folderProfiles, userAIConfig)
		if err == nil {
			summaries = append(summaries, MetadataOnlySummary{
				FileName: f.Name,
				Summary:  aiResp.Summary,
				Tags:     aiResp.Tags,
				Entities: aiResp.Entities,
			})
			// Save summary to DB
			meta.Summary = aiResp.Summary
			database.DB.Save(&meta)
		}
		time.Sleep(100 * time.Millisecond) // Don't overwhelm agent
	}

	// 3. PHASE 2: GLOBAL DECISION (Clustering)
	log.Printf("[Batch Scan] Starting Phase 2: Requesting master plan...")
	clusterResp, err := triggerBatchClustering(userID, summaries, existingFolders, folderProfiles, userAIConfig)
	if err != nil {
		return 0, fmt.Errorf("batch clustering failed: %v", err)
	}

	// 4. PHASE 3: IN-PLACE MIGRATION
	log.Printf("[Batch Scan] Starting Phase 3: Executing in-place moves...")
	filesMoved := 0
	for _, f := range filesToProcess {
		targetSubfolder := clusterResp.FolderMap[f.Name]
		if targetSubfolder == "" {
			continue
		}
		
		suggestedName := ""
		if userAIConfig.RenameFile {
			suggestedName = clusterResp.NameMap[f.Name]
		}

		// Move files IN-PLACE (into subfolders of originPath)
		ExecuteInPlaceMove(f.Path, f.Name, suggestedName, targetSubfolder, originPath, userID)
		filesMoved++
	}

	return filesMoved, nil
}

func listExistingFolders(userID string, config models.UserAIConfig) []string {
	folders := []string{}
	if config.DestinationPath == "" {
		return folders
	}
	dest := filepath.Clean(config.DestinationPath)
	entries, _ := os.ReadDir(dest)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			folders = append(folders, e.Name())
		}
	}
	return folders
}

func getFolderProfiles(userID string) map[string]string {
	profiles := make(map[string]string)
	var dbProfiles []models.UserFolderProfile
	database.DB.Where("user_id = ?", userID).Find(&dbProfiles)
	for _, p := range dbProfiles {
		profiles[p.FolderName] = p.Description
	}
	return profiles
}

func triggerBatchClustering(userID string, summaries []MetadataOnlySummary, existing []string, profiles map[string]string, userAIConfig models.UserAIConfig) (*ClusterResponse, error) {
	payload := ClusterRequest{
		UserID:          userID,
		Files:           summaries,
		ExistingFolders: existing,
		FolderProfiles:  profiles,
		GeminiAPIKey:    userAIConfig.GeminiAPIKey,
		GeminiModel:     userAIConfig.GeminiModel,
		RenameFile:      userAIConfig.RenameFile,
		RenameFormat:    userAIConfig.RenameFormat,
	}

	jsonData, _ := json.Marshal(payload)
	aiConfig := config.GetAIServiceConfig()
	endpoint := aiConfig.Endpoint("/api/analyze/cluster")

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
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
		return nil, fmt.Errorf("AI agent returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var clusterResp ClusterResponse
	if err := json.NewDecoder(resp.Body).Decode(&clusterResp); err != nil {
		return nil, err
	}
	return &clusterResp, nil
}

func ExecuteInPlaceMove(sourcePath, fileName, suggestedName, targetFolder, originPath, userID string) {
	// Create subfolder directly in originPath
	targetDir := filepath.Join(originPath, targetFolder)
	os.MkdirAll(targetDir, os.ModePerm)

	finalName := fileName
	if suggestedName != "" {
		finalName = suggestedName
	}

	finalName = EnsureUniqueName(targetDir, finalName)
	destPath := filepath.Join(targetDir, finalName)
	
	err := copyFile(sourcePath, destPath)
	if err == nil {
		os.Remove(sourcePath)
		// Update DB metadata with new in-place location
		var meta models.FileMetadata
		if err := database.DB.Where("nas_path = ? AND owner_id = ?", sourcePath, userID).First(&meta).Error; err == nil {
			meta.NASPath = destPath
			meta.FileName = finalName
			database.DB.Save(&meta)
		}
		log.Printf("[Cleaner] Organized %s -> %s", finalName, targetFolder)
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


