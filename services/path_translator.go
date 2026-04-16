package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
// - Windows drive letters: Z:\faan -> /srv/samba/homes/faan
// - UNC paths: \\192.168.100.192\faan -> /srv/samba/homes/faan
// - SMB shares: \\rpi-hostname\faan -> /srv/samba/homes/faan
// - Already-Linux paths: /srv/samba/homes/faan -> /srv/samba/homes/faan
func (pt *PathTranslator) TranslatePath(inputPath string) (string, error) {
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
			// Build path: Samba home base + share name + subpath
			result := pt.SambaHomeBase + "/" + shareName
			if subpath != "" {
				result += "/" + subpath
			}
			log.Printf("[PathTranslator] Translated UNC path: %s -> %s", inputPath, result)
			return result, nil
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

		// Smart fallback: assume unknown Windows drive letters map to Samba home base
		// This is reasonable because:
		// - Most Windows clients access SMB shares through mapped drive letters
		// - All mapped drives likely point to the same Samba server
		// - So Z:\faan, Y:\faan, etc. all resolve to /srv/samba/homes/faan
		// Example: Z:\faan\Documents -> /srv/samba/homes/faan/Documents
		unixPath := strings.ReplaceAll(windowsPath, "\\", "/")
		result := pt.SambaHomeBase + "/" + unixPath
		log.Printf("[PathTranslator] Translated Windows path (auto-detected): %s -> %s (assumed drive letter maps to Samba home)", inputPath, result)
		return result, nil
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
