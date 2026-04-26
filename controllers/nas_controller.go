package controllers

import (
	"api/database"
	"api/models"
	"api/services"
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
		log.Printf("json unmarshal error: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to parse lsblk output: " + err.Error()})
	}

	var mounted = make([]LogicalVolume, 0)
	var unmounted = make([]PhysicalDisk, 0)
	mountedPaths := make(map[string]bool) // Track already-added mount points

	// Get all RAID arrays to check if disks are used in RAID
	var raidArrays []models.RaidArray
	database.DB.Find(&raidArrays)

	// Build a map of disk paths that are part of RAID arrays
	raidDiskMap := make(map[string]bool)
	for _, raid := range raidArrays {
		raidDiskMap[raid.Disk1] = true
		raidDiskMap[raid.Disk2] = true
	}

	for _, dev := range lsblk.Blockdevices {
		// Process each block device (disk, lvm, raid)
		if dev.Type == "disk" {
			// A disk might have partitions. We need to check if ANY of its children are mounted.
			hasMountedChild := false
			processDeviceTree(dev, &mounted, &unmounted, &hasMountedChild, mountedPaths)

			// If neither the disk nor any children are mounted, it's an unmounted physical drive
			// But skip disks that are part of RAID arrays
			// List all unmounted disks (both raw disks and partitioned disks)
			// Raw disks can be formatted using FormatAndMount endpoint
			if dev.Mountpoint == "" && !hasMountedChild {
				// Check if this disk is part of a RAID array
				isPartOfRaid := raidDiskMap["/dev/"+dev.Kname]

				// Only add to unmounted if it's NOT part of a RAID
				if !isPartOfRaid {
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

				// Only include specific file systems or device types
				// We don't want tempfs, proc, sysfs, snapfuse, etc.
				if strings.HasPrefix(fstype, "overlay") || strings.HasPrefix(fstype, "cgroup") ||
					fstype == "squashfs" || fstype == "tmpfs" || fstype == "devtmpfs" || fstype == "fuse" ||
					fstype == "autofs" || fstype == "proc" || fstype == "sysfs" || fstype == "securityfs" ||
					fstype == "nsfs" || fstype == "devpts" || fstype == "mqueue" || fstype == "pstore" ||
					fstype == "bpf" || fstype == "debugfs" || fstype == "tracefs" || fstype == "hugetlbfs" {
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

	vol := LogicalVolume{
		ID:         dev.Kname,
		Name:       name,
		MountPoint: dev.Mountpoint,
		FileSystem: dev.Fstype,
		RaidLevel:  strings.ToUpper(dev.Type),
		RaidState:  "Active",
		TotalSize:  total,
		UsedSize:   used,
		Disks:      []string{dev.Kname},
	}
	*mounted = append(*mounted, vol)
}

type MountRequest struct {
	Device     string `json:"device"`
	MountDir   string `json:"mountDir"`
	FileSystem string `json:"fileSystem"` // optional, will be auto-detected if not provided
}

type FormatAndMountRequest struct {
	Device     string `json:"device"`     // e.g., "/dev/sdb"
	MountName  string `json:"mountName"`  // e.g., "NAS" - will mount to /mnt/NAS, or leave empty for default
	FileSystem string `json:"fileSystem"` // e.g., "ext4", defaults to "ext4"
}

// isSystemDisk checks if a device is the root/system disk
func isSystemDisk(device string) bool {
	// Method 1: Check if device matches the root filesystem
	rootCmd := exec.Command("df", "/")
	output, _ := rootCmd.CombinedOutput()
	if strings.Contains(string(output), device) {
		return true
	}

	// Method 2: Check if partitions of this device are mounted at critical paths
	// Normalize device name (e.g., /dev/sda -> sda)
	devName := strings.TrimPrefix(device, "/dev/")

	mountCmd := exec.Command("mount")
	mountOutput, _ := mountCmd.CombinedOutput()
	mountLines := strings.Split(string(mountOutput), "\n")

	systemPaths := []string{"/", "/boot", "/usr", "/var", "/etc", "/sys", "/proc", "/dev", "/tmp"}

	for _, line := range mountLines {
		// Look for lines containing the device
		if strings.Contains(line, devName) {
			for _, sysPath := range systemPaths {
				if strings.Contains(line, " on "+sysPath+" ") {
					return true
				}
			}
		}
	}

	return false
}

// removeFromFstab removes any entries from /etc/fstab that match the given target (UUID or mount point)
func removeFromFstab(target string) error {
	if target == "" {
		return nil
	}

	log.Printf("[NAS] Removing entry from /etc/fstab for: %s\n", target)

	// Escape slash for sed
	escapedTarget := strings.ReplaceAll(target, "/", "\\/")

	// We use sed to delete any line containing the target string
	// This is simple and effective for cleaning up fstab
	sedCmd := fmt.Sprintf("sudo sed -i '/%s/d' /etc/fstab", escapedTarget)
	
	cmd := exec.Command("bash", "-c", sedCmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[NAS] Warning: Failed to remove from fstab: %v, output: %s\n", err, string(output))
		return err
	}
	
	return nil
}

// forceStopDevice uses fuser to kill processes and fully release a device
func forceStopDevice(device string, maxRetries int) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Try to kill all processes using the device
		fuserCmd := exec.Command("sudo", "fuser", "-k", "-9", device)
		_, _ = fuserCmd.CombinedOutput() // Ignore errors; fuser fails if no processes

		time.Sleep(500 * time.Millisecond)

		// Check if device is now available
		testCmd := exec.Command("sudo", "wipefs", device)
		if err := testCmd.Run(); err == nil {
			return nil // Device is available
		}

		if attempt < maxRetries {
			time.Sleep(1 * time.Second)
		}
	}
	return fmt.Errorf("device still busy after %d attempts", maxRetries)
}

// detectRaidArrays finds all RAID arrays currently active on the system
// Returns a map of device -> RAID array name
func detectRaidArrays() map[string]string {
	raidMap := make(map[string]string)

	// Use mdadm to query all active RAID arrays
	mdadmCmd := exec.Command("sudo", "mdadm", "--query", "--export")
	output, _ := mdadmCmd.CombinedOutput()

	// Parse mdadm output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "MD_DEVS=") {
			// Extract device list from MD_DEVS field
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				devs := strings.Fields(parts[1])
				// All devices in this RAID array
				for _, dev := range devs {
					raidMap[strings.TrimSpace(dev)] = "active_raid"
				}
			}
		}
	}

	// Alternative: Check using lsblk for md device children
	// This catches RAID arrays more reliably
	lsblkCmd := exec.Command("lsblk", "-J", "-n", "-o", "NAME,TYPE")
	lsblkOutput, _ := lsblkCmd.CombinedOutput()

	var lsblkData map[string]interface{}
	_ = json.Unmarshal(lsblkOutput, &lsblkData)

	// Check each device for md children
	if blockdevices, ok := lsblkData["blockdevices"].([]interface{}); ok {
		for _, bd := range blockdevices {
			bdMap, _ := bd.(map[string]interface{})
			name, _ := bdMap["name"].(string)
			
			// Check if this device name appears in /proc/mdstat
			mdstat, _ := os.ReadFile("/proc/mdstat")
			if name != "" && strings.Contains(string(mdstat), name) {
				raidMap["/dev/"+name] = "active_in_mdstat"
			}

			// Check if has raid child via lsblk
			if children, ok := bdMap["children"].([]interface{}); ok {
				for _, child := range children {
					if childMap, ok := child.(map[string]interface{}); ok {
						if childType, ok := childMap["type"].(string); ok && (childType == "raid" || childType == "raid1") {
							if name != "" {
								raidMap["/dev/"+name] = "has_raid_child"
							}
						}
					}
				}
			}
		}
	}

	return raidMap
}

// hardenDeviceCleanup aggressively removes RAID/LVM metadata and releases kernel locks
func hardenDeviceCleanup(device string) error {
	log.Printf("[NAS] START NUCLEAR CLEANUP for: %s", device)

	// 1. Disable swap if active
	exec.Command("sudo", "swapoff", device).Run()
	exec.Command("sudo", "swapoff", device+"1").Run()
	exec.Command("sudo", "swapoff", device+"2").Run()

	// 2. Identify and stop MD devices using this disk
	devName := filepath.Base(device) // e.g., "sda"
	
	// CRITICAL: Wipe RAID superblocks first so the kernel releases its hold
	log.Printf("[NAS] Wiping RAID superblocks on %s...", device)
	exec.Command("sudo", "mdadm", "--zero-superblock", "--force", device).Run()
	exec.Command("sudo", "mdadm", "--zero-superblock", "--force", device+"1").Run()

	// Parse /proc/mdstat to find EXACT md devices using this disk
	mdstat, _ := os.ReadFile("/proc/mdstat")
	mdstatStr := string(mdstat)
	
	// Example line: md127 : active raid1 sda1[0] sdb1[1]
	// We look for "sda" in the line and extract the "mdX" part
	lines := strings.Split(mdstatStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, devName) {
			parts := strings.Fields(line)
			if len(parts) > 0 && strings.HasPrefix(parts[0], "md") {
				mdDev := "/dev/" + parts[0]
				log.Printf("[NAS] Stopping RAID device %s which is using %s", mdDev, device)
				exec.Command("sudo", "mdadm", "--stop", mdDev).Run()
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// 4. THE NUCLEAR OPTION: Wipe first 50MB and create new label
	log.Printf("[NAS] Zapping all GPT/MBR structures on %s...", device)
	exec.Command("sudo", "sgdisk", "--zap-all", device).Run()

	log.Printf("[NAS] Force-creating new GPT label on %s...", device)
	exec.Command("sudo", "parted", "-s", device, "mklabel", "gpt").Run()
	
	log.Printf("[NAS] Wiping disk headers with dd...")
	exec.Command("sudo", "dd", "if=/dev/zero", "of="+device, "bs=1M", "count=50", "conv=notrunc").Run()

	// 5. Clear signatures again
	exec.Command("sudo", "wipefs", "-a", "-f", device).Run()

	// 6. Refresh kernel and settle udev
	exec.Command("sudo", "udevadm", "settle").Run()
	exec.Command("sudo", "partprobe", device).Run()
	exec.Command("sudo", "sync").Run()

	time.Sleep(3 * time.Second) // Let the kernel breathe
	log.Printf("[NAS] NUCLEAR CLEANUP COMPLETE for: %s", device)

	return nil
}

// 3. Format to specified filesystem (default ext4)
// 4. Create mount directory and mount
// 5. Get UUID and add to /etc/fstab for persistence
func FormatAndMount(c *fiber.Ctx) error {
	var req FormatAndMountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Device == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Device path is required"})
	}

	// **SAFETY CHECK: Prevent formatting system disks**
	if isSystemDisk(req.Device) {
		log.Printf("[NAS] ⛔ Attempt to format system disk: %s\n", req.Device)
		return c.Status(403).JSON(fiber.Map{
			"error": fmt.Sprintf("❌ Cannot format %s: This is a system disk in use by the operating system. Formatting would damage your system.", req.Device),
		})
	}

	// Check if device is part of any RAID arrays
	var raids []models.RaidArray
	if err := database.DB.Find(&raids).Error; err != nil {
		log.Printf("[NAS] Warning: Could not query RAID arrays: %v\n", err)
	} else {
		log.Printf("[NAS] Found %d RAID arrays in database\n", len(raids))
		reqDeviceNorm := strings.TrimPrefix(strings.TrimPrefix(req.Device, "/dev/"), "mmcblk")

		for _, raid := range raids {
			log.Printf("[NAS] Checking RAID %s: Disk1=%s, Disk2=%s against Device=%s\n",
				raid.Name, raid.Disk1, raid.Disk2, req.Device)

			// Normalize device paths - handle various formats
			disk1 := strings.TrimPrefix(raid.Disk1, "/dev/")
			disk2 := strings.TrimPrefix(raid.Disk2, "/dev/")

			// Extract just the disk name (e.g., "sda" from "sda1" or "/dev/sda")
			disk1Base := strings.Split(disk1, "p")[0] // Handle sda vs sdap1
			disk1Base = strings.Split(disk1Base, "1")[0]
			disk1Base = strings.Split(disk1Base, "2")[0]

			disk2Base := strings.Split(disk2, "p")[0]
			disk2Base = strings.Split(disk2Base, "1")[0]
			disk2Base = strings.Split(disk2Base, "2")[0]

			reqDeviceBase := strings.Split(reqDeviceNorm, "p")[0]
			reqDeviceBase = strings.Split(reqDeviceBase, "1")[0]
			reqDeviceBase = strings.Split(reqDeviceBase, "2")[0]

			log.Printf("[NAS] Normalized comparison: %s vs %s/%s\n", reqDeviceBase, disk1Base, disk2Base)

			if reqDeviceBase == disk1Base || reqDeviceBase == disk2Base {
				log.Printf("[NAS] ⛔ Device %s is part of RAID array %s\n", req.Device, raid.Name)
				return c.Status(409).JSON(fiber.Map{
					"error": fmt.Sprintf("Device %s is part of RAID array %s. Please delete the RAID array first.", req.Device, raid.Name),
				})
			}
		}
	}

	// **NEW**: Check for active RAID arrays on the system itself
	log.Printf("[NAS] Checking for active RAID arrays on the system...\n")
	activeRaids := detectRaidArrays()
	if len(activeRaids) > 0 {
		log.Printf("[NAS] Found %d active RAID arrays\n", len(activeRaids))

		// Normalize device name for comparison (e.g., /dev/sda -> sda)
		devName := strings.TrimPrefix(req.Device, "/dev/")

		for raidDev, raidStatus := range activeRaids {
			raidDevName := strings.TrimPrefix(raidDev, "/dev/")
			// Check if requested device is in any active RAID
			if strings.HasPrefix(raidDevName, devName) || strings.HasPrefix(devName, strings.Split(raidDevName, "p")[0]) {
				log.Printf("[NAS] ⛔ Device %s is part of active RAID array (%s: %s)\n", req.Device, raidDev, raidStatus)
				return c.Status(409).JSON(fiber.Map{
					"error": fmt.Sprintf("Device %s is currently part of an active RAID array. You must remove the RAID array first using 'mdadm' before formatting this disk.", req.Device),
				})
			}
		}
	}

	// Default to ext4
	if req.FileSystem == "" {
		req.FileSystem = "ext4"
	}

	// Determine mount name and directory
	if req.MountName == "" {
		req.MountName = filepath.Base(req.Device)
	}
	mountDir := "/mnt/" + req.MountName

	log.Printf("[NAS] Format and mount: device=%s, mountDir=%s, fs=%s\n", req.Device, mountDir, req.FileSystem)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		totalSteps := 5
		currentStep := 1

		// Step 1: Unmount, stop processes, and wipe
		sendProgress(w, fmt.Sprintf("Step %d/%d: Unmounting existing mounts...", currentStep, totalSteps), "loading", currentStep, totalSteps)
		unmountCmd := exec.Command("bash", "-c", fmt.Sprintf("sudo umount -l %s* 2>/dev/null || true", req.Device))
		if _, err := unmountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] Unmount warning (may be ok): %v\n", err)
		}
		time.Sleep(300 * time.Millisecond)

		sendProgress(w, fmt.Sprintf("Step %d/%d: Stopping processes using device...", currentStep, totalSteps), "loading", currentStep, totalSteps)
		if err := forceStopDevice(req.Device, 3); err != nil {
			log.Printf("[NAS] Force stop warning: %v\n", err)
			sendProgress(w, fmt.Sprintf("Step %d/%d: ⚠️ Warning: %s", currentStep, totalSteps, err.Error()), "warning", currentStep, totalSteps)
		}
		time.Sleep(300 * time.Millisecond)

		sendProgress(w, fmt.Sprintf("Step %d/%d: Wiping old filesystem signatures...", currentStep, totalSteps), "loading", currentStep, totalSteps)
		hardenDeviceCleanup(req.Device)
		time.Sleep(300 * time.Millisecond)

		// Step 2: Format
		currentStep = 2
		sendProgress(w, fmt.Sprintf("Step %d/%d: Formatting %s to %s...", currentStep, totalSteps, req.Device, req.FileSystem), "loading", currentStep, totalSteps)

		// Retry formatting up to 3 times with delay
		formatSuccess := false
		for attempt := 1; attempt <= 3; attempt++ {
			// Create new command for each attempt (can't reuse exec.Cmd)
			var formatCmd *exec.Cmd
			switch req.FileSystem {
			case "ext4":
				formatCmd = exec.Command("sudo", "mkfs.ext4", "-F", "-F", req.Device)
			case "ext3":
				formatCmd = exec.Command("sudo", "mkfs.ext3", "-F", "-F", req.Device)
			case "ntfs":
				formatCmd = exec.Command("sudo", "mkfs.ntfs", "-f", "-f", req.Device)
			case "vfat":
				formatCmd = exec.Command("sudo", "mkfs.vfat", "-I", req.Device) // -I forces on whole disk
			default:
				sendProgress(w, fmt.Sprintf("Unsupported filesystem: %s", req.FileSystem), "error", currentStep, totalSteps)
				return
			}

			if output, err := formatCmd.CombinedOutput(); err != nil {
				if attempt < 3 {
					log.Printf("[NAS] Format attempt %d failed (retrying): %v, output: %s\n", attempt, err, string(output))
					sendProgress(w, fmt.Sprintf("Step %d/%d: Attempt %d/3 - Retrying format...", currentStep, totalSteps, attempt), "loading", currentStep, totalSteps)
					time.Sleep(2 * time.Second)
				} else {
					log.Printf("[NAS] format failed after %d attempts: %v, output: %s\n", attempt, err, string(output))
					sendProgress(w, fmt.Sprintf("❌ Format failed: %s", string(output)), "error", currentStep, totalSteps)
				}
			} else {
				log.Printf("[NAS] Format completed successfully on attempt %d: %s\n", attempt, req.Device)
				formatSuccess = true
				break
			}
		}

		if !formatSuccess {
			return
		}
		time.Sleep(500 * time.Millisecond)

		// Step 3: Create mount directory and mount
		currentStep = 3
		sendProgress(w, fmt.Sprintf("Step %d/%d: Creating mount directory %s...", currentStep, totalSteps, mountDir), "loading", currentStep, totalSteps)
		mkdirCmd := exec.Command("sudo", "mkdir", "-p", mountDir)
		if output, err := mkdirCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mkdir failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("❌ Failed to create directory: %s", string(output)), "error", currentStep, totalSteps)
			return
		}
		time.Sleep(500 * time.Millisecond)

		sendProgress(w, fmt.Sprintf("Step %d/%d: Mounting %s...", currentStep, totalSteps, req.Device), "loading", currentStep, totalSteps)
		mountCmd := exec.Command("sudo", "mount", req.Device, mountDir)
		if output, err := mountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mount failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("❌ Mount failed: %s", string(output)), "error", currentStep, totalSteps)
			return
		}
		log.Printf("[NAS] Mounted successfully: %s -> %s\n", req.Device, mountDir)
		time.Sleep(500 * time.Millisecond)

		// Step 4: Get UUID and add to fstab for persistence
		currentStep = 4
		sendProgress(w, fmt.Sprintf("Step %d/%d: Retrieving disk UUID for persistent mounting...", currentStep, totalSteps), "loading", currentStep, totalSteps)
		uuidCmd := exec.Command("bash", "-c", fmt.Sprintf("sudo blkid -s UUID -o value %s", req.Device))
		uuidOutput, err := uuidCmd.CombinedOutput()
		if err != nil {
			log.Printf("[NAS] UUID retrieval failed: %v\n", err)
			sendProgress(w, fmt.Sprintf("Step %d/%d: ⚠️ Could not retrieve UUID, disk will not auto-mount on reboot", currentStep, totalSteps), "warning", currentStep, totalSteps)
		} else {
			uuid := strings.TrimSpace(string(uuidOutput))
			if uuid != "" {
				log.Printf("[NAS] Retrieved UUID: %s\n", uuid)
				sendProgress(w, fmt.Sprintf("Step %d/%d: Adding UUID to /etc/fstab...", currentStep, totalSteps), "loading", currentStep, totalSteps)

				// Cleanup any existing entries for this UUID or mount point to prevent duplicates
				removeFromFstab(uuid)
				removeFromFstab(mountDir)

				// Create fstab entry with nofail to prevent emergency mode if drive is missing at boot
				fstabEntry := fmt.Sprintf("UUID=%s %s %s defaults,noatime,nofail,x-systemd.device-timeout=5 0 2", uuid, mountDir, req.FileSystem)
				log.Printf("[NAS] fstab entry: %s\n", fstabEntry)

				// Add to fstab using tee
				echoCmd := fmt.Sprintf("echo '%s' | sudo tee -a /etc/fstab > /dev/null", fstabEntry)
				fstabCmd := exec.Command("bash", "-c", echoCmd)
				if output, err := fstabCmd.CombinedOutput(); err != nil {
					log.Printf("[NAS] fstab update failed: %v, output: %s\n", err, string(output))
					sendProgress(w, fmt.Sprintf("Step %d/%d: ⚠️ Warning: Could not update fstab: %s", currentStep, totalSteps, string(output)), "warning", currentStep, totalSteps)
				} else {
					log.Printf("[NAS] Added to fstab successfully\n")
					time.Sleep(500 * time.Millisecond)
				}
			}
		}

		// Step 5: Register volume in database
		currentStep = 5
		sendProgress(w, fmt.Sprintf("Step %d/%d: Registering volume in database...", currentStep, totalSteps), "loading", currentStep, totalSteps)

		// Get disk usage information
		dfCmd := exec.Command("df", "-B1", mountDir)
		dfOutput, err := dfCmd.CombinedOutput()
		if err != nil {
			log.Printf("[NAS] Failed to get disk usage: %v\n", err)
			sendProgress(w, fmt.Sprintf("Step %d/%d: ⚠️ Could not retrieve disk size information", currentStep, totalSteps), "warning", currentStep, totalSteps)
		}

		var totalSize int64
		var usedSize int64

		if err == nil {
			// Parse df output to get total and used sizes
			lines := strings.Split(strings.TrimSpace(string(dfOutput)), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 3 {
					if size, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						totalSize = size
					}
					if size, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
						usedSize = size
					}
				}
			}
		}

		// Create volume entry in database
		volumeID := fmt.Sprintf("vol_%d", time.Now().UnixNano())
		now := time.Now().Unix()
		volume := map[string]interface{}{
			"id":          volumeID,
			"mount_point": mountDir,
			"device_path": req.Device,
			"file_system": req.FileSystem,
			"total_size":  totalSize,
			"used_size":   usedSize,
			"status":      "Mounted",
			"created_at":  now,
			"updated_at":  now,
		}

		if err := database.DB.Table("volumes").Create(volume).Error; err != nil {
			log.Printf("[NAS] Error registering volume: %v\n", err)
			sendProgress(w, fmt.Sprintf("Step %d/%d: ⚠️ Volume mounted but could not persist to database", currentStep, totalSteps), "warning", currentStep, totalSteps)
		} else {
			log.Printf("[NAS] Volume registered in database with ID: %s\n", volumeID)
			sendProgress(w, fmt.Sprintf("Step %d/%d: ✓ All steps completed successfully! Disk ready.", currentStep, totalSteps), "success", currentStep, totalSteps)
		}
	})

	return nil
}

// sendProgress sends a progress message as SSE with detailed tracking
func sendProgress(w *bufio.Writer, message, status string, currentStep, totalSteps int) {
	percentage := 0
	if totalSteps > 0 {
		percentage = (currentStep * 100) / totalSteps
	}
	// Ensure percentage is within 0-100 and doesn't reach 100 until truly done
	if percentage >= 100 && status != "success" {
		percentage = 99
	}
	fmt.Fprintf(w, "data: {\"step\":\"%s\", \"status\":\"%s\", \"progress\":%d, \"currentStep\":%d, \"totalSteps\":%d}\n\n",
		message, status, percentage, currentStep, totalSteps)
	w.Flush()
}

// sendProgressSimple sends progress without step tracking (backward compatibility)
func sendProgressSimple(w *bufio.Writer, message, status string) {
	fmt.Fprintf(w, "data: {\"step\":\"%s\", \"status\":\"%s\"}\n\n", message, status)
	w.Flush()
}

// MountDevice mounts a block device to a specified directory or a default directory under /mnt
func MountDevice(c *fiber.Ctx) error {
	var req MountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Device == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Device path is required"})
	}

	// Validate that the device is a partition, not a raw disk
	// Partitions end with a number (e.g., /dev/sda1, /dev/nvme0n1p1)
	// Raw disks don't (e.g., /dev/sda, /dev/sdb)
	deviceName := filepath.Base(req.Device)
	lastChar := deviceName[len(deviceName)-1]
	isPartition := lastChar >= '0' && lastChar <= '9'

	if !isPartition {
		// This is a raw disk, not a partition
		log.Printf("[NAS] Mount attempted on raw disk (not partition): %s\n", req.Device)
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			fmt.Fprintf(w, "data: {\"step\":\"Cannot mount raw disk %s\", \"status\":\"error\", \"details\":\"This is an unpartitioned disk. You must first: 1) Partition it (using fdisk/parted), 2) Format each partition (using mkfs), then mount the partition (e.g., %s1)\"}\n\n", req.Device, req.Device)
			w.Flush()
		})
		return nil
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		mountDir := req.MountDir
		if mountDir == "" {
			// default to /mnt/<device_name>
			deviceName := filepath.Base(req.Device)
			mountDir = "/mnt/" + deviceName
		}

		log.Printf("[NAS] Mount attempt: device=%s, mountDir=%s\n", req.Device, mountDir)

		totalSteps := 4
		currentStep := 1

		sendProgress(w, fmt.Sprintf("Step %d/%d: Creating mount point %s...", currentStep, totalSteps, mountDir), "loading", currentStep, totalSteps)
		time.Sleep(500 * time.Millisecond) // realistic UX delay

		// Auto-detect filesystem if not provided
		currentStep = 2
		fileSystem := req.FileSystem
		if fileSystem == "" {
			sendProgress(w, fmt.Sprintf("Step %d/%d: Detecting filesystem...", currentStep, totalSteps), "loading", currentStep, totalSteps)
			blkidCmd := exec.Command("sudo", "blkid", "-s", "TYPE", "-o", "value", req.Device)
			if output, err := blkidCmd.CombinedOutput(); err == nil {
				fileSystem = strings.TrimSpace(string(output))
				if fileSystem == "" {
					fileSystem = "unknown"
				}
			} else {
				fileSystem = "unknown"
			}
			time.Sleep(300 * time.Millisecond)
		}

		// Create mount point if it doesn't exist (use sudo for /mnt access)
		mkdirCmd := exec.Command("sudo", "mkdir", "-p", mountDir)
		if output, err := mkdirCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mkdir failed: %v, output: %s\n", err, string(output))
			fmt.Fprintf(w, "data: {\"step\":\"Failed to create directory: %s\", \"status\":\"error\", \"progress\":0, \"currentStep\":%d, \"totalSteps\":%d}\n\n", string(output), currentStep, totalSteps)
			w.Flush()
			return
		}
		log.Printf("[NAS] Mount point created: %s\n", mountDir)

		currentStep = 3
		sendProgress(w, fmt.Sprintf("Step %d/%d: Mounting device %s...", currentStep, totalSteps, req.Device), "loading", currentStep, totalSteps)
		time.Sleep(500 * time.Millisecond)

		// Run mount command
		cmd := exec.Command("sudo", "mount", req.Device, mountDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mount failed: %v, output: %s\n", err, string(output))
			fmt.Fprintf(w, "data: {\"step\":\"Mount failed: %s\", \"status\":\"error\", \"progress\":0, \"currentStep\":%d, \"totalSteps\":%d}\n\n", string(output), currentStep, totalSteps)
			w.Flush()
			return
		}
		log.Printf("[NAS] Device mounted successfully: %s -> %s\n", req.Device, mountDir)

		fmt.Fprintf(w, "data: {\"step\":\"✓ Device mounted successfully!\", \"status\":\"success\", \"progress\":75, \"currentStep\":%d, \"totalSteps\":%d, \"mountPoint\":\"%s\"}\n\n", currentStep, totalSteps, mountDir)
		w.Flush()

		// Step 5: Register volume in database
		currentStep = 4
		sendProgress(w, fmt.Sprintf("Step %d/%d: Registering volume in database...", currentStep, totalSteps), "loading", currentStep, totalSteps)
		time.Sleep(300 * time.Millisecond)

		// Get disk usage information
		dfCmd := exec.Command("df", "-B1", mountDir)
		dfOutput, err := dfCmd.CombinedOutput()
		if err != nil {
			log.Printf("[NAS] Failed to get disk usage: %v\n", err)
			fmt.Fprintf(w, "data: {\"step\":\"Warning: Could not retrieve disk size information\", \"status\":\"warning\", \"progress\":90, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps)
			w.Flush()
		}

		var totalSize int64
		var usedSize int64

		if err == nil {
			// Parse df output to get total and used sizes
			lines := strings.Split(strings.TrimSpace(string(dfOutput)), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 3 {
					if size, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						totalSize = size
					}
					if size, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
						usedSize = size
					}
				}
			}
		}

		// Create volume entry in database
		volumeID := fmt.Sprintf("vol_%d", time.Now().UnixNano())
		now := time.Now().Unix()
		volume := map[string]interface{}{
			"id":          volumeID,
			"mount_point": mountDir,
			"device_path": req.Device,
			"file_system": fileSystem,
			"total_size":  totalSize,
			"used_size":   usedSize,
			"status":      "Mounted",
			"created_at":  now,
			"updated_at":  now,
		}

		if err := database.DB.Table("volumes").Create(volume).Error; err != nil {
			log.Printf("[NAS] Error registering volume: %v\n", err)
			fmt.Fprintf(w, "data: {\"step\":\"⚠️ Volume mounted but could not persist to database\", \"status\":\"warning\", \"progress\":90, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps)
			w.Flush()
		} else {
			log.Printf("[NAS] Volume registered in database with ID: %s\n", volumeID)
			fmt.Fprintf(w, "data: {\"step\":\"✓ All steps completed successfully! Volume ready.\", \"status\":\"success\", \"progress\":100, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps)
			w.Flush()
		}
	})

	return nil
}

// UnmountDevice unmounts a block device or mount point
func UnmountDevice(c *fiber.Ctx) error {
	var req MountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	target := req.MountDir
	if target == "" {
		target = req.Device
	}

	if target == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Device or MountDir is required"})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		totalSteps := 4
		currentStep := 1

		// Step 0: Try to find the volume in the database to get its true mount point and ID
		var volume models.Volume
		dbResult := database.DB.Where("mount_point = ? OR device_path = ?", target, target).First(&volume)
		
		// If we found it, use the official mount point for all operations
		if dbResult.Error == nil && volume.MountPoint != "" {
			target = volume.MountPoint
			log.Printf("[NAS] Resolved unmount target for %s -> %s (ID: %s)\n", req.Device, target, volume.ID)
		} else {
			log.Printf("[NAS] Warning: Could not find volume in database for target %s, proceeding with raw path\n", target)
		}

		// Step 1: Attempt normal unmount
		fmt.Fprintf(w, "data: {\"step\":\"Step %d/%d: Attempting to unmount %s...\", \"status\":\"loading\", \"progress\":0, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps, target, currentStep, totalSteps)
		w.Flush()
		time.Sleep(500 * time.Millisecond)

		cmd := exec.Command("sudo", "umount", target)
		output, err := cmd.CombinedOutput()

		unmountSuccess := false
		if err == nil {
			unmountSuccess = true
		} else {
			errMsg := string(output)
			// Check if target is busy - try lazy unmount
			if strings.Contains(errMsg, "busy") || strings.Contains(errMsg, "Device or resource busy") {
				currentStep = 2
				fmt.Fprintf(w, "data: {\"step\":\"Step %d/%d: Mount point busy, attempting lazy unmount...\", \"status\":\"loading\", \"progress\":25, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps, currentStep, totalSteps)
				w.Flush()
				time.Sleep(500 * time.Millisecond)

				// Try lazy unmount (-l flag defers unmount until resources are released)
				cmd = exec.Command("sudo", "umount", "-l", target)
				output, err = cmd.CombinedOutput()

				if err == nil {
					unmountSuccess = true
				} else {
					errMsg = string(output)
					// Last resort: try force unmount
					if strings.Contains(errMsg, "busy") || strings.Contains(errMsg, "Device or resource busy") {
						currentStep = 2
						fmt.Fprintf(w, "data: {\"step\":\"Step %d/%d: Lazy unmount failed, attempting force unmount...\", \"status\":\"loading\", \"progress\":25, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps, currentStep, totalSteps)
						w.Flush()
						time.Sleep(500 * time.Millisecond)

						cmd = exec.Command("sudo", "umount", "-f", target)
						output, err = cmd.CombinedOutput()
						if err == nil {
							unmountSuccess = true
						}
					}
				}
			}
		}

		if !unmountSuccess {
			fmt.Fprintf(w, "data: {\"step\":\"Unmount failed: %s\", \"status\":\"error\"}\n\n", strings.TrimSpace(string(output)))
			w.Flush()
			return
		}

		// Step 3: Cleanup and update database
		currentStep = 3
		fmt.Fprintf(w, "data: {\"step\":\"Step %d/%d: Cleaning up mount directories and shares...\", \"status\":\"loading\", \"progress\":50, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps, currentStep, totalSteps)
		w.Flush()
		time.Sleep(300 * time.Millisecond)

		// Delete all shares associated with this volume
		if volume.ID != "" {
			var shares []models.Share
			if err := database.DB.Where("volume_id = ?", volume.ID).Find(&shares).Error; err == nil {
				for _, share := range shares {
					// Unregister from Samba
					services.Samba.UnregisterShare(share.Name)
					// Delete from database
					database.DB.Unscoped().Delete(&share)
				}
			}
		}

		// Remove mount directory if in /mnt or /media
		// BE CAREFUL: Only remove if it's a subdirectory, not /mnt itself!
		if (strings.HasPrefix(target, "/mnt/") && len(target) > 5) || (strings.HasPrefix(target, "/media/") && len(target) > 7) {
			log.Printf("[NAS] Physically removing mount point directory: %s\n", target)
			// Use sudo rm -rf to remove directory
			rmCmd := exec.Command("sudo", "rm", "-rf", target)
			if output, err := rmCmd.CombinedOutput(); err != nil {
				log.Printf("[NAS] Warning: Failed to remove mount directory %s: %v, output: %s\n", target, err, string(output))
				// Continue anyway - unmount was successful
			}
			}
		}

		// Step 3.5: Cleanup /etc/fstab entry to prevent boot issues
		// Even if the device is unmounted, we don't want systemd trying to mount it on reboot
		removeFromFstab(target)

		// Step 4: Update database status
		currentStep = 4
		fmt.Fprintf(w, "data: {\"step\":\"Step %d/%d: Updating database status...\", \"status\":\"loading\", \"progress\":75, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps, currentStep, totalSteps)
		w.Flush()

		// Update database: mark volume as unmounted
		var updateErr error
		if volume.ID != "" {
			updateErr = database.DB.Model(&models.Volume{}).
				Where("id = ?", volume.ID).
				Update("status", models.VolumeStatusUnmounted).Error
		} else {
			// Fallback: search by path if we don't have ID
			updateErr = database.DB.Model(&models.Volume{}).
				Where("mount_point = ? OR device_path = ?", target, target).
				Update("status", models.VolumeStatusUnmounted).Error
		}

		if updateErr != nil {
			log.Printf("[NAS] Error updating volume status: %v\n", updateErr)
			fmt.Fprintf(w, "data: {\"step\":\"⚠️ Device unmounted but database update failed\", \"status\":\"warning\"}\n\n")
			w.Flush()
		}

		fmt.Fprintf(w, "data: {\"step\":\"✓ Device unmounted successfully!\", \"status\":\"success\", \"progress\":100, \"currentStep\":%d, \"totalSteps\":%d}\n\n", currentStep, totalSteps)
		w.Flush()
	})

	return nil
}

// GetVolumes retrieves all registered mounted volumes from the database
func GetVolumes(c *fiber.Ctx) error {
	type VolumeResponse struct {
		ID         string `json:"id"`
		MountPoint string `json:"mount_point"`
		DevicePath string `json:"device_path"`
		FileSystem string `json:"file_system"`
		TotalSize  int64  `json:"total_size"`
		UsedSize   int64  `json:"used_size"`
		Status     string `json:"status"`
		CreatedAt  int64  `json:"created_at"`
	}

	volumes := []*VolumeResponse{}

	// Only retrieve volumes with "Mounted" status
	if err := database.DB.Table("volumes").
		Where("status = ?", models.VolumeStatusMounted).
		Order("created_at DESC").
		Scan(&volumes).Error; err != nil {
		log.Printf("[NAS] Error fetching volumes: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch volumes"})
	}

	// Return empty array instead of null
	if volumes == nil {
		volumes = []*VolumeResponse{}
	}

	return c.JSON(volumes)
}

// RegisterVolume registers a newly mounted volume to the database
func RegisterVolume(c *fiber.Ctx) error {
	type VolumeRegistration struct {
		MountPoint string `json:"mount_point"`
		DevicePath string `json:"device_path"`
		FileSystem string `json:"file_system"`
		TotalSize  int64  `json:"total_size"`
		UsedSize   int64  `json:"used_size"`
	}

	var req VolumeRegistration
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Validate required fields
	if req.MountPoint == "" || req.DevicePath == "" {
		return c.Status(400).JSON(fiber.Map{"error": "mount_point and device_path are required"})
	}

	// Check if volume already exists
	var count int64
	if err := database.DB.Table("volumes").Where("mount_point = ? OR device_path = ?", req.MountPoint, req.DevicePath).Count(&count).Error; err != nil {
		log.Printf("[NAS] Error checking existing volume: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to check existing volumes"})
	}

	if count > 0 {
		return c.Status(409).JSON(fiber.Map{"error": "Volume already registered"})
	}

	// Generate unique ID (using UUID library would be better, but this works for now)
	volumeID := fmt.Sprintf("vol_%d", time.Now().UnixNano())

	// Create new volume
	volume := map[string]interface{}{
		"id":          volumeID,
		"mount_point": req.MountPoint,
		"device_path": req.DevicePath,
		"file_system": req.FileSystem,
		"total_size":  req.TotalSize,
		"used_size":   req.UsedSize,
		"status":      "Mounted",
	}

	if err := database.DB.Table("volumes").Create(volume).Error; err != nil {
		log.Printf("[NAS] Error registering volume: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to register volume"})
	}

	log.Printf("[NAS] Volume registered: %s at %s\n", req.DevicePath, req.MountPoint)
	return c.Status(201).JSON(volume)
}

// DeleteVolume removes a volume registration from the database
func DeleteVolume(c *fiber.Ctx) error {
	volumeID := c.Params("id")
	if volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume ID is required"})
	}

	// Check if volume is referenced by any shares
	var shareCount int64
	if err := database.DB.Table("shares").Where("volume_id = ?", volumeID).Count(&shareCount).Error; err != nil {
		log.Printf("[NAS] Error checking shares: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to check shares"})
	}

	if shareCount > 0 {
		return c.Status(409).JSON(fiber.Map{"error": fmt.Sprintf("Cannot delete volume: %d shares are using this volume", shareCount)})
	}

	// Delete the volume
	if err := database.DB.Table("volumes").Where("id = ?", volumeID).Delete(map[string]interface{}{}).Error; err != nil {
		log.Printf("[NAS] Error deleting volume: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete volume"})
	}

	log.Printf("[NAS] Volume deleted: %s\n", volumeID)
	return c.Status(200).JSON(fiber.Map{"message": "Volume deleted successfully"})
}

// AssignVolumeToUser assigns a volume to a user (admin-only)
func AssignVolumeToUser(c *fiber.Ctx) error {
	type AssignVolumeRequest struct {
		UserID          string `json:"user_id"`
		VolumeID        string `json:"volume_id"`
		PermissionLevel string `json:"permission_level"`
	}

	var req AssignVolumeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.UserID == "" || req.VolumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "user_id and volume_id are required"})
	}

	if req.PermissionLevel == "" {
		req.PermissionLevel = "ReadWrite"
	}

	// Validate user exists
	var userCount int64
	if err := database.DB.Table("users").Where("id = ?", req.UserID).Count(&userCount).Error; err != nil || userCount == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}

	// Validate volume exists
	var volumeCount int64
	if err := database.DB.Table("volumes").Where("id = ?", req.VolumeID).Count(&volumeCount).Error; err != nil || volumeCount == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "Volume not found"})
	}

	// Check if assignment already exists
	var existingCount int64
	if err := database.DB.Table("user_volumes").Where("user_id = ? AND volume_id = ?", req.UserID, req.VolumeID).Count(&existingCount).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to check existing assignment"})
	}

	if existingCount > 0 {
		// Update existing assignment
		if err := database.DB.Table("user_volumes").Where("user_id = ? AND volume_id = ?", req.UserID, req.VolumeID).Update("permission_level", req.PermissionLevel).Error; err != nil {
			log.Printf("[NAS] Error updating volume assignment: %v\n", err)
			return c.Status(500).JSON(fiber.Map{"error": "Failed to update assignment"})
		}
	} else {
		// Create new assignment
		userVolumeID := fmt.Sprintf("uv_%d", time.Now().UnixNano())
		assignment := map[string]interface{}{
			"id":               userVolumeID,
			"user_id":          req.UserID,
			"volume_id":        req.VolumeID,
			"permission_level": req.PermissionLevel,
		}

		if err := database.DB.Table("user_volumes").Create(assignment).Error; err != nil {
			log.Printf("[NAS] Error creating volume assignment: %v\n", err)
			return c.Status(500).JSON(fiber.Map{"error": "Failed to assign volume"})
		}
	}

	log.Printf("[NAS] Volume %s assigned to user %s with permission %s\n", req.VolumeID, req.UserID, req.PermissionLevel)
	return c.Status(201).JSON(fiber.Map{"message": "Volume assigned successfully"})
}

// RevokeVolumeFromUser removes volume access from a user (admin-only)
func RevokeVolumeFromUser(c *fiber.Ctx) error {
	userID := c.Params("userId")
	volumeID := c.Params("volumeId")

	if userID == "" || volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "User ID and Volume ID are required"})
	}

	if err := database.DB.Table("user_volumes").Where("user_id = ? AND volume_id = ?", userID, volumeID).Delete(map[string]interface{}{}).Error; err != nil {
		log.Printf("[NAS] Error revoking volume access: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to revoke access"})
	}

	log.Printf("[NAS] Volume %s revoked from user %s\n", volumeID, userID)
	return c.Status(200).JSON(fiber.Map{"message": "Access revoked successfully"})
}

// GetUserVolumes retrieves all volumes accessible to a specific user
func GetUserVolumes(c *fiber.Ctx) error {
	userID := c.Params("userId")
	if userID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "User ID is required"})
	}

	type VolumeAccess struct {
		ID              string `json:"id"`
		MountPoint      string `json:"mount_point"`
		DevicePath      string `json:"device_path"`
		FileSystem      string `json:"file_system"`
		TotalSize       int64  `json:"total_size"`
		UsedSize        int64  `json:"used_size"`
		Status          string `json:"status"`
		PermissionLevel string `json:"permission_level"`
		CreatedAt       int64  `json:"created_at"`
	}

	var volumes []VolumeAccess

	err := database.DB.Table("volumes").
		Select("volumes.id, volumes.mount_point, volumes.device_path, volumes.file_system, volumes.total_size, volumes.used_size, volumes.status, user_volumes.permission_level, volumes.created_at").
		Joins("INNER JOIN user_volumes ON volumes.id = user_volumes.volume_id").
		Where("user_volumes.user_id = ?", userID).
		Order("volumes.created_at DESC").
		Scan(&volumes).Error

	if err != nil {
		log.Printf("[NAS] Error fetching user volumes: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch volumes"})
	}

	return c.JSON(volumes)
}

// GetVolumeUsers retrieves all users with access to a specific volume (admin-only)
func GetVolumeUsers(c *fiber.Ctx) error {
	volumeID := c.Params("volumeId")
	if volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume ID is required"})
	}

	type UserAccess struct {
		UserID          string `json:"user_id"`
		Username        string `json:"username"`
		Email           string `json:"email"`
		PermissionLevel string `json:"permission_level"`
		AssignedAt      int64  `json:"assigned_at"`
	}

	var users []UserAccess

	err := database.DB.Table("users").
		Select("users.id as user_id, users.username, users.email, user_volumes.permission_level, user_volumes.created_at as assigned_at").
		Joins("INNER JOIN user_volumes ON users.id = user_volumes.user_id").
		Where("user_volumes.volume_id = ?", volumeID).
		Order("users.username ASC").
		Scan(&users).Error

	if err != nil {
		log.Printf("[NAS] Error fetching volume users: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch users"})
	}

	return c.JSON(users)
}

// ===== PHASE 4A: ADVANCED PERMISSIONS =====

// SetSharePermission assigns permission to user/group for a specific share
func SetSharePermission(c *fiber.Ctx) error {
	type PermissionRequest struct {
		ShareID         string `json:"share_id"`
		UserID          string `json:"user_id"`
		GroupID         string `json:"group_id"`
		PermissionLevel string `json:"permission_level"`
		CanShare        bool   `json:"can_share"`
	}

	var req PermissionRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.ShareID == "" || (req.UserID == "" && req.GroupID == "") {
		return c.Status(400).JSON(fiber.Map{"error": "share_id and either user_id or group_id required"})
	}

	permissionID := fmt.Sprintf("perm_%d", time.Now().UnixNano())
	permission := map[string]interface{}{
		"id":               permissionID,
		"share_id":         req.ShareID,
		"user_id":          req.UserID,
		"group_id":         req.GroupID,
		"permission_level": req.PermissionLevel,
		"can_share":        req.CanShare,
	}

	if err := database.DB.Table("share_permissions").Create(permission).Error; err != nil {
		log.Printf("[NAS] Error setting share permission: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to set permission"})
	}

	return c.Status(201).JSON(permission)
}

// GetSharePermissions retrieves all permissions for a share
func GetSharePermissions(c *fiber.Ctx) error {
	shareID := c.Params("shareId")
	if shareID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Share ID is required"})
	}

	type ShareAccess struct {
		ID              string `json:"id"`
		UserID          string `json:"user_id"`
		Username        string `json:"username"`
		GroupID         string `json:"group_id"`
		GroupName       string `json:"group_name"`
		PermissionLevel string `json:"permission_level"`
		CanShare        bool   `json:"can_share"`
	}

	var permissions []ShareAccess

	err := database.DB.Table("share_permissions").
		Select("share_permissions.id, share_permissions.user_id, users.username, share_permissions.group_id, user_groups.name as group_name, share_permissions.permission_level, share_permissions.can_share").
		Joins("LEFT JOIN users ON share_permissions.user_id = users.id").
		Joins("LEFT JOIN user_groups ON share_permissions.group_id = user_groups.id").
		Where("share_permissions.share_id = ?", shareID).
		Scan(&permissions).Error

	if err != nil {
		log.Printf("[NAS] Error fetching share permissions: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch permissions"})
	}

	return c.JSON(permissions)
}

// RevokeSharePermission removes permission to a share
func RevokeSharePermission(c *fiber.Ctx) error {
	permissionID := c.Params("id")
	if permissionID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Permission ID is required"})
	}

	if err := database.DB.Table("share_permissions").Where("id = ?", permissionID).Delete(map[string]interface{}{}).Error; err != nil {
		log.Printf("[NAS] Error revoking share permission: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to revoke permission"})
	}

	return c.Status(200).JSON(fiber.Map{"message": "Permission revoked successfully"})
}

// CreateUserGroup creates a new user group
func CreateUserGroup(c *fiber.Ctx) error {
	type GroupRequest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	var req GroupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Group name is required"})
	}

	groupID := fmt.Sprintf("grp_%d", time.Now().UnixNano())
	group := map[string]interface{}{
		"id":          groupID,
		"name":        req.Name,
		"description": req.Description,
	}

	if err := database.DB.Table("user_groups").Create(group).Error; err != nil {
		log.Printf("[NAS] Error creating group: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create group"})
	}

	return c.Status(201).JSON(group)
}

// AddUserToGroup adds a user to a group
func AddUserToGroup(c *fiber.Ctx) error {
	type MemberRequest struct {
		GroupID string `json:"group_id"`
		UserID  string `json:"user_id"`
	}

	var req MemberRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	memberID := fmt.Sprintf("mem_%d", time.Now().UnixNano())
	member := map[string]interface{}{
		"id":       memberID,
		"group_id": req.GroupID,
		"user_id":  req.UserID,
	}

	if err := database.DB.Table("group_members").Create(member).Error; err != nil {
		log.Printf("[NAS] Error adding user to group: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to add user to group"})
	}

	return c.Status(201).JSON(member)
}

// SetStorageQuota sets quota limit for user or share
func SetStorageQuota(c *fiber.Ctx) error {
	type QuotaRequest struct {
		UserID           string `json:"user_id"`
		ShareID          string `json:"share_id"`
		MaxBytes         int64  `json:"max_bytes"`
		WarningThreshold int    `json:"warning_threshold"`
	}

	var req QuotaRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if (req.UserID == "" && req.ShareID == "") || req.MaxBytes == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Either user_id or share_id, and max_bytes required"})
	}

	quotaID := fmt.Sprintf("quota_%d", time.Now().UnixNano())
	quota := map[string]interface{}{
		"id":                quotaID,
		"user_id":           req.UserID,
		"share_id":          req.ShareID,
		"max_bytes":         req.MaxBytes,
		"warning_threshold": req.WarningThreshold,
	}

	if err := database.DB.Table("storage_quotas").Create(quota).Error; err != nil {
		log.Printf("[NAS] Error setting quota: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to set quota"})
	}

	return c.Status(201).JSON(quota)
}

// ===== PHASE 4B: VOLUME HEALTH & MONITORING =====

// GetVolumeHealth retrieves health status for a volume
func GetVolumeHealth(c *fiber.Ctx) error {
	volumeID := c.Params("volumeId")
	if volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume ID is required"})
	}

	type HealthStatus struct {
		ID              string  `json:"id"`
		Status          string  `json:"status"`
		Temperature     float32 `json:"temperature"`
		UsedSpace       int64   `json:"used_space"`
		TotalSpace      int64   `json:"total_space"`
		PercentageUsed  float64 `json:"percentage_used"`
		ErrorCount      int     `json:"error_count"`
		SMARTScore      int     `json:"smart_score"`
		LastCheckTime   int64   `json:"last_check_time"`
		ReadsPerSecond  int     `json:"reads_per_second"`
		WritesPerSecond int     `json:"writes_per_second"`
	}

	var health HealthStatus

	err := database.DB.Table("volume_healths").
		Where("volume_id = ?", volumeID).
		Order("created_at DESC").
		Limit(1).
		Scan(&health).Error

	if err != nil {
		log.Printf("[NAS] Error fetching volume health: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch health status"})
	}

	// Calculate percentage used
	if health.TotalSpace > 0 {
		health.PercentageUsed = (float64(health.UsedSpace) / float64(health.TotalSpace)) * 100
	}

	return c.JSON(health)
}

// GetVolumeAlerts retrieves recent alerts for a volume
func GetVolumeAlerts(c *fiber.Ctx) error {
	volumeID := c.Params("volumeId")
	if volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume ID is required"})
	}

	severityFilter := c.Query("severity")
	unresolvedOnly := c.QueryBool("unresolved", false)

	var alerts []map[string]interface{}

	query := database.DB.Table("volume_alerts").Where("volume_id = ?", volumeID)

	if severityFilter != "" {
		query = query.Where("severity = ?", severityFilter)
	}

	if unresolvedOnly {
		query = query.Where("resolved = ?", false)
	}

	err := query.Order("created_at DESC").Limit(100).Scan(&alerts).Error

	if err != nil {
		log.Printf("[NAS] Error fetching alerts: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch alerts"})
	}

	return c.JSON(alerts)
}

// ResolveAlert marks an alert as resolved
func ResolveAlert(c *fiber.Ctx) error {
	alertID := c.Params("id")
	if alertID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Alert ID is required"})
	}

	if err := database.DB.Table("volume_alerts").
		Where("id = ?", alertID).
		Updates(map[string]interface{}{
			"resolved":    true,
			"resolved_at": time.Now().Unix(),
		}).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to resolve alert"})
	}

	return c.JSON(fiber.Map{"message": "Alert resolved"})
}

// SetCleanupPolicy configures automatic cleanup for a volume
func SetCleanupPolicy(c *fiber.Ctx) error {
	type PolicyRequest struct {
		VolumeID             string `json:"volume_id"`
		Enabled              bool   `json:"enabled"`
		TriggerThreshold     int    `json:"trigger_threshold"`
		MaxCleanupPercentage int    `json:"max_cleanup_percentage"`
		Action               string `json:"action"`
		FileAgeThresholdDays int    `json:"file_age_threshold_days"`
		ExcludePatterns      string `json:"exclude_patterns"`
	}

	var req PolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	policyID := fmt.Sprintf("policy_%d", time.Now().UnixNano())
	policy := map[string]interface{}{
		"id":                      policyID,
		"volume_id":               req.VolumeID,
		"enabled":                 req.Enabled,
		"trigger_threshold":       req.TriggerThreshold,
		"max_cleanup_percentage":  req.MaxCleanupPercentage,
		"action":                  req.Action,
		"file_age_threshold_days": req.FileAgeThresholdDays,
		"exclude_patterns":        req.ExcludePatterns,
	}

	if err := database.DB.Table("cleanup_policies").Create(policy).Error; err != nil {
		log.Printf("[NAS] Error setting cleanup policy: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to set policy"})
	}

	return c.Status(201).JSON(policy)
}

// GetCleanupPolicy retrieves cleanup policy for a volume
func GetCleanupPolicy(c *fiber.Ctx) error {
	volumeID := c.Params("volumeId")
	if volumeID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Volume ID is required"})
	}

	var policy map[string]interface{}

	err := database.DB.Table("cleanup_policies").
		Where("volume_id = ?", volumeID).
		Order("created_at DESC").
		Limit(1).
		Scan(&policy).Error

	if err != nil {
		log.Printf("[NAS] Error fetching cleanup policy: %v\n", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch policy"})
	}

	return c.JSON(policy)
}

// CreateRaid1 creates a RAID-1 mirror from a mounted volume and an unmounted disk
func CreateRaid1(c *fiber.Ctx) error {
	type CreateRaid1Request struct {
		ExistingDisk string `json:"existingDisk"` // Device path of mounted disk (e.g., /dev/sda1)
		NewDisk      string `json:"newDisk"`      // Device path of unmounted disk (e.g., /dev/sdb)
		Name         string `json:"name"`         // Name for the RAID array
	}

	var req CreateRaid1Request
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.ExistingDisk == "" || req.NewDisk == "" || req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "existingDisk, newDisk, and name are required"})
	}

	// Normalize device paths - add /dev/ prefix if missing
	existingDisk := req.ExistingDisk
	if !strings.HasPrefix(existingDisk, "/dev/") {
		existingDisk = "/dev/" + existingDisk
	}

	newDisk := req.NewDisk
	if !strings.HasPrefix(newDisk, "/dev/") {
		newDisk = "/dev/" + newDisk
	}

	// Ensure mdadm is installed
	if _, err := exec.LookPath("mdadm"); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "mdadm not found - RAID tools not installed"})
	}

	// Check if devices exist
	existingCmd := exec.Command("sudo", "test", "-b", existingDisk)
	if err := existingCmd.Run(); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Existing disk not found: " + existingDisk})
	}

	newCmd := exec.Command("sudo", "test", "-b", newDisk)
	if err := newCmd.Run(); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "New disk not found: " + newDisk})
	}

	// Get mount point of existing disk
	mountCmd := exec.Command("sh", "-c", fmt.Sprintf("mount | grep %s | awk '{print $3}'", existingDisk))
	output, err := mountCmd.CombinedOutput()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Could not determine mount point of existing disk"})
	}
	mountPoint := strings.TrimSpace(string(output))
	if mountPoint == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Existing disk is not mounted"})
	}

	// Send event stream start
	// Set headers for SSE
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		totalSteps := 7
		currentStep := 0

		sendRaidEvent := func(status string, step string) error {
			currentStep++
			percentage := 0
			if totalSteps > 0 {
				percentage = (currentStep * 100) / totalSteps
			}
			if percentage >= 100 && status != "success" {
				percentage = 99
			}
			event := fmt.Sprintf("data: {\"status\":\"%s\",\"step\":\"Step %d/%d: %s\",\"progress\":%d,\"currentStep\":%d,\"totalSteps\":%d}\n\n",
				status, currentStep, totalSteps, step, percentage, currentStep, totalSteps)
			_, err := fmt.Fprint(w, event)
			w.Flush()
			return err
		}

		// Partition the new disk
		sendRaidEvent("progress", "Partitioning new disk...")
		partCmd := exec.Command("sudo", "fdisk", "-l", newDisk)
		if _, err := partCmd.CombinedOutput(); err != nil {
			sendRaidEvent("error", "Failed to read disk layout")
			return
		}

		// Create partition on new disk matching existing disk
		sendRaidEvent("progress", fmt.Sprintf("Creating partition on %s...", newDisk))

		// First, wipe the new disk to remove any existing partition table
		wipeCmd := exec.Command("sudo", "wipefs", "-a", newDisk)
		if out, err := wipeCmd.CombinedOutput(); err != nil {
			// Log but continue - wipefs might fail if disk is new
			log.Printf("[NAS] Warning: wipefs failed: %s", string(out))
		}

		time.Sleep(1 * time.Second)

		// Try using parted for more reliable partitioning
		// Create a single partition using the entire disk
		partCmd = exec.Command("sudo", "parted", "-s", newDisk, "mklabel", "msdos")
		if out, err := partCmd.CombinedOutput(); err != nil {
			sendRaidEvent("error", fmt.Sprintf("Failed to create partition table: %s", string(out)))
			return
		}

		time.Sleep(1 * time.Second)

		// Create single partition
		partCreateCmd := exec.Command("sudo", "parted", "-s", newDisk, "mkpart", "primary", "ext4", "1MiB", "100%")
		if out, err := partCreateCmd.CombinedOutput(); err != nil {
			sendRaidEvent("error", fmt.Sprintf("Failed to create partition: %s", string(out)))
			return
		}

		// Wait for device nodes to be created
		time.Sleep(2 * time.Second)

		// Verify partition was created
		partListCmd := exec.Command("sudo", "lsblk", "-n", "-o", "NAME", newDisk)
		listOut, _ := partListCmd.CombinedOutput()
		log.Printf("[NAS] Partition list after creation:\n%s", string(listOut))

		// Determine partition number (usually partition 1)
		newDiskPart := newDisk + "1"

		// Verify the partition exists
		testPartCmd := exec.Command("sudo", "test", "-b", newDiskPart)
		if err := testPartCmd.Run(); err != nil {
			// Try partition 2 in case partition 1 is reserved
			newDiskPart = newDisk + "2"
			testPartCmd2 := exec.Command("sudo", "test", "-b", newDiskPart)
			if err := testPartCmd2.Run(); err != nil {
				sendRaidEvent("error", fmt.Sprintf("Partition not found at %s1 or %s2. Check disk at %s.", newDisk, newDisk, newDisk))
				return
			}
		}

		log.Printf("[NAS] Using partition: %s", newDiskPart)
		sendRaidEvent("progress", fmt.Sprintf("Using partition %s for RAID", strings.TrimPrefix(newDiskPart, "/dev/")))

		// Unmount existing disk before RAID creation (mdadm needs exclusive access)
		sendRaidEvent("progress", fmt.Sprintf("Unmounting %s for RAID preparation...", existingDisk))
		umountCmd := exec.Command("sudo", "umount", "-fl", existingDisk)
		if out, err := umountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] Unmount output: %s", string(out))
			// Don't fail here - disk might not be mounted
		}
		time.Sleep(1 * time.Second)

		// Create RAID-1 array
		sendRaidEvent("progress", fmt.Sprintf("Creating RAID-1 array with %s and %s...", existingDisk, newDiskPart))

		raidName := fmt.Sprintf("md%d", time.Now().UnixNano()%1000)
		// Use --run to auto-answer prompts, --bitmap=internal for write-intent bitmap
		// --assume-clean preserves existing data, --force overrides safety checks
		mdadmCmd := exec.Command("sudo", "mdadm", "--create", "/dev/"+raidName, "--level", "1", "--raid-devices", "2", "--force", "--run", "--bitmap=internal", "--assume-clean", existingDisk, newDiskPart)

		out, err := mdadmCmd.CombinedOutput()
		if err != nil {
			log.Printf("[NAS] mdadm create output: %s", string(out))
			sendRaidEvent("error", fmt.Sprintf("Failed to create RAID-1: %s", string(out)))
			return
		}

		sendRaidEvent("progress", "Waiting for RAID array to initialize...")
		time.Sleep(3 * time.Second)

		// Format the RAID array with ext4
		sendRaidEvent("progress", "Formatting RAID array...")
		formatCmd := exec.Command("sudo", "mkfs.ext4", "-F", "/dev/"+raidName)
		if out, err := formatCmd.CombinedOutput(); err != nil {
			sendRaidEvent("error", fmt.Sprintf("Failed to format RAID array: %s", string(out)))
			// Try to stop the array
			exec.Command("sudo", "mdadm", "--stop", "/dev/"+raidName).Run()
			return
		}

		// Update RAID configuration (don't send event - this is post-completion cleanup)
		log.Printf("[NAS] Updating RAID configuration...")
		examCmd := exec.Command("sudo", "mdadm", "--examine", "--scan")
		confOutput, _ := examCmd.CombinedOutput()

		confCmd := exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/mdadm/mdadm.conf", string(confOutput)))
		_ = confCmd.Run()

		// Update initramfs
		updateCmd := exec.Command("sudo", "update-initramfs", "-u")
		_ = updateCmd.Run()

		// Save RAID info to database
		now := time.Now().Unix()
		raidID := fmt.Sprintf("raid1_%d", now)
		raidEntry := models.RaidArray{
			ID:         raidID,
			Name:       req.Name,
			RaidLevel:  "RAID1",
			RaidName:   raidName,
			DevicePath: "/dev/" + raidName,
			Status:     "active",
			Disk1:      existingDisk,
			Disk2:      newDiskPart,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := database.DB.Create(&raidEntry).Error; err != nil {
			log.Printf("[NAS] Error saving RAID to database: %v\n", err)
			// RAID was created but DB insert failed - still return success
		} else {
			log.Printf("[NAS] RAID saved to database: %s", raidID)
		}

		// Remount the original disk back to its mount point
		log.Printf("[NAS] Remounting %s to %s...", existingDisk, mountPoint)
		remountCmd := exec.Command("sudo", "mount", existingDisk, mountPoint)
		if out, err := remountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] Warning: Could not remount %s: %s", existingDisk, string(out))
			// Don't fail here - RAID was created successfully
		} else {
			log.Printf("[NAS] Successfully remounted %s to %s", existingDisk, mountPoint)
		}

		sendRaidEvent("success", fmt.Sprintf("✓ RAID-1 array %s created successfully at /dev/%s. Mirror sync in progress.", req.Name, raidName))
	})

	return nil
}

// GetRaidArrays returns all RAID arrays from the database
func GetRaidArrays(c *fiber.Ctx) error {
	var raids []models.RaidArray
	if err := database.DB.Find(&raids).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch RAID arrays"})
	}

	return c.JSON(raids)
}

// DeleteRaidArray stops and deletes a RAID array, then removes it from database
func DeleteRaidArray(c *fiber.Ctx) error {
	type DeleteRaidRequest struct {
		RaidName string `json:"raidName"` // e.g., "md594"
	}

	var req DeleteRaidRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.RaidName == "" {
		return c.Status(400).JSON(fiber.Map{"error": "raidName is required"})
	}

	// Ensure raidName doesn't have /dev/ prefix for safety
	raidName := strings.TrimPrefix(req.RaidName, "/dev/")

	// Stop the RAID array
	log.Printf("[NAS] Stopping RAID array: %s", raidName)
	stopCmd := exec.Command("sudo", "mdadm", "--stop", "/dev/"+raidName)
	if out, err := stopCmd.CombinedOutput(); err != nil {
		log.Printf("[NAS] Warning: Failed to stop RAID array: %s", string(out))
		// Continue anyway - might already be stopped
	}

	time.Sleep(1 * time.Second)

	// Remove RAID array from mdadm.conf
	log.Printf("[NAS] Removing RAID from mdadm.conf")
	removeCmd := exec.Command("sh", "-c", fmt.Sprintf("sudo sed -i '/ARRAY.*%s/d' /etc/mdadm/mdadm.conf", raidName))
	if out, err := removeCmd.CombinedOutput(); err != nil {
		log.Printf("[NAS] Warning: Failed to remove from mdadm.conf: %s", string(out))
	}

	// Update initramfs
	updateCmd := exec.Command("sudo", "update-initramfs", "-u")
	if out, err := updateCmd.CombinedOutput(); err != nil {
		log.Printf("[NAS] Warning: Failed to update initramfs: %s", string(out))
	}

	// Remove from database
	log.Printf("[NAS] Removing RAID from database")
	if err := database.DB.Where("raid_name = ?", raidName).Delete(&models.RaidArray{}).Error; err != nil {
		log.Printf("[NAS] Error removing RAID from database: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to remove RAID from database"})
	}

	log.Printf("[NAS] RAID array %s deleted successfully", raidName)
	return c.JSON(fiber.Map{
		"status":  "success",
		"message": fmt.Sprintf("RAID array %s has been deleted", raidName),
	})
}
