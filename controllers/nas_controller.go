package controllers

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

func readCPUTemp() float64 {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	tempStr := strings.TrimSpace(string(data))

	// temp is in millidegree, e.g. 52000 = 52.0°C
	milli, err := strconv.Atoi(tempStr)
	if err != nil {
		return 0
	}
	return float64(milli) / 1000.0
}

func GetNASMonitor(c *gin.Context) {

	// CPU usage
	cpuPercent, _ := cpu.Percent(0, false)

	// CPU temperature (real RPi method)
	cpuTemp := readCPUTemp()

	// RAM
	vm, _ := mem.VirtualMemory()

	// Network bytes
	netIO, _ := net.IOCounters(false)
	network := netIO[0]

	c.JSON(http.StatusOK, gin.H{
		"cpu_usage":  cpuPercent[0],
		"cpu_temp_c": fmt.Sprintf("%.2f", cpuTemp),
		"ram": gin.H{
			"total":        vm.Total,
			"used":         vm.Used,
			"free":         vm.Free,
			"used_percent": vm.UsedPercent,
		},
		"network": gin.H{
			"rx_bytes": network.BytesRecv,
			"tx_bytes": network.BytesSent,
		},
	})
}
