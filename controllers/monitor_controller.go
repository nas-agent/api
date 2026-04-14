package controllers

import (
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
	"time"
)

type SystemStats struct {
	CPUUsage   float64            `json:"cpu_usage"`
	RAM        RAMStats           `json:"ram"`
	Network    NetworkStats       `json:"network"`
	CPUTempC   float64            `json:"cpu_temp_c"`
	Uptime     string             `json:"uptime"`
	Timestamp  int64              `json:"timestamp"`
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
