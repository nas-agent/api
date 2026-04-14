package main

import (
	"api/database"
	"api/models"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	database.ConnectDB()
	
	userID := uint(2) // faan
	var config models.UserAIConfig
	database.DB.Where("user_id = ?", userID).First(&config)
	fmt.Printf("DestinationPath: [%s]\n", config.DestinationPath)

	diskFolders := make(map[string]bool)
	if config.DestinationPath != "" {
		root := filepath.Clean(config.DestinationPath)
		fmt.Printf("Cleaned Root: [%s]\n", root)
		
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Printf("Walk error at %s: %v\n", path, err)
				return nil
			}
			if d.IsDir() && path != root {
				rel, _ := filepath.Rel(root, path)
				fmt.Printf("Found dir: [%s], Rel: [%s]\n", path, rel)
				// Limit to 2 levels deep
				if strings.Count(rel, string(os.PathSeparator)) < 2 {
					diskFolders[rel] = true
				} else {
					fmt.Printf("Skipping [%s] - too deep\n", rel)
				}
			}
			return nil
		})
		if err != nil {
			fmt.Printf("WalkDir returned error: %v\n", err)
		}
	}

	fmt.Printf("Total disk folders found: %d\n", len(diskFolders))
	for f := range diskFolders {
		fmt.Printf(" - %s\n", f)
	}
}
