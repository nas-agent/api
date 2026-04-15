package controllers

import (
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

	for _, dev := range lsblk.Blockdevices {
		// Process each block device (disk, lvm, raid)
		if dev.Type == "disk" {
			// A disk might have partitions. We need to check if ANY of its children are mounted.
			hasMountedChild := false
			processDeviceTree(dev, &mounted, &unmounted, &hasMountedChild, mountedPaths)

			// If neither the disk nor any children are mounted, it's an unmounted physical drive
			// List all unmounted disks (both raw disks and partitioned disks)
			// Raw disks can be formatted using FormatAndMount endpoint
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
	Device   string `json:"device"`
	MountDir string `json:"mountDir"`
}

type FormatAndMountRequest struct {
	Device     string `json:"device"`     // e.g., "/dev/sdb"
	MountName  string `json:"mountName"`  // e.g., "NAS" - will mount to /mnt/NAS, or leave empty for default
	FileSystem string `json:"fileSystem"` // e.g., "ext4", defaults to "ext4"
}

// FormatAndMount formats a disk and mounts it persistently
// Steps:
// 1. Unmount and wipe old filesystem
// 2. Format to specified filesystem (default ext4)
// 3. Create mount directory and mount
// 4. Get UUID and add to /etc/fstab for persistence
func FormatAndMount(c *fiber.Ctx) error {
	var req FormatAndMountRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Device == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Device path is required"})
	}

	// Validate device is a partition, not raw disk
	deviceName := filepath.Base(req.Device)
	lastChar := deviceName[len(deviceName)-1]
	isPartition := lastChar >= '0' && lastChar <= '9'

	// For raw disks, we'll use the whole disk (e.g., /dev/sdb -> /dev/sdb1 after part init)
	// But for safety, we require explicit partitioning first
	if !isPartition {
		// Actually, let's allow raw disk formatting for simplicity
		// We'll create a single partition on it
		log.Printf("[NAS] Format and mount on raw disk: %s\n", req.Device)
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
		// Step 1: Unmount and Wipe
		sendProgress(w, "Unmounting existing mounts...", "loading")
		unmountCmd := exec.Command("bash", "-c", fmt.Sprintf("sudo umount -l %s* 2>/dev/null || true", req.Device))
		if output, err := unmountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] Unmount warning (may be ok): %v\n", err)
			sendProgress(w, fmt.Sprintf("Warning unmounting existing mounts: %s", string(output)), "warning")
		}
		time.Sleep(500 * time.Millisecond)

		sendProgress(w, "Wiping old filesystem signatures...", "loading")
		wipeCmd := exec.Command("sudo", "wipefs", "-a", req.Device)
		if output, err := wipeCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] wipe failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("Warning wiping disk: %s", string(output)), "warning")
		}
		time.Sleep(500 * time.Millisecond)

		// Step 2: Format
		sendProgress(w, fmt.Sprintf("Formatting %s to %s...", req.Device, req.FileSystem), "loading")
		var formatCmd *exec.Cmd
		switch req.FileSystem {
		case "ext4":
			formatCmd = exec.Command("sudo", "mkfs.ext4", "-F", req.Device)
		case "ext3":
			formatCmd = exec.Command("sudo", "mkfs.ext3", "-F", req.Device)
		case "ntfs":
			formatCmd = exec.Command("sudo", "mkfs.ntfs", "-f", req.Device)
		case "vfat":
			formatCmd = exec.Command("sudo", "mkfs.vfat", "-F", "32", req.Device)
		default:
			sendProgress(w, fmt.Sprintf("Unsupported filesystem: %s", req.FileSystem), "error")
			return
		}

		if output, err := formatCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] format failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("Format failed: %s", string(output)), "error")
			return
		}
		log.Printf("[NAS] Format completed successfully: %s\n", req.Device)
		time.Sleep(500 * time.Millisecond)

		// Step 3: Create mount directory and mount
		sendProgress(w, fmt.Sprintf("Creating mount directory %s...", mountDir), "loading")
		mkdirCmd := exec.Command("sudo", "mkdir", "-p", mountDir)
		if output, err := mkdirCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mkdir failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("Failed to create directory: %s", string(output)), "error")
			return
		}
		time.Sleep(500 * time.Millisecond)

		sendProgress(w, fmt.Sprintf("Mounting %s to %s...", req.Device, mountDir), "loading")
		mountCmd := exec.Command("sudo", "mount", req.Device, mountDir)
		if output, err := mountCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mount failed: %v, output: %s\n", err, string(output))
			sendProgress(w, fmt.Sprintf("Mount failed: %s", string(output)), "error")
			return
		}
		log.Printf("[NAS] Mounted successfully: %s -> %s\n", req.Device, mountDir)
		time.Sleep(500 * time.Millisecond)

		// Step 4: Get UUID and add to fstab for persistence
		sendProgress(w, "Retrieving disk UUID for persistent mounting...", "loading")
		uuidCmd := exec.Command("bash", "-c", fmt.Sprintf("sudo blkid -s UUID -o value %s", req.Device))
		uuidOutput, err := uuidCmd.CombinedOutput()
		if err != nil {
			log.Printf("[NAS] UUID retrieval failed: %v\n", err)
			// Continue anyway, we can still mount without fstab
			sendProgress(w, "Warning: Could not retrieve UUID, disk will not auto-mount on reboot", "warning")
		} else {
			uuid := strings.TrimSpace(string(uuidOutput))
			if uuid != "" {
				log.Printf("[NAS] Retrieved UUID: %s\n", uuid)
				sendProgress(w, fmt.Sprintf("Adding UUID=%s to /etc/fstab...", uuid), "loading")

				// Create fstab entry
				fstabEntry := fmt.Sprintf("UUID=%s %s %s defaults,noatime 0 2", uuid, mountDir, req.FileSystem)
				log.Printf("[NAS] fstab entry: %s\n", fstabEntry)

				// Add to fstab using tee
				echoCmd := fmt.Sprintf("echo '%s' | sudo tee -a /etc/fstab > /dev/null", fstabEntry)
				fstabCmd := exec.Command("bash", "-c", echoCmd)
				if output, err := fstabCmd.CombinedOutput(); err != nil {
					log.Printf("[NAS] fstab update failed: %v, output: %s\n", err, string(output))
					sendProgress(w, fmt.Sprintf("Warning: Could not update fstab: %s", string(output)), "warning")
				} else {
					log.Printf("[NAS] Added to fstab successfully\n")
					time.Sleep(500 * time.Millisecond)
				}
			}
		}

		// Success!
		sendProgress(w, fmt.Sprintf("Disk formatted and mounted successfully at %s!", mountDir), "success")
		log.Printf("[NAS] Format and mount completed successfully\n")
	})

	return nil
}

// sendProgress sends a progress message as SSE
func sendProgress(w *bufio.Writer, message, status string) {
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

		fmt.Fprintf(w, "data: {\"step\":\"Creating mount point %s...\", \"status\":\"loading\"}\n\n", mountDir)
		w.Flush()
		time.Sleep(500 * time.Millisecond) // realistic UX delay

		// Create mount point if it doesn't exist (use sudo for /mnt access)
		mkdirCmd := exec.Command("sudo", "mkdir", "-p", mountDir)
		if output, err := mkdirCmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mkdir failed: %v, output: %s\n", err, string(output))
			fmt.Fprintf(w, "data: {\"step\":\"Failed to create directory: %s\", \"status\":\"error\"}\n\n", string(output))
			w.Flush()
			return
		}
		log.Printf("[NAS] Mount point created: %s\n", mountDir)

		fmt.Fprintf(w, "data: {\"step\":\"Mounting device %s...\", \"status\":\"loading\"}\n\n", req.Device)
		w.Flush()
		time.Sleep(500 * time.Millisecond)

		// Run mount command
		cmd := exec.Command("sudo", "mount", req.Device, mountDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[NAS] mount failed: %v, output: %s\n", err, string(output))
			fmt.Fprintf(w, "data: {\"step\":\"Mount failed: %s\", \"status\":\"error\"}\n\n", string(output))
			w.Flush()
			return
		}
		log.Printf("[NAS] Device mounted successfully: %s -> %s\n", req.Device, mountDir)

		fmt.Fprintf(w, "data: {\"step\":\"Device mounted successfully!\", \"status\":\"success\", \"mountPoint\":\"%s\"}\n\n", mountDir)
		w.Flush()
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
		fmt.Fprintf(w, "data: {\"step\":\"Unmounting %s...\", \"status\":\"loading\"}\n\n", target)
		w.Flush()
		time.Sleep(500 * time.Millisecond)

		// Run unmount command
		cmd := exec.Command("sudo", "umount", target)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(w, "data: {\"step\":\"Unmount failed: %s\", \"status\":\"error\"}\n\n", string(output))
			w.Flush()
			return
		}

		fmt.Fprintf(w, "data: {\"step\":\"Cleaning up directories...\", \"status\":\"loading\"}\n\n")
		w.Flush()
		time.Sleep(300 * time.Millisecond)

		// Optionally remove empty mount directory if it's in /mnt or /media
		if strings.HasPrefix(target, "/mnt/") || strings.HasPrefix(target, "/media/") {
			os.Remove(target) // Ignores error if directory is not empty
		}

		fmt.Fprintf(w, "data: {\"step\":\"Device unmounted successfully!\", \"status\":\"success\"}\n\n")
		w.Flush()
	})

	return nil
}
