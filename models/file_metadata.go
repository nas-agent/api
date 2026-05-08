package models

type FileMetadata struct {
	ID            uint   `gorm:"primaryKey" json:"file_id"`
	NASPath       string `gorm:"uniqueIndex;not null" json:"nas_path"`
	FileName      string `gorm:"not null" json:"file_name"`
	FileType      string `json:"file_type"`
	FileSizeBytes int64  `json:"file_size_bytes"`
	CreatedAt     int64  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     int64  `gorm:"autoUpdateTime" json:"updated_at"`
	LastAccessed  int64  `json:"last_accessed"`
	OwnerID       string `gorm:"index" json:"owner_id"`
	Summary       string `gorm:"type:text" json:"summary"`
	SummaryTH     string `gorm:"type:text" json:"summary_th"`
	Description   string `gorm:"type:text" json:"description"`
	DescriptionTH string `gorm:"type:text" json:"description_th"`
	Entities      string `gorm:"type:text" json:"entities"`
	Status        string `json:"status"`

	// Relationships
	Tags       []FileTag       `gorm:"foreignKey:FileID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"tags"`
	Embeddings []FileEmbedding `gorm:"foreignKey:FileID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"embeddings"`
}
