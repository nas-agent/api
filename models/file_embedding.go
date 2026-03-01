package models

type FileEmbedding struct {
	ID              uint   `gorm:"primaryKey" json:"embedding_id"`
	FileID          uint   `gorm:"uniqueIndex;not null" json:"file_id"`
	VectorIndexKey  string `json:"vector_index_key"`
	EmbeddingVector string `json:"embedding_vector"`
	LastIndexed     int64  `gorm:"autoUpdateTime" json:"last_indexed"`
}
