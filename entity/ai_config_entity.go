package entity

import (
	"encoding/json"

	"gorm.io/gorm"
)

// AIConfig represents AI autosort configuration (shared or user-specific)
type AIConfig struct {
	gorm.Model
	UserID              *uint  `json:"user_id" gorm:"index"` // NULL for shared config
	IsShared            bool   `json:"is_shared" gorm:"default:false"`
	Enabled             bool   `json:"enabled" gorm:"default:true"`
	AutoRename          bool   `json:"auto_rename" gorm:"default:true"`
	IgnoreThreshold     int    `json:"ignore_threshold" gorm:"default:30"`
	AutomationThreshold int    `json:"automation_threshold" gorm:"default:85"`
	SourcePath          string `json:"source_path"`
	DestinationPath     string `json:"destination_path"`
	DeleteAfterImport   bool   `json:"delete_after_import" gorm:"default:true"`
	TargetExtensions    string `json:"target_extensions"` // JSON array stored as string
	NamingConvention    string `json:"naming_convention" gorm:"default:'category'"`
	ExcludedFolders     string `json:"excluded_folders"` // JSON array stored as string
}

// GetTargetExtensions parses the JSON string into a slice
func (c *AIConfig) GetTargetExtensions() []string {
	var extensions []string
	if c.TargetExtensions != "" {
		json.Unmarshal([]byte(c.TargetExtensions), &extensions)
	}
	return extensions
}

// SetTargetExtensions converts slice to JSON string
func (c *AIConfig) SetTargetExtensions(extensions []string) error {
	data, err := json.Marshal(extensions)
	if err != nil {
		return err
	}
	c.TargetExtensions = string(data)
	return nil
}

// GetExcludedFolders parses the JSON string into a slice
func (c *AIConfig) GetExcludedFolders() []string {
	var folders []string
	if c.ExcludedFolders != "" {
		json.Unmarshal([]byte(c.ExcludedFolders), &folders)
	}
	return folders
}

// SetExcludedFolders converts slice to JSON string
func (c *AIConfig) SetExcludedFolders(folders []string) error {
	data, err := json.Marshal(folders)
	if err != nil {
		return err
	}
	c.ExcludedFolders = string(data)
	return nil
}

// AIConfigDTO for API requests/responses
type AIConfigDTO struct {
	ID                  uint     `json:"id,omitempty"`
	UserID              *uint    `json:"user_id,omitempty"`
	Enabled             bool     `json:"enabled"`
	AutoRename          bool     `json:"autoRename"`
	IgnoreThreshold     int      `json:"ignoreThreshold"`
	AutomationThreshold int      `json:"automationThreshold"`
	SourcePath          string   `json:"sourcePath"`
	DestinationPath     string   `json:"destinationPath"`
	DeleteAfterImport   bool     `json:"deleteAfterImport"`
	TargetExtensions    []string `json:"targetExtensions"`
	NamingConvention    string   `json:"namingConvention"`
	ExcludedFolders     []string `json:"excludedFolders"`
}

// ToDTO converts entity to DTO
func (c *AIConfig) ToDTO() AIConfigDTO {
	return AIConfigDTO{
		ID:                  c.ID,
		UserID:              c.UserID,
		Enabled:             c.Enabled,
		AutoRename:          c.AutoRename,
		IgnoreThreshold:     c.IgnoreThreshold,
		AutomationThreshold: c.AutomationThreshold,
		SourcePath:          c.SourcePath,
		DestinationPath:     c.DestinationPath,
		DeleteAfterImport:   c.DeleteAfterImport,
		TargetExtensions:    c.GetTargetExtensions(),
		NamingConvention:    c.NamingConvention,
		ExcludedFolders:     c.GetExcludedFolders(),
	}
}

// UserConfigDTO extends AIConfigDTO with user information
type UserConfigDTO struct {
	AIConfigDTO
	UserName string `json:"userName"`
}
