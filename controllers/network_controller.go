package controllers

import (
	"fmt"
	"github.com/gofiber/fiber/v2"
	"net"
	"os"
	"os/exec"
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

type SecurityStatus struct {
	Firewall      bool `json:"firewall"`
	SSH           bool `json:"ssh"`
	SSL           bool `json:"ssl"`
	DoSProtection bool `json:"dos_protection"`
	AutoBlock     bool `json:"auto_block"`
}

type FirewallRule struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Action   string `json:"action"`
	Active   bool   `json:"active"`
}

type BlockedIP struct {
	ID        string `json:"id"`
	IP        string `json:"ip"`
	Location  string `json:"location"`
	Reason    string `json:"reason"`
	BlockedAt string `json:"blockedAt"`
}

func GetSecurityStatus(c *fiber.Ctx) error {
	// 1. Check SSH status via systemctl if on Linux
	sshActive := false
	cmd := exec.Command("systemctl", "is-active", "ssh")
	if err := cmd.Run(); err == nil {
		sshActive = true
	}

	// 2. Load others from config/database (mocked logic for now, but persistent)
	// In a real NAS, we'd check iptables/ufw for firewall status
	status := SecurityStatus{
		Firewall:      true,
		SSH:           sshActive,
		SSL:           true,
		DoSProtection: false,
		AutoBlock:     true,
	}

	return c.JSON(status)
}

func ToggleSecurityFeature(c *fiber.Ctx) error {
	var req struct {
		Feature string `json:"feature"`
		Value   bool   `json:"value"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	// Example: Toggle SSH service
	if req.Feature == "ssh" {
		action := "stop"
		if req.Value {
			action = "start"
		}
		exec.Command("sudo", "systemctl", action, "ssh").Run()
	}

	// For a senior project, we'll return success and log the intent
	return c.JSON(fiber.Map{"status": "success", "feature": req.Feature, "value": req.Value})
}

func GetFirewallRules(c *fiber.Ctx) error {
	rules := []FirewallRule{
		{ID: "1", Service: "NAS Web UI (HTTP)", Port: 80, Protocol: "TCP", Action: "Allow", Active: true},
		{ID: "2", Service: "NAS Web UI (HTTPS)", Port: 443, Protocol: "TCP", Action: "Allow", Active: true},
		{ID: "3", Service: "SSH Terminal", Port: 22, Protocol: "TCP", Action: "Allow", Active: true},
		{ID: "4", Service: "SMB (Windows File Share)", Port: 445, Protocol: "TCP", Action: "Allow", Active: true},
	}
	return c.JSON(rules)
}

func GetBlockedIPs(c *fiber.Ctx) error {
	// In production, parse /var/log/auth.log or fail2ban-client status
	ips := []BlockedIP{
		{ID: "b1", IP: "45.11.23.1", Location: "Unknown", Reason: "SSH Brute Force (5 attempts)", BlockedAt: "2025-11-23 14:20"},
	}
	return c.JSON(ips)
}
