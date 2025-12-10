package controllers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
)

// Structs for lsblk JSON output
type LsblkDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Size       string        `json:"size"`
	MountPoint string        `json:"mountpoint"`
	FSType     string        `json:"fstype"`
	Model      string        `json:"model"`
	Serial     string        `json:"serial"`
	Hotplug    bool          `json:"hotplug"`
	Children   []LsblkDevice `json:"children,omitempty"`
}

type LsblkOutput struct {
	BlockDevices []LsblkDevice `json:"blockdevices"`
}

// Response structures
type PhysicalDisk struct {
	ID          string `json:"id"`
	DevicePath  string `json:"devicePath"`
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Size        string `json:"size"`
	Type        string `json:"type"`
	Connection  string `json:"connection"`
	Status      string `json:"status"`
	Temperature int    `json:"temperature"`
	Role        string `json:"role"`
}

type LogicalVolume struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	MountPoint string   `json:"mountPoint"`
	FileSystem string   `json:"fileSystem"`
	RaidLevel  string   `json:"raidLevel"`
	RaidState  string   `json:"raidState"`
	TotalSize  string   `json:"totalSize"`
	UsedSize   string   `json:"usedSize"`
	Disks      []string `json:"disks"`
}

// GetStorageDevices - Get real storage devices from Linux system
func GetStorageDevices(c *gin.Context) {
	// Execute lsblk command to get block devices
	cmd := exec.Command("lsblk", "-J", "-o", "NAME,TYPE,SIZE,MOUNTPOINT,FSTYPE,MODEL,SERIAL,HOTPLUG")
	output, err := cmd.Output()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to read storage devices",
			"details": err.Error(),
		})
		return
	}

	var lsblkData LsblkOutput
	if err := json.Unmarshal(output, &lsblkData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to parse storage data",
			"details": err.Error(),
		})
		return
	}

	var mountedVolumes []LogicalVolume
	var unmountedDisks []PhysicalDisk

	// Process each block device
	for _, device := range lsblkData.BlockDevices {
		// Skip loop devices and other virtual devices
		if device.Type == "loop" || device.Type == "rom" {
			continue
		}

		devicePath := "/dev/" + device.Name

		// Check if it's a disk (not a partition)
		if device.Type == "disk" {
			// Determine connection type (USB vs internal)
			connection := "SATA"
			if device.Hotplug {
				connection = "USB 3.0"
			}

			// Determine disk type
			diskType := "HDD"
			if strings.Contains(strings.ToLower(device.Model), "ssd") {
				diskType = "SSD"
			} else if strings.Contains(strings.ToLower(device.Model), "flash") || strings.Contains(strings.ToLower(device.Model), "usb") {
				diskType = "Flash"
			}

			// Check if disk has any mounted partitions
			hasMountedPartitions := false
			if device.Children != nil {
				for _, child := range device.Children {
					if child.MountPoint != "" {
						hasMountedPartitions = true

						// Add as mounted volume
						mountedVolumes = append(mountedVolumes, LogicalVolume{
							ID:         child.Name,
							Name:       deviceName(device.Model, child.Name),
							MountPoint: child.MountPoint,
							FileSystem: strings.ToUpper(child.FSType),
							RaidLevel:  "Single",
							RaidState:  "Active",
							TotalSize:  child.Size,
							UsedSize:   "0", // Would need df to get actual usage
							Disks:      []string{device.Name},
						})
					}
				}
			}

			// If no mounted partitions, it's unassigned
			if !hasMountedPartitions && device.MountPoint == "" {
				unmountedDisks = append(unmountedDisks, PhysicalDisk{
					ID:          device.Name,
					DevicePath:  devicePath,
					Model:       device.Model,
					Serial:      device.Serial,
					Size:        device.Size,
					Type:        diskType,
					Connection:  connection,
					Status:      "Healthy",
					Temperature: 0, // Would need smartctl to get real temp
					Role:        "Unassigned",
				})
			} else if device.MountPoint != "" {
				// Disk itself is mounted (no partitions)
				mountedVolumes = append(mountedVolumes, LogicalVolume{
					ID:         device.Name,
					Name:       deviceName(device.Model, device.Name),
					MountPoint: device.MountPoint,
					FileSystem: strings.ToUpper(device.FSType),
					RaidLevel:  "Single",
					RaidState:  "Active",
					TotalSize:  device.Size,
					UsedSize:   "0",
					Disks:      []string{device.Name},
				})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"mounted":   mountedVolumes,
		"unmounted": unmountedDisks,
		"timestamp": getCurrentTimestamp(),
	})
}

// Helper function to create friendly device name
func deviceName(model, deviceID string) string {
	if model != "" {
		return model
	}
	return deviceID
}

// Helper to get current timestamp
func getCurrentTimestamp() string {
	return exec.Command("date", "+%Y-%m-%d %H:%M:%S").String()
}
