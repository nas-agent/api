package database

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"api/models"
	"golang.org/x/crypto/bcrypt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB
var ErrRecordNotFound = gorm.ErrRecordNotFound

func ConnectDB() {
	var err error

	// Create data directory if it doesn't exist
	os.MkdirAll("./data", os.ModePerm)

	DB, err = gorm.Open(sqlite.Open("./data/app.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database. \n", err)
	}

	log.Println("Connected to database successfully")

	// Auto Migrate schemas
	err = DB.AutoMigrate(
		&models.User{},
		&models.ActivityLog{},
		&models.UserUsage{},
		&models.UserAIConfig{},
		&models.AIActionLog{},
		&models.DecisionEvent{},
		&models.UserSetting{},
		&models.FileMetadata{},
		&models.FileTag{},
		&models.FileEmbedding{},
		&models.FeedbackLog{},
		&models.UserFolderProfile{},
		&models.UserNamingProfile{},
		&models.Volume{},
		&models.UserVolume{},
		&models.Share{},
		&models.RaidArray{},
		// Phase 4A: Advanced Permissions
		&models.UserGroup{},
		&models.GroupMember{},
		&models.SharePermission{},
		&models.StorageQuota{},
		// Phase 4B: Volume Health & Monitoring
		&models.VolumeHealth{},
		&models.VolumeAlert{},
		&models.CleanupPolicy{},
	)
	if err != nil {
		log.Fatal("Failed to auto-migrate database. \n", err)
	}
}

// InitializeRaidArraysFromSystem detects existing RAID arrays from the system
// and populates the database with any missing entries
func InitializeRaidArraysFromSystem() {
	log.Println("Scanning system for existing RAID arrays...")

	// Run mdadm to detect existing RAID arrays
	cmd := exec.Command("sudo", "mdadm", "--examine", "--scan")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[RAID] No RAID arrays detected or mdadm not available: %v\n", err)
		return
	}

	// Parse mdadm output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "ARRAY") {
			continue
		}

		// Parse line like: ARRAY /dev/md/myraid metadata=1.2 name=myraid:0 UUID=...
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		devicePath := parts[1] // e.g., /dev/md/myraid or /dev/md594
		deviceName := strings.TrimPrefix(devicePath, "/dev/")

		// Avoid double /dev/ prefix
		if strings.HasPrefix(deviceName, "/") {
			deviceName = deviceName[1:]
		}

		// Check if this RAID array is already in database
		var existing models.RaidArray
		if err := DB.Where("raid_name = ? OR name = ?", deviceName, deviceName).First(&existing).Error; err == nil {
			log.Printf("[RAID] RAID %s already in database, skipping\n", deviceName)
			continue
		}

		// Get RAID details
		detailCmd := exec.Command("sudo", "mdadm", "--detail", devicePath)
		detailOutput, detailErr := detailCmd.CombinedOutput()
		if detailErr != nil {
			log.Printf("[RAID] Could not get details for %s: %v\n", devicePath, detailErr)
			continue
		}

		// Extract member devices from mdadm detail output
		// Look for lines like: /dev/sda1 as 0 (S) active sync (slot 0)
		var disk1, disk2 string
		detailLines := strings.Split(string(detailOutput), "\n")
		for _, detailLine := range detailLines {
			if strings.Contains(detailLine, "active sync") || strings.Contains(detailLine, "faulty") {
				fields := strings.Fields(detailLine)
				if len(fields) > 0 && (strings.HasPrefix(fields[0], "/dev/") || strings.HasPrefix(fields[0], "sd") || strings.HasPrefix(fields[0], "hd") || strings.HasPrefix(fields[0], "nvme")) {
					diskPath := fields[0]
					if !strings.HasPrefix(diskPath, "/dev/") {
						diskPath = "/dev/" + diskPath
					}
					if disk1 == "" {
						disk1 = diskPath
					} else if disk2 == "" {
						disk2 = diskPath
						break
					}
				}
			}
		}

		// Determine RAID level
		raidLevel := "RAID1"
		for _, detailLine := range detailLines {
			if strings.Contains(detailLine, "Raid Level") {
				if strings.Contains(detailLine, "raid5") {
					raidLevel = "RAID5"
				} else if strings.Contains(detailLine, "raid6") {
					raidLevel = "RAID6"
				} else if strings.Contains(detailLine, "raid0") {
					raidLevel = "RAID0"
				}
				break
			}
		}

		// Create RAID entry in database
		now := time.Now().Unix()
		raidEntry := models.RaidArray{
			ID:         "raid_" + strings.ReplaceAll(deviceName, "/", "_") + "_" + time.Now().Format("20060102_150405"),
			Name:       deviceName,
			RaidLevel:  raidLevel,
			RaidName:   deviceName,
			DevicePath: devicePath,
			Status:     "active",
			Disk1:      disk1,
			Disk2:      disk2,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := DB.Create(&raidEntry).Error; err != nil {
			log.Printf("[RAID] Error saving RAID %s to database: %v\n", deviceName, err)
		} else {
			log.Printf("[RAID] ✓ Populated existing RAID %s: %s + %s\n", deviceName, disk1, disk2)
		}
	}

	log.Println("RAID array initialization complete")
}

// EnsureAdminUser creates or updates a user with admin privileges
// Useful for debugging and first-time setup via CLI flags
func EnsureAdminUser(username, password string) {
	var user models.User
	
	// Try to find the user
	err := DB.Where("username = ?", username).First(&user).Error
	
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), 14)
	now := time.Now().Unix()

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("[Setup] Admin user '%s' not found, creating...\n", username)
			user = models.User{
				Username: username,
				Email:    username + "@local.nas",
				Password: string(hashedPassword),
				Role:     "admin",
				CreatedAt: now,
				UpdatedAt: now,
			}
			if createErr := DB.Create(&user).Error; createErr != nil {
				log.Printf("[Setup] Failed to create admin user: %v\n", createErr)
			} else {
				log.Printf("[Setup] ✓ Admin user '%s' created successfully\n", username)
			}
		} else {
			log.Printf("[Setup] Error looking up admin user: %v\n", err)
		}
	} else {
		log.Printf("[Setup] Admin user '%s' exists, updating password and ensuring admin role...\n", username)
		user.Password = string(hashedPassword)
		user.Role = "admin"
		user.UpdatedAt = now
		if saveErr := DB.Save(&user).Error; saveErr != nil {
			log.Printf("[Setup] Failed to update admin user: %v\n", saveErr)
		} else {
			log.Printf("[Setup] ✓ Admin user '%s' updated successfully\n", username)
		}
	}
}
