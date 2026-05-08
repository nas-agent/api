package controllers

import (
	"api/database"
	"api/models"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type FolderItem struct {
	Name       string       `json:"name"`
	Path       string       `json:"path"`
	IsDir      bool         `json:"is_dir"`
	Subfolders []FolderItem `json:"subfolders,omitempty"`
}

type FolderListResponse struct {
	Folders  []FolderItem `json:"folders"`
	IsDrives bool         `json:"isDrives"`
	Error    string       `json:"error,omitempty"`
}

// ListFolders lists directories at the given path with sudo elevation for permission bypass.
// Handles both Windows-style (C:\) and Unix-style paths (/mnt/, /home/, etc.)
func ListFolders(c *fiber.Ctx) error {
	var req struct {
		Path string `json:"path"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	path := strings.TrimSpace(req.Path)

	// Default to home directory if no path provided
	if path == "" {
		path = "/home"
	}

	log.Printf("Listing folders for path: %s", path)

	folders, isDrives, err := readFoldersWithSudo(path)
	if err != nil {
		log.Printf("Error reading folders: %v", err)
		return c.Status(500).JSON(FolderListResponse{
			Folders:  []FolderItem{},
			IsDrives: false,
			Error:    fmt.Sprintf("Cannot read path: %v", err),
		})
	}

	// Sort folders by name
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Name < folders[j].Name
	})

	return c.JSON(FolderListResponse{
		Folders:  folders,
		IsDrives: isDrives,
	})
}

// readFoldersWithSudo uses sudo to read directory contents, bypassing permission restrictions.
// This allows the API (running as unprivileged user) to list user home directories.
func readFoldersWithSudo(path string) ([]FolderItem, bool, error) {
	if path == "" {
		path = "/home"
	}

	// Validate path to prevent directory traversal
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, false, fmt.Errorf("invalid path: %v", err)
	}

	// Use sudo with ls to read directories we may not have permission for
	cmd := exec.Command("sudo", "ls", "-1", absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("sudo ls error for %s: %v (output: %s)", absPath, err, strings.TrimSpace(string(out)))
		return nil, false, fmt.Errorf("cannot read directory: %v", err)
	}

	entries := strings.Split(strings.TrimSpace(string(out)), "\n")
	var folders []FolderItem

	for _, entry := range entries {
		if entry == "" {
			continue
		}

		fullPath := filepath.Join(absPath, entry)

		// Check if it's a directory using sudo stat
		statCmd := exec.Command("sudo", "test", "-d", fullPath)
		if statCmd.Run() != nil {
			// Not a directory, skip it
			continue
		}

		folders = append(folders, FolderItem{
			Name:  entry,
			Path:  fullPath,
			IsDir: true,
		})
	}

	return folders, false, nil
}

// TestFolderBrowser tests the folder listing functionality and returns diagnostic info.
// Endpoint: POST /api/folders/test
// Returns: { can_sudo: bool, samba_home_base: string, test_path: string, folders: [], error?: string }
func TestFolderBrowser(c *fiber.Ctx) error {
	// Test if sudo works
	testCmd := exec.Command("sudo", "test", "-d", "/tmp")
	canSudo := testCmd.Run() == nil

	// Get SAMBA_HOME_BASE from environment
	sambaHomeBase := os.Getenv("SAMBA_HOME_BASE")
	if sambaHomeBase == "" {
		sambaHomeBase = "/srv/samba/homes"
	}

	// Try to list the SAMBA_HOME_BASE directory
	testPath := sambaHomeBase
	folders, _, err := readFoldersWithSudo(testPath)

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		log.Printf("TestFolderBrowser: Error listing %s: %v", testPath, err)
	}

	return c.JSON(fiber.Map{
		"can_sudo":           canSudo,
		"samba_home_base":    sambaHomeBase,
		"test_path":          testPath,
		"test_path_readable": err == nil,
		"folders_count":      len(folders),
		"folders":            folders,
		"error":              errMsg,
	})
}

// CreateFolder creates a new directory at the specified path.
func CreateFolder(c *fiber.Ctx) error {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	if req.Path == "" || req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Path and Name are required"})
	}

	fullPath := filepath.Join(req.Path, req.Name)

	// Use sudo mkdir to create folder with permissions
	cmd := exec.Command("sudo", "mkdir", "-p", fullPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Error creating folder: %v (output: %s)", err, string(out))
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to create folder: %v", string(out))})
	}

	// Set permissions so the user can actually use it
	exec.Command("sudo", "chmod", "777", fullPath).Run()

	return c.JSON(fiber.Map{
		"message": "Folder created successfully",
		"path":    fullPath,
	})
}

// BrowseNAS lists everything (files and folders) at the given path.
// If path is "/" or empty, it returns the user's shares.
func BrowseNAS(c *fiber.Ctx) error {
	var req struct {
		Path string `json:"path"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	path := strings.TrimSpace(req.Path)
	userID := GetUserID(c)

	// CASE 1: ROOT PATH - Return Shares
	if path == "" || path == "/" {
		var shares []models.Share
		// Filter: Only show Public shares OR Private shares owned by this user
		database.DB.Joins("INNER JOIN volumes ON shares.volume_id = volumes.id").
			Where("volumes.status = ? AND (shares.type = ? OR shares.owner_id = ?)", 
				"Mounted", models.ShareTypePublic, userID).
			Find(&shares)

		items := make([]map[string]interface{}, 0)
		for _, s := range shares {
			items = append(items, map[string]interface{}{
				"name":         s.Name,
				"path":         s.Path,
				"is_directory": true,
				"size_bytes":   0,
			})
		}
		return c.JSON(fiber.Map{"items": items, "is_root": true})
	}

	// CASE 2: SPECIFIC PATH - Return Files and Folders
	entries, err := readAllEntriesWithSudo(path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"items": entries, "is_root": false})
}

func readAllEntriesWithSudo(path string) ([]map[string]interface{}, error) {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("invalid path")
	}

	// Use ls -p to mark directories with / and -F to mark types
	cmd := exec.Command("sudo", "ls", "-1p", "--group-directories-first", absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cannot read directory")
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	items := make([]map[string]interface{}, 0)

	for _, line := range lines {
		if line == "" {
			continue
		}

		isDir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		fullPath := filepath.Join(absPath, name)

		// Get size for files
		var size int64 = 0
		if !isDir {
			sizeCmd := exec.Command("sudo", "stat", "-c", "%s", fullPath)
			sizeOut, _ := sizeCmd.Output()
			size, _ = strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
		}

		items = append(items, map[string]interface{}{
			"name":         name,
			"path":         fullPath,
			"is_directory": isDir,
			"size_bytes":   size,
		})
	}

	return items, nil
}

// readFolders is a fallback that reads directories without sudo (for testing/dev).
// Used when sudo is not available or for paths the API process can already read.
func readFolders(path string) ([]FolderItem, bool, error) {
	if path == "" {
		path = "/home"
	}

	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, false, fmt.Errorf("invalid path: %v", err)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, false, err
	}

	var folders []FolderItem
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		folders = append(folders, FolderItem{
			Name:  entry.Name(),
			Path:  filepath.Join(absPath, entry.Name()),
			IsDir: true,
		})
	}

	return folders, false, nil
}
