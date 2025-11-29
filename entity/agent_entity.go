package entity

import "gorm.io/gorm"

type AgentModel struct {
	gorm.Model
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AgentDecision struct {
	gorm.Model
	FileName        string `json:"file_name"`
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Reason          string `json:"reason"`
}
