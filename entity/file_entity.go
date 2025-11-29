package entity

import "time"

type FileItem struct {
	Name       string    `json:"name"`
	FullPath   string    `json:"full_path"`
	ModifiedAt time.Time `json:"modified_at"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
}
