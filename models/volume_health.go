package models

import (
	"gorm.io/gorm"
)

type DiskHealthStatus string

const (
	HealthStatusGood     DiskHealthStatus = "Good"
	HealthStatusWarning  DiskHealthStatus = "Warning"
	HealthStatusCritical DiskHealthStatus = "Critical"
	HealthStatusFailing  DiskHealthStatus = "Failing"
	HealthStatusUnknown  DiskHealthStatus = "Unknown"
)

type AlertSeverity string

const (
	AlertLevelInfo     AlertSeverity = "Info"
	AlertLevelWarning  AlertSeverity = "Warning"
	AlertLevelCritical AlertSeverity = "Critical"
)

// VolumeHealth stores disk SMART data and health status
type VolumeHealth struct {
	ID              string           `gorm:"primaryKey" json:"id"`
	VolumeID        string           `gorm:"index;not null" json:"volume_id"`
	Status          DiskHealthStatus `gorm:"default:'Unknown'" json:"status"`
	Temperature     float32          `json:"temperature"` // Celsius
	UsedSpace       int64            `json:"used_space"`  // Bytes
	TotalSpace      int64            `json:"total_space"` // Bytes
	ReadsPerSecond  int              `json:"reads_per_second"`
	WritesPerSecond int              `json:"writes_per_second"`
	ErrorCount      int              `json:"error_count"`
	SMARTScore      int              `json:"smart_score"` // 0-100
	LastCheckTime   int64            `gorm:"index" json:"last_check_time"`
	NextCheckTime   int64            `json:"next_check_time"`
	CreatedAt       int64            `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       int64            `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt   `gorm:"index" json:"-"`

	// Relationships
	Volume Volume        `gorm:"foreignKey:VolumeID;constraint:OnDelete:CASCADE" json:"volume,omitempty"`
	Alerts []VolumeAlert `gorm:"foreignKey:VolumeID;constraint:OnDelete:CASCADE" json:"alerts,omitempty"`
}

// VolumeAlert stores alerts raised for volumes
type VolumeAlert struct {
	ID         string         `gorm:"primaryKey" json:"id"`
	VolumeID   string         `gorm:"index;not null" json:"volume_id"`
	Severity   AlertSeverity  `gorm:"index;default:'Warning'" json:"severity"`
	AlertType  string         `gorm:"index" json:"alert_type"` // "HighTemp", "HighUsage", "HighError", "SMART"
	Message    string         `json:"message"`
	Resolved   bool           `gorm:"default:false" json:"resolved"`
	CreatedAt  int64          `gorm:"autoCreateTime" json:"created_at"`
	ResolvedAt int64          `json:"resolved_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	Volume Volume `gorm:"foreignKey:VolumeID;constraint:OnDelete:CASCADE" json:"volume,omitempty"`
}

type CleanupAction string

const (
	CleanupActionArchive  CleanupAction = "Archive"
	CleanupActionCompress CleanupAction = "Compress"
	CleanupActionDelete   CleanupAction = "Delete"
	CleanupActionNotify   CleanupAction = "Notify"
)

// CleanupPolicy defines automatic cleanup when space is low
type CleanupPolicy struct {
	ID                   string         `gorm:"primaryKey" json:"id"`
	VolumeID             string         `gorm:"index;not null" json:"volume_id"`
	Enabled              bool           `gorm:"default:false" json:"enabled"`
	TriggerThreshold     int            `gorm:"default:90" json:"trigger_threshold"`       // Trigger at 90% usage
	MaxCleanupPercentage int            `gorm:"default:20" json:"max_cleanup_percentage"`  // Free up max 20%
	Action               CleanupAction  `gorm:"default:'Notify'" json:"action"`            // What to do
	FileAgeThresholdDays int            `gorm:"default:30" json:"file_age_threshold_days"` // Files older than this
	ExcludePatterns      string         `json:"exclude_patterns"`                          // Comma-separated patterns to exclude
	LastRunTime          int64          `json:"last_run_time"`
	NextRunTime          int64          `json:"next_run_time"`
	CreatedAt            int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`

	// Relationships
	Volume Volume `gorm:"foreignKey:VolumeID;constraint:OnDelete:CASCADE" json:"volume,omitempty"`
}
