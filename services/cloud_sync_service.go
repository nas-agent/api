package services

import (
	"api/database"
	"api/models"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────
// Mock Google Drive Client
// In production, replace with real google.golang.org/api/drive/v3 client
// ─────────────────────────────────────────────

type MockDriveFile struct {
	ID       string
	Name     string
	MimeType string
	Size     int64
	Path     string
}

type MockDriveClient struct {
	Email     string
	files     map[string]*MockDriveFile
	mu        sync.Mutex
	callDelay time.Duration
}

func NewMockDriveClient(email string) *MockDriveClient {
	return &MockDriveClient{
		Email:     email,
		files:     make(map[string]*MockDriveFile),
		callDelay: 80 * time.Millisecond, // simulate network latency
	}
}

func (c *MockDriveClient) Upload(localPath, driveFolderPath, fileName string, size int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	time.Sleep(c.callDelay) // simulate API call

	driveID := fmt.Sprintf("mock_drive_%d", time.Now().UnixNano())
	drivePath := filepath.Join(driveFolderPath, fileName)

	c.files[driveID] = &MockDriveFile{
		ID:   driveID,
		Name: fileName,
		Size: size,
		Path: drivePath,
	}

	log.Printf("[MockDrive] ✅ Uploaded: %s -> Drive:%s (id: %s)", localPath, drivePath, driveID)
	return driveID, nil
}

func (c *MockDriveClient) CreateFolder(parentID, name string) (string, error) {
	time.Sleep(c.callDelay)
	folderID := fmt.Sprintf("mock_folder_%s_%d", strings.ReplaceAll(name, " ", "_"), time.Now().UnixNano())
	log.Printf("[MockDrive] 📁 Created folder: %s (id: %s)", name, folderID)
	return folderID, nil
}

func (c *MockDriveClient) GetQuota() (used, total int64) {
	return 5_000_000_000, 15_000_000_000 // 5GB used / 15GB total (mock)
}

// ─────────────────────────────────────────────
// Cloud Sync Service
// ─────────────────────────────────────────────

type SyncResult struct {
	FilesSynced  int
	FilesSkipped int
	FilesFailed  int
	TotalBytes   int64
	Errors       []string
}

// RunCloudSync executes a full one-way sync of a user's NAS files to Google Drive.
func RunCloudSync(userID string) (*SyncResult, error) {
	// 1. Load config
	var config models.CloudSyncConfig
	if err := database.DB.Where("user_id = ?", userID).First(&config).Error; err != nil {
		return nil, fmt.Errorf("cloud sync not configured for user %s", userID)
	}
	if !config.Enabled {
		return nil, fmt.Errorf("cloud sync is disabled for this user")
	}

	// 2. Create a sync log entry
	syncLog := models.CloudSyncLog{
		UserID:    userID,
		Status:    "running",
		StartedAt: time.Now().Unix(),
	}
	database.DB.Create(&syncLog)

	result := &SyncResult{}
	var client *MockDriveClient

	if config.MockMode {
		client = NewMockDriveClient(config.DriveEmail)
		if client.Email == "" {
			client.Email = "mock-user@gmail.com"
		}
	} else {
		// TODO: Initialize real Google Drive client using config.AccessToken / config.RefreshToken
		return nil, fmt.Errorf("real Google Drive integration not yet enabled; use mock mode")
	}

	// 3. Fetch all files owned by this user
	var files []models.FileMetadata
	database.DB.Where("owner_id = ?", userID).Find(&files)

	log.Printf("[CloudSync] Starting sync for user %s — %d files to process", userID, len(files))

	rootFolderID := config.DriveFolderID
	if rootFolderID == "" {
		var err error
		rootFolderID, err = client.CreateFolder("root", "NAS-Agent Backup")
		if err != nil {
			finalizeSyncLog(syncLog.ID, "failed", result, err.Error())
			return nil, err
		}
		// Save root folder ID for future syncs
		database.DB.Model(&config).Update("drive_folder_id", rootFolderID)
	}

	// 4. Sync each file
	folderIDCache := map[string]string{"": rootFolderID}

	for _, file := range files {
		if file.NASPath == "" {
			result.FilesSkipped++
			continue
		}

		// Check if file exists on disk
		info, err := os.Stat(file.NASPath)
		if err != nil {
			log.Printf("[CloudSync] ⚠️ File not found on disk: %s", file.NASPath)
			result.FilesSkipped++
			continue
		}

		// Check if already synced and unchanged (idempotency)
		var existingSync models.CloudSyncFile
		alreadySynced := database.DB.Where("user_id = ? AND file_id = ? AND sync_status = 'synced'", userID, file.ID).First(&existingSync).Error == nil
		if alreadySynced && existingSync.LastSyncAt > file.UpdatedAt {
			result.FilesSkipped++
			continue
		}

		// Determine Drive sub-folder based on NAS folder structure
		nasDir := filepath.Dir(file.NASPath)
		driveFolderID, err := ensureDriveFolderPath(client, rootFolderID, nasDir, folderIDCache)
		if err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: folder error: %v", file.FileName, err))
			continue
		}

		// Upload file
		driveFileID, err := client.Upload(file.NASPath, nasDir, file.FileName, info.Size())
		if err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: upload error: %v", file.FileName, err))
			upsertCloudSyncFile(userID, file.ID, file.NASPath, "", "", "failed", 0, info.Size())
			continue
		}

		// Record the sync in DB
		upsertCloudSyncFile(userID, file.ID, file.NASPath, driveFileID, driveFolderID, "synced", time.Now().Unix(), info.Size())
		result.FilesSynced++
		result.TotalBytes += info.Size()
	}

	// 5. Update config timestamps
	now := time.Now().Unix()
	nextSync := calculateNextSync(config.Schedule, now)
	database.DB.Model(&config).Updates(map[string]interface{}{
		"last_sync_at": now,
		"next_sync_at": nextSync,
	})

	// 6. Finalize log
	status := "success"
	if result.FilesFailed > 0 && result.FilesSynced == 0 {
		status = "failed"
	} else if result.FilesFailed > 0 {
		status = "partial"
	}
	finalizeSyncLog(syncLog.ID, status, result, strings.Join(result.Errors, "; "))

	log.Printf("[CloudSync] ✅ Sync complete for user %s — synced: %d, skipped: %d, failed: %d",
		userID, result.FilesSynced, result.FilesSkipped, result.FilesFailed)

	return result, nil
}

func ensureDriveFolderPath(client *MockDriveClient, rootID, nasPath string, cache map[string]string) (string, error) {
	// Build a simple Drive sub-folder named after the last segment of the NAS path
	folderName := filepath.Base(nasPath)
	if folderName == "" || folderName == "." {
		return rootID, nil
	}

	cacheKey := nasPath
	if id, ok := cache[cacheKey]; ok {
		return id, nil
	}

	folderID, err := client.CreateFolder(rootID, folderName)
	if err != nil {
		return rootID, err
	}
	cache[cacheKey] = folderID
	return folderID, nil
}

func upsertCloudSyncFile(userID string, fileID uint, nasPath, driveFileID, drivePath, status string, syncedAt, size int64) {
	var record models.CloudSyncFile
	err := database.DB.Where("user_id = ? AND file_id = ?", userID, fileID).First(&record).Error

	if err != nil {
		// Create new
		database.DB.Create(&models.CloudSyncFile{
			UserID:        userID,
			FileID:        fileID,
			NASPath:       nasPath,
			DriveFileID:   driveFileID,
			DrivePath:     drivePath,
			SyncStatus:    status,
			LastSyncAt:    syncedAt,
			FileSizeBytes: size,
		})
	} else {
		// Update existing
		database.DB.Model(&record).Updates(map[string]interface{}{
			"drive_file_id":   driveFileID,
			"drive_path":      drivePath,
			"sync_status":     status,
			"last_sync_at":    syncedAt,
			"file_size_bytes": size,
		})
	}
}

func finalizeSyncLog(logID uint, status string, result *SyncResult, errMsg string) {
	database.DB.Model(&models.CloudSyncLog{}).Where("id = ?", logID).Updates(map[string]interface{}{
		"status":        status,
		"files_synced":  result.FilesSynced,
		"files_skipped": result.FilesSkipped,
		"files_failed":  result.FilesFailed,
		"total_bytes":   result.TotalBytes,
		"error_message": errMsg,
		"finished_at":   time.Now().Unix(),
	})
}

func calculateNextSync(schedule string, from int64) int64 {
	t := time.Unix(from, 0)
	switch schedule {
	case "weekly":
		return t.Add(7 * 24 * time.Hour).Unix()
	case "hourly":
		return t.Add(1 * time.Hour).Unix()
	default: // daily
		return t.Add(24 * time.Hour).Unix()
	}
}

// ─────────────────────────────────────────────
// Scheduler — runs in background, checks every 5min
// ─────────────────────────────────────────────

func StartCloudSyncScheduler() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		log.Println("☁️  Cloud Sync Scheduler started (checks every 5 minutes)")
		for range ticker.C {
			checkAndRunScheduledSyncs()
		}
	}()
}

func checkAndRunScheduledSyncs() {
	var configs []models.CloudSyncConfig
	now := time.Now().Unix()

	// Find all enabled configs where next_sync_at is in the past
	database.DB.Where("enabled = ? AND next_sync_at <= ? AND next_sync_at > 0", true, now).Find(&configs)

	for _, cfg := range configs {
		log.Printf("[CloudSync Scheduler] ⏰ Triggering scheduled sync for user %s", cfg.UserID)
		go func(userID string) {
			if _, err := RunCloudSync(userID); err != nil {
				log.Printf("[CloudSync Scheduler] ❌ Sync failed for user %s: %v", userID, err)
			}
		}(cfg.UserID)
	}
}
