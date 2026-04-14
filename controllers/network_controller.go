package controllers

import (
	"fmt"
	"github.com/gofiber/fiber/v2"
	"net"
	"os"
	"strings"
)

type NetworkInterface struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	MAC    string `json:"mac"`
	Status string `json:"status"`
	Speed  string `json:"speed"`
}

func GetNetworkInterfaces(c *fiber.Ctx) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	var results []NetworkInterface
	for _, iface := range interfaces {
		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Get IP
		addrs, _ := iface.Addrs()
		ip := "N/A"
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ip = ipnet.IP.String()
					break
				}
			}
		}

		status := "Disconnected"
		if iface.Flags&net.FlagUp != 0 {
			status = "Connected"
		}

		// Get Speed (Linux specific)
		// On Raspberry Pi, this usually works for eth0
		speed := "N/A"
		speedFile := fmt.Sprintf("/sys/class/net/%s/speed", iface.Name)
		if data, err := os.ReadFile(speedFile); err == nil {
			speedValue := strings.TrimSpace(string(data))
			if speedValue != "" {
				speed = speedValue + " Mbps"
			}
		}

		results = append(results, NetworkInterface{
			Name:   iface.Name,
			IP:     ip,
			MAC:    iface.HardwareAddr.String(),
			Status: status,
			Speed:  speed,
		})
	}

	return c.JSON(results)
}
