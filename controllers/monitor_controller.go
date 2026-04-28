package controllers

import (
	"api/config"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
	"github.com/shirou/gopsutil/v4/disk"
	"net/http"
	"time"
)

type SystemStats struct {
	CPUUsage  float64      `json:"cpu_usage"`
	RAM       RAMStats     `json:"ram"`
	Network   NetworkStats `json:"network"`
	CPUTempC  float64      `json:"cpu_temp_c"`
	Uptime    string       `json:"uptime"`
	Timestamp int64        `json:"timestamp"`
}

type DiskStats struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

type RAMStats struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
}

type NetworkStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

func GetDiskStats(c *fiber.Ctx) error {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get partitions"})
	}

	var results []DiskStats
	for _, p := range partitions {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}

		// Filter out small/virtual partitions if needed, but for NAS we want main ones
		// Usually we want /, /mnt/*, /srv/*
		results = append(results, DiskStats{
			Name:    p.Device,
			Path:    p.Mountpoint,
			Total:   usage.Total,
			Used:    usage.Used,
			Free:    usage.Free,
			Percent: usage.UsedPercent,
		})
	}

	return c.JSON(results)
}

func GetSystemStats(c *fiber.Ctx) error {
	// 1. CPU Usage
	cpuPercents, _ := cpu.Percent(time.Second, false)
	var cpuUsage float64
	if len(cpuPercents) > 0 {
		cpuUsage = cpuPercents[0]
	}

	// 2. RAM Stats
	vMem, _ := mem.VirtualMemory()
	ram := RAMStats{
		Total:       vMem.Total,
		Used:        vMem.Used,
		UsedPercent: vMem.UsedPercent,
	}

	// 3. Network Stats (Aggregated across all interfaces)
	netIO, _ := net.IOCounters(false)
	network := NetworkStats{}
	if len(netIO) > 0 {
		network.RxBytes = netIO[0].BytesRecv
		network.TxBytes = netIO[0].BytesSent
	}

	// 4. Host Info (Uptime)
	hInfo, _ := host.Info()
	uptimeDuration := time.Duration(hInfo.Uptime) * time.Second
	uptimeStr := fmt.Sprintf("%dd %dh %dm", 
		int(uptimeDuration.Hours())/24, 
		int(uptimeDuration.Hours())%24, 
		int(uptimeDuration.Minutes())%60)

	// 5. Temperature (Using sensors subpackage in v4)
	var temp float64
	temps, err := sensors.SensorsTemperatures()
	if err == nil && len(temps) > 0 {
		temp = temps[0].Temperature
	} else {
		temp = 0.0 
	}

	stats := SystemStats{
		CPUUsage:  cpuUsage,
		RAM:       ram,
		Network:   network,
		CPUTempC:  temp,
		Uptime:    uptimeStr,
		Timestamp: time.Now().Unix(),
	}

	return c.JSON(stats)
}

func GetAIMonitorStats(c *fiber.Ctx) error {
	aiConfig := config.GetAIServiceConfig()
	
	// Fetch from agent service
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", aiConfig.Endpoint("/api/monitor"), nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create request"})
	}
	
	req.Header.Set("X-API-Key", aiConfig.APIKey)
	
	resp, err := client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Agent service unreachable"})
	}
	defer resp.Body.Close()
	
	var stats interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to decode agent response"})
	}
	
	return c.JSON(stats)
}
