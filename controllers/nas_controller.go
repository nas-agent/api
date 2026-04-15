package controllers

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	mountedPaths := make(map[string]bool) // Track already-added mount points

	for _, dev := range lsblk.Blockdevices {
		// Process each block device (disk, lvm, raid)
		if dev.Type == "disk" {
			// A disk might have partitions. We need to check if ANY of its children are mounted.
			hasMountedChild := false
			processDeviceTree(dev, &mounted, &unmounted, &hasMountedChild, mountedPaths)

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
				addMountedVolume(dev, &mounted, mountedPaths)
			}
		}
	}

	// Additionally, read /proc/mounts to capture all mounted filesystems
	// This catches bind mounts, manually mounted directories, and other mounts not shown by lsblk
	if file, err := os.Open("/proc/mounts"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) >= 6 {
				device := parts[0]
				mountPoint := parts[1]
				fstype := parts[2]

				// Skip system mounts and already-processed mounts
				if shouldSkipMount(mountPoint) || mountedPaths[mountPoint] {
					continue
				}

				// Skip if mountpoint doesn't exist
				if _, err := os.Stat(mountPoint); err != nil {
					continue
				}

				// Create a logical volume entry for this mount
				usage, _ := disk.Usage(mountPoint)
				name := filepath.Base(mountPoint)
				if name == "" || name == "/" {
					name = mountPoint
				}

				mounted = append(mounted, LogicalVolume{
					ID:         strings.TrimPrefix(device, "/dev/"),
					Name:       name,
					MountPoint: mountPoint,
					FileSystem: fstype,
					RaidLevel:  "Single",
					RaidState:  "Active",
					TotalSize:  strconv.FormatUint(usage.Total, 10),
					UsedSize:   strconv.FormatUint(usage.Used, 10),
					Disks:      []string{strings.TrimPrefix(device, "/dev/")},
				})
				mountedPaths[mountPoint] = true
			}
		}
	}

	return c.JSON(fiber.Map{
		"mounted":   mounted,
		"unmounted": unmounted,
	})
}

// shouldSkipMount checks if a mount point should be skipped
func shouldSkipMount(mountPoint string) bool {
	skipPaths := []string{
		"/",
		"/boot",
		"/dev",
		"/proc",
		"/sys",
		"/run",
		"/tmp",
		"/var",
		"/usr",
		"/etc",
		"/lib",
		"/bin",
		"/sbin",
		"/opt",
		"/snap",
	}

	for _, skip := range skipPaths {
		if mountPoint == skip || strings.HasPrefix(mountPoint, skip+"/") {
			return true
		}
	}
	return false
}

// processDeviceTree recursively looks for mount points in partitions or logical volumes
func processDeviceTree(dev LsblkDevice, mounted *[]LogicalVolume, unmounted *[]PhysicalDisk, hasMountedChild *bool, mountedPaths map[string]bool) {
	if dev.Mountpoint != "" {
		*hasMountedChild = true
		addMountedVolume(dev, mounted, mountedPaths)
	}

	for _, child := range dev.Children {
		processDeviceTree(child, mounted, unmounted, hasMountedChild, mountedPaths)
	}
}

func addMountedVolume(dev LsblkDevice, mounted *[]LogicalVolume, mountedPaths map[string]bool) {
	// Skip system-critical paths that shouldn't be managed/shared easily
	if dev.Mountpoint == "/" || dev.Mountpoint == "/boot" || strings.HasPrefix(dev.Mountpoint, "/boot/") {
		return
	}

	// Skip if already added
	if mountedPaths[dev.Mountpoint] {
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

	mountedPaths[dev.Mountpoint] = true
}
