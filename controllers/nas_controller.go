package controllers

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/shirou/gopsutil/v4/disk"
)

// Frontend expected types
type PhysicalDisk struct {
	ID          string  `json:"id"`
	DevicePath  string  `json:"devicePath"`
	Model       string  `json:"model"`
	Serial      string  `json:"serial"`
	Size        string  `json:"size"`
	Type        string  `json:"type"`
	Connection  string  `json:"connection"`
	Status      string  `json:"status"`
	Temperature float64 `json:"temperature"`
	Role        string  `json:"role"`
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

// lsblk JSON Parsing
type LsblkOutput struct {
	Blockdevices []LsblkDevice `json:"blockdevices"`
}

type LsblkDevice struct {
	Name       string        `json:"name"`
	Kname      string        `json:"kname"`
	Label      string        `json:"label"`
	Mountpoint string        `json:"mountpoint"`
	Size       string        `json:"size"`
	Fstype     string        `json:"fstype"`
	Model      string        `json:"model"`
	Serial     string        `json:"serial"`
	Type       string        `json:"type"`
	Tran       string        `json:"tran"`
	Vendor     string        `json:"vendor"`
	Children   []LsblkDevice `json:"children"`
}

func GetStorageDevices(c *fiber.Ctx) error {
	// Execute lsblk to get full picture of hardware and mount points
	cmd := exec.Command("lsblk", "--json", "-o", "NAME,KNAME,LABEL,MOUNTPOINT,SIZE,FSTYPE,MODEL,SERIAL,TYPE,TRAN,VENDOR")
	output, err := cmd.Output()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to execute lsblk: " + err.Error()})
	}

	var lsblk LsblkOutput
	if err := json.Unmarshal(output, &lsblk); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to parse lsblk output: " + err.Error()})
	}

	var mounted []LogicalVolume
	var unmounted []PhysicalDisk

	for _, dev := range lsblk.Blockdevices {
		// Process each block device (disk, lvm, raid)
		if dev.Type == "disk" {
			// A disk might have partitions. We need to check if ANY of its children are mounted.
			hasMountedChild := false
			processDeviceTree(dev, &mounted, &unmounted, &hasMountedChild)

			// If neither the disk nor any children are mounted, it's an unmounted physical drive
			if dev.Mountpoint == "" && !hasMountedChild {
				unmounted = append(unmounted, PhysicalDisk{
					ID:          dev.Kname,
					DevicePath:  "/dev/" + dev.Kname,
					Model:       strings.TrimSpace(dev.Vendor + " " + dev.Model),
					Serial:      dev.Serial,
					Size:        dev.Size,
					Type:        "HDD", // Could detect SSD via /sys/block/.../rotational
					Connection:  strings.ToUpper(dev.Tran),
					Status:      "Healthy",
					Temperature: 32, // Stub for now
					Role:        "Unassigned",
				})
			}
		} else if dev.Type == "raid1" || dev.Type == "raid0" || dev.Type == "raid5" {
			// Handle virtual RAID devices directly
			if dev.Mountpoint != "" {
				addMountedVolume(dev, &mounted)
			}
		}
	}

	return c.JSON(fiber.Map{
		"mounted":   mounted,
		"unmounted": unmounted,
	})
}

// processDeviceTree recursively looks for mount points in partitions or logical volumes
func processDeviceTree(dev LsblkDevice, mounted *[]LogicalVolume, unmounted *[]PhysicalDisk, hasMountedChild *bool) {
	if dev.Mountpoint != "" {
		*hasMountedChild = true
		addMountedVolume(dev, mounted)
	}

	for _, child := range dev.Children {
		processDeviceTree(child, mounted, unmounted, hasMountedChild)
	}
}

func addMountedVolume(dev LsblkDevice, mounted *[]LogicalVolume) {
	// Skip system-critical paths that shouldn't be managed/shared easily
	if dev.Mountpoint == "/" || dev.Mountpoint == "/boot" || strings.HasPrefix(dev.Mountpoint, "/boot/") {
		return
	}

	// Capture usage stats
	usage, err := disk.Usage(dev.Mountpoint)
	var total, used string
	if err == nil {
		total = strconv.FormatUint(usage.Total, 10)
		used = strconv.FormatUint(usage.Used, 10)
	} else {
		total = dev.Size
		used = "0"
	}

	name := dev.Label
	if name == "" {
		name = dev.Name
	}

	*mounted = append(*mounted, LogicalVolume{
		ID:         dev.Kname,
		Name:       name,
		MountPoint: dev.Mountpoint,
		FileSystem: dev.Fstype,
		RaidLevel:  strings.ToUpper(dev.Type),
		RaidState:  "Active",
		TotalSize:  total,
		UsedSize:   used,
		Disks:      []string{dev.Kname},
	})
}
