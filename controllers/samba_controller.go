package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func GetShares(c *fiber.Ctx) error {
	var shares []models.Share
	database.DB.Find(&shares)

	// In a real implementation, we might want to enrichment this data
	// with real-time status from Samba, but for now, we serve from DB.
	return c.JSON(shares)
}

func CreateShare(c *fiber.Ctx) error {
	var input struct {
		Name     string           `json:"name"`
		Type     models.ShareType `json:"type"`
		OwnerID  string           `json:"owner_id"`
		VolumeID string           `json:"volume_id"`
		IsPublic bool             `json:"is_public"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	// 1. Lookup Volume and get mount point
	var volume models.Volume
	if input.VolumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "volume_id is required"})
	}

	if err := database.DB.Where("id = ?", input.VolumeID).First(&volume).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Volume not found"})
	}

	// Validate volume is mounted
	if volume.Status != "Mounted" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume is not mounted"})
	}

	// 2. Determine Path using volume mount point
	var path string
	if input.Type == models.ShareTypePrivate {
		// Find username for the owner
		var user models.User
		if err := database.DB.Where("id = ?", input.OwnerID).First(&user).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "Owner not found"})
		}
		path = fmt.Sprintf("%s/homes/%s", volume.MountPoint, user.Username)
	} else {
		// Public share
		path = fmt.Sprintf("%s/shares/%s", volume.MountPoint, input.Name)
	}

	// 2.5 Create the share directory on disk if it doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Create parent directories with sudo if needed
		mkdirCmd := exec.Command("sudo", "mkdir", "-p", path)
		if output, err := mkdirCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to create share directory %s: %v, output: %s\n", path, err, string(output))
			return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to create share directory: %s", string(output))})
		}
		log.Printf("Created share directory: %s\n", path)
	}

	// 2.6 Set proper permissions on the share directory
	if input.Type == models.ShareTypePrivate {
		// Private share: set owner and permissions
		var user models.User
		database.DB.Where("id = ?", input.OwnerID).First(&user)

		// chown user:sambashare path
		chownCmd := exec.Command("sudo", "chown", fmt.Sprintf("%s:sambashare", user.Username), path)
		if output, err := chownCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to chown %s: %v, output: %s\n", path, err, string(output))
		}

		// chmod 2770 path (group sticky bit, rwx for user and group)
		chmodCmd := exec.Command("sudo", "chmod", "2770", path)
		if output, err := chmodCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to chmod %s: %v, output: %s\n", path, err, string(output))
		}
	} else {
		// Public share: world readable/writable
		// chown nobody:sambashare path
		chownCmd := exec.Command("sudo", "chown", "nobody:sambashare", path)
		if output, err := chownCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to chown %s: %v, output: %s\n", path, err, string(output))
		}

		// chmod 2777 path (group sticky bit, rwx for all)
		chmodCmd := exec.Command("sudo", "chmod", "2777", path)
		if output, err := chmodCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to chmod %s: %v, output: %s\n", path, err, string(output))
		}
	}

	// 3. Create in DB
	share := models.Share{
		ID:       uuid.New().String(),
		Name:     input.Name,
		Path:     path,
		Type:     input.Type,
		OwnerID:  input.OwnerID,
		VolumeID: input.VolumeID,
		Status:   "Active",
		Protocol: "SMB",
	}

	if err := database.DB.Create(&share).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to save share to database"})
	}

	// 4. Sync to System
	var ownerName string
	if input.OwnerID != "" {
		var user models.User
		database.DB.Where("id = ?", input.OwnerID).First(&user)
		ownerName = user.Username
	}

	if err := services.Samba.RegisterShare(share.Name, share.Path, ownerName, input.IsPublic); err != nil {
		// Log error but don't fail the request completely if DB succeeded
		// In a production app, we'd want better rollback logic.
		fmt.Printf("Warning: Failed to sync share to Samba: %v\n", err)
	}

	return c.JSON(share)
}

func DeleteShare(c *fiber.Ctx) error {
	id := c.Params("id")
	var share models.Share
	if err := database.DB.Where("id = ?", id).First(&share).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Share not found"})
	}

	// 1. Unregister from Samba system
	if err := services.Samba.UnregisterShare(share.Name); err != nil {
		fmt.Printf("Warning: Failed to remove share from Samba config: %v\n", err)
	}

	// 2. Delete from DB
	database.DB.Unscoped().Delete(&share)

	return c.JSON(fiber.Map{"message": "Share removed from database and Samba config"})
}

// DiagoseShare checks if a share is properly configured and accessible
func DiagnosticShare(c *fiber.Ctx) error {
	shareID := c.Params("id")

	type DiagnosticResult struct {
		ShareID      string   `json:"share_id"`
		ShareName    string   `json:"share_name"`
		SharePath    string   `json:"share_path"`
		PathExists   bool     `json:"path_exists"`
		PathWritable bool     `json:"path_writable"`
		SambaRunning bool     `json:"samba_running"`
		ConfigValid  bool     `json:"config_valid"`
		Permissions  string   `json:"permissions"`
		Issues       []string `json:"issues"`
	}

	result := DiagnosticResult{
		ShareID: shareID,
		Issues:  []string{},
	}

	// 1. Find share in database
	var share models.Share
	if err := database.DB.Where("id = ?", shareID).First(&share).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Share not found"})
	}

	result.ShareName = share.Name
	result.SharePath = share.Path

	// 2. Check if path exists
	if _, err := os.Stat(share.Path); os.IsNotExist(err) {
		result.PathExists = false
		result.Issues = append(result.Issues, fmt.Sprintf("Share path does not exist: %s", share.Path))
	} else {
		result.PathExists = true

		// 3. Check if path is writable
		testFile := share.Path + "/.samba_write_test"
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			result.PathWritable = false
			result.Issues = append(result.Issues, fmt.Sprintf("Share path is not writable: %v", err))
		} else {
			result.PathWritable = true
			os.Remove(testFile) // cleanup
		}

		// 4. Check permissions
		info, _ := os.Stat(share.Path)
		result.Permissions = info.Mode().String()
	}

	// 5. Check if Samba is running
	cmd := exec.Command("sudo", "systemctl", "is-active", "smbd")
	if err := cmd.Run(); err != nil {
		result.SambaRunning = false
		result.Issues = append(result.Issues, "Samba daemon (smbd) is not running")
	} else {
		result.SambaRunning = true
	}

	// 6. Validate smb.conf contains this share
	cmd = exec.Command("sudo", "testparm", "-s")
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.ConfigValid = false
		result.Issues = append(result.Issues, "smb.conf validation failed")
	} else {
		configStr := string(output)
		if !strings.Contains(configStr, "["+share.Name+"]") {
			result.ConfigValid = false
			result.Issues = append(result.Issues, fmt.Sprintf("Share [%s] not found in smb.conf", share.Name))
		} else {
			result.ConfigValid = true
		}
	}

	// If no issues, add success message
	if len(result.Issues) == 0 {
		result.Issues = []string{"✅ Share is fully configured and accessible"}
	}

	return c.JSON(result)
}

// GetShareDiagnostics checks all shares for issues
func GetShareDiagnostics(c *fiber.Ctx) error {
	type ShareStatus struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Path   string   `json:"path"`
		Status string   `json:"status"` // "OK", "WARNING", "ERROR"
		Issues []string `json:"issues"`
	}

	var shares []models.Share
	database.DB.Find(&shares)

	if shares == nil {
		shares = []models.Share{}
	}

	statuses := make([]ShareStatus, 0)

	for _, share := range shares {
		status := ShareStatus{
			ID:     share.ID,
			Name:   share.Name,
			Path:   share.Path,
			Status: "OK",
			Issues: []string{},
		}

		// Check path exists
		if _, err := os.Stat(share.Path); os.IsNotExist(err) {
			status.Status = "ERROR"
			status.Issues = append(status.Issues, fmt.Sprintf("Path does not exist: %s", share.Path))
		} else {
			// Check writable
			testFile := share.Path + "/.test"
			if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
				status.Status = "WARNING"
				status.Issues = append(status.Issues, "Path exists but not writable")
			} else {
				os.Remove(testFile)
			}
		}

		statuses = append(statuses, status)
	}

	return c.JSON(statuses)
}
