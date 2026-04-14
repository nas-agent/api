package controllers

import (
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/shirou/gopsutil/v4/disk"
	"strings"
)

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

func GetStorageDevices(c *fiber.Ctx) error {
	var mounted []LogicalVolume
	partitions, err := disk.Partitions(true)
	
	if err == nil {
		for i, part := range partitions {
			// Filter out pseudo-filesystems
			if strings.HasPrefix(part.Fstype, "squashfs") || part.Fstype == "" || part.Fstype == "proc" || part.Fstype == "sysfs" || part.Fstype == "tmpfs" || part.Fstype == "devtmpfs" {
				continue
			}

			usage, errUsage := disk.Usage(part.Mountpoint)
			if errUsage != nil {
				continue
			}

			mounted = append(mounted, LogicalVolume{
				ID:         fmt.Sprintf("vol-%d", i),
				Name:       part.Device,
				MountPoint: part.Mountpoint,
				FileSystem: part.Fstype,
				RaidLevel:  "Single", // Default stub assuming single disk mapping
				RaidState:  "Active",
				TotalSize:  fmt.Sprintf("%d", usage.Total),
				UsedSize:   fmt.Sprintf("%d", usage.Used),
				Disks:      []string{part.Device},
			})
		}
	} else {
		fmt.Printf("Error fetching partitions: %v\n", err)
	}

	// Returning empty array for unmounted on generic OS implementation as it requires low-level block scanning (e.g., lsblk wrapping) which is OS specific
	return c.JSON(fiber.Map{
		"mounted":   mounted,
		"unmounted": []interface{}{},
	})
}
