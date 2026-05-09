package models

// CloudSyncConfig stores per-user Google Drive sync preferences
type CloudSyncConfig struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	UserID        string `gorm:"uniqueIndex;not null" json:"user_id"`
	Enabled       bool   `gorm:"default:false" json:"enabled"`
	Schedule      string `gorm:"default:'daily'" json:"schedule"` // "daily", "weekly", "manual"
	MockMode      bool   `gorm:"default:true" json:"mock_mode"`  // Use mock API (true) or real Google (false)
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	AccessToken   string `gorm:"type:text" json:"access_token"`   // OAuth access token (encrypted in prod)
	RefreshToken  string `gorm:"type:text" json:"refresh_token"`  // OAuth refresh token
	DriveEmail    string `json:"drive_email"`                     // Connected Google account email
	DriveFolderID string `json:"drive_folder_id"`                 // Root Drive folder ID for NAS backups
	LastSyncAt    int64  `json:"last_sync_at"`
	NextSyncAt    int64  `json:"next_sync_at"`
	CreatedAt     int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     int64  `gorm:"autoUpdateTime" json:"updated_at"`
}

// CloudSyncLog records each sync job attempt
type CloudSyncLog struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	UserID       string `gorm:"index;not null" json:"user_id"`
	Status       string `json:"status"`        // "running", "success", "partial", "failed"
	FilesSynced  int    `json:"files_synced"`
	FilesSkipped int    `json:"files_skipped"`
	FilesFailed  int    `json:"files_failed"`
	TotalBytes   int64  `json:"total_bytes"`
	ErrorMessage string `gorm:"type:text" json:"error_message"`
	StartedAt    int64  `gorm:"autoCreateTime" json:"started_at"`
	FinishedAt   int64  `json:"finished_at"`
}

// CloudSyncFile tracks individual file sync state
type CloudSyncFile struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	UserID      string `gorm:"index;not null" json:"user_id"`
	FileID      uint   `gorm:"index" json:"file_id"`
	NASPath     string `gorm:"not null" json:"nas_path"`
	DriveFileID string `json:"drive_file_id"` // Google Drive file ID
	DrivePath   string `json:"drive_path"`    // Human-readable Drive path
	SyncStatus  string `json:"sync_status"`   // "synced", "pending", "failed", "deleted"
	LastSyncAt  int64  `json:"last_sync_at"`
	FileSizeBytes int64 `json:"file_size_bytes"`
	CreatedAt   int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   int64  `gorm:"autoUpdateTime" json:"updated_at"`
}
