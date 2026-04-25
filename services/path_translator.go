package services

import (
	"api/database"
	"api/models"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type PathTranslator struct {
	RpiHostname         string
	RpiIP               string
	SambaHomeBase       string
	DriveLetterMappings map[string]string // e.g., "Z" -> "/srv/samba/homes"
}

// NewPathTranslator creates a new path translator instance
func NewPathTranslator() *PathTranslator {
	pt := &PathTranslator{
		SambaHomeBase:       getSambaHomeBase(),
		DriveLetterMappings: make(map[string]string),
	}

	// Get RPI hostname and IP
	pt.RpiHostname = getRpiHostname()
	pt.RpiIP = getRpiIP()

	return pt
}

// TranslatePath converts any path format to canonical Linux path
// Supports:
// - Windows drive letters: Z:\faan -> /mnt/suanton/homes/faan
// - UNC paths: \\192.168.100.192\faan -> /mnt/suanton/homes/faan
// - SMB shares: \\rpi-hostname\faan -> /mnt/suanton/homes/faan
// - Already-Linux paths: /mnt/suanton/homes/faan -> /mnt/suanton/homes/faan
func (pt *PathTranslator) TranslatePath(userID string, inputPath string) (string, error) {
	if inputPath == "" {
		return "", fmt.Errorf("empty path")
	}

	inputPath = strings.TrimSpace(inputPath)

	// Case 1: Already a Linux path
	if strings.HasPrefix(inputPath, "/") {
		log.Printf("[PathTranslator] Path already Linux format: %s", inputPath)
		return inputPath, nil
	}

	// Case 2: UNC path format (\\host\share\path)
	// Examples:
	// \\192.168.100.192\faan\Documents
	// \\rpi-hostname\faan
	if strings.HasPrefix(inputPath, "\\\\") {
		uncPath := strings.TrimPrefix(inputPath, "\\\\")
		parts := strings.Split(uncPath, "\\")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid UNC path format: %s", inputPath)
		}

		host := parts[0]
		shareName := parts[1]
		subpath := strings.Join(parts[2:], "/") // Convert remaining backslashes to forward slashes

		// Check if host matches our RPI hostname or IP
		isLocalHost := strings.EqualFold(host, pt.RpiHostname) || host == pt.RpiIP

		// Smart fallback: if host is an IP address and doesn't match our RPI,
		// still attempt translation (might be the RPI under a different IP)
		// Example: \\192.168.100.195\faan could be the RPI accessed via different network
		if !isLocalHost {
			// Try to parse as IP - if it's an IP address, it's likely pointing to the RPI network
			isIPAddress := strings.Count(host, ".") == 3 || strings.Contains(host, ":")
			if !isIPAddress {
				// It's a hostname but doesn't match ours
				return "", fmt.Errorf("unknown SMB host: %s", host)
			}
			// For IP addresses that don't match exactly, assume they're referring to the RPI
			isLocalHost = true
		}

		if isLocalHost {
			// Find the user's share path from the database
			var share models.Share
			if err := database.DB.Where("owner_id = ? AND type = 'Private'", userID).First(&share).Error; err != nil {
				return "", fmt.Errorf("could not find private share for user: %v", err)
			}

			// Build path: Share's real path + subpath (if any)
			// The shareName in UNC is usually just the username or share name
			result := share.Path
			if subpath != "" {
				// If the UNC path was \\host\share\folder, we need to handle if 'share' matches our share name
				if !strings.EqualFold(shareName, share.Name) {
					// If it's a sub-folder of the share, append it
					result = filepath.Join(result, subpath)
				} else {
					// If it's just the share, result is already share.Path
					result = filepath.Join(result, subpath)
				}
			}
			log.Printf("[PathTranslator] Translated UNC path via DB: %s -> %s", inputPath, result)
			return filepath.Clean(result), nil
		}

		return "", fmt.Errorf("unknown SMB host: %s", host)
	}

	// Case 3: Windows drive letter format (Z:\path)
	// Pattern: [A-Z]:\path
	drivePattern := regexp.MustCompile(`^([A-Za-z]):\\(.*)$`)
	matches := drivePattern.FindStringSubmatch(inputPath)
	if len(matches) == 3 {
		driveLetter := strings.ToUpper(matches[1])
		windowsPath := matches[2]

		// Check if we have a registered mapping for this drive letter
		if baseMapping, exists := pt.DriveLetterMappings[driveLetter]; exists {
			// Convert Windows backslashes to forward slashes
			unixPath := strings.ReplaceAll(windowsPath, "\\", "/")
			result := baseMapping + "/" + unixPath
			log.Printf("[PathTranslator] Translated Windows path (registered): %s -> %s", inputPath, result)
			return result, nil
		}

		// Accurate translation: lookup the user's private share path
		var share models.Share
		if err := database.DB.Where("owner_id = ? AND type = 'Private'", userID).First(&share).Error; err != nil {
			return "", fmt.Errorf("could not find private share for user: %v", err)
		}

		unixPath := strings.ReplaceAll(windowsPath, "\\", "/")
		result := filepath.Join(share.Path, unixPath)
		log.Printf("[PathTranslator] Translated Windows path via DB: %s -> %s", inputPath, result)
		return filepath.Clean(result), nil
	}

	return "", fmt.Errorf("unsupported path format: %s", inputPath)
}

func getRpiHostname() string {
	hostname, _ := os.Hostname()
	return hostname
}

func getRpiIP() string {
	// Try to get the IP address via hostname resolution or system command
	// For now, check common RPI network interfaces
	cmd := exec.Command("hostname", "-I")
	output, err := cmd.Output()
	if err == nil {
		ips := strings.Fields(strings.TrimSpace(string(output)))
		if len(ips) > 0 {
			return ips[0] // Return first IP (typically eth0)
		}
	}
	return "127.0.0.1"
}

// RegisterDriveLetterMapping registers a Windows drive letter to a Linux path
// Example: RegisterDriveLetterMapping("Z", "/srv/samba/homes")
func (pt *PathTranslator) RegisterDriveLetterMapping(driveLetter, linuxPath string) {
	pt.DriveLetterMappings[strings.ToUpper(driveLetter)] = linuxPath
}

// GetSMBConfig returns the SMB configuration for frontend consumption
func (pt *PathTranslator) GetSMBConfig() map[string]interface{} {
	return map[string]interface{}{
		"rpi_hostname":    pt.RpiHostname,
		"rpi_ip":          pt.RpiIP,
		"samba_home_base": pt.SambaHomeBase,
		"unc_path_format": fmt.Sprintf("\\\\%s\\{share_name}", pt.RpiIP),
	}
}

// ToWindowsPath converts a Linux NAS path back to a Windows-friendly UNC path
func (pt *PathTranslator) ToWindowsPath(userID string, linuxPath string) string {
	// Find the user's private share
	var share models.Share
	if err := database.DB.Where("owner_id = ? AND type = 'Private'", userID).First(&share).Error; err != nil {
		// Fallback to public share check if private not found
		if err := database.DB.Where("type = 'Public'").First(&share).Error; err != nil {
			return linuxPath
		}
	}

	// Standardize paths
	cleanLinux := filepath.Clean(linuxPath)
	cleanShare := filepath.Clean(share.Path)

	if strings.HasPrefix(cleanLinux, cleanShare) {
		relPath, _ := filepath.Rel(cleanShare, cleanLinux)
		// Construct UNC path: \\IP\share_name\subfolder\file.ext
		uncPath := fmt.Sprintf("\\\\%s\\%s", pt.RpiIP, share.Name)
		if relPath != "." {
			uncPath = fmt.Sprintf("%s\\%s", uncPath, strings.ReplaceAll(relPath, "/", "\\"))
		}
		return uncPath
	}

	return linuxPath
}
