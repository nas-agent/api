package main

import (
	"api/database"
	"api/routes"
	"api/services"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

// checkSudoersConfig verifies that required commands are in sudoers
// and attempts to auto-setup if not configured
func checkSudoersConfig() {
	log.Println("Checking sudoers configuration for NAS operations...")

	// Test if sudo mkdir works without password
	cmd := exec.Command("sudo", "-n", "test", "-w", "/mnt")
	if err := cmd.Run(); err != nil {
		log.Println("⚠️  Passwordless sudo not configured. Attempting automatic setup...")

		// Try to find and run the setup script
		if setupRun() {
			log.Println("✓ Sudoers configuration completed successfully!")
			return
		}

		// If auto-setup failed, show manual instructions
		log.Println("")
		log.Println("⚠️  WARNING: Passwordless sudo not configured!")
		log.Println("Please run the setup script manually:")
		log.Println("")
		log.Println("  sudo ./setup-nas-sudo.sh")
		log.Println("")
		log.Println("Or run this command:")
		log.Println("  sudo tee -a /etc/sudoers.d/nas-mount << EOF")
		log.Println("  %sudo ALL=(ALL) NOPASSWD: /bin/mkdir")
		log.Println("  %sudo ALL=(ALL) NOPASSWD: /bin/mount")
		log.Println("  %sudo ALL=(ALL) NOPASSWD: /bin/umount")
		log.Println("  EOF")
		log.Println("")
		log.Println("NAS mount features may fail without this configuration.")
		log.Println("")
	} else {
		log.Println("✓ Sudoers configuration verified")
	}
}

// setupRun attempts to find and execute the setup script
func setupRun() bool {
	// Get the directory of the current executable
	exe, err := os.Executable()
	if err != nil {
		log.Println("Could not determine executable path")
		return false
	}
	exeDir := filepath.Dir(exe)

	// Try bash script first
	bashScript := filepath.Join(exeDir, "setup-nas-sudo.sh")
	if _, err := os.Stat(bashScript); err == nil {
		log.Println("Running setup script:", bashScript)
		cmd := exec.Command("sudo", bashScript)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return true
		}
		log.Println("Bash script setup failed:", err)
	}

	// Try Python script as fallback
	pythonScript := filepath.Join(exeDir, "setup-nas-sudo.py")
	if _, err := os.Stat(pythonScript); err == nil {
		log.Println("Running setup script:", pythonScript)
		cmd := exec.Command("sudo", "python3", pythonScript)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return true
		}
		log.Println("Python script setup failed:", err)
	}

	log.Println("No setup scripts found in:", exeDir)
	return false
}

// startUDPDiscoveryListener starts a UDP listener for NAS API discovery
// Responds to "WHO_IS_NAS_API?" broadcasts on port 9999
func startUDPDiscoveryListener() {
	go func() {
		addr := net.UDPAddr{
			Port: 9999,
			IP:   net.ParseIP("0.0.0.0"),
		}
		conn, err := net.ListenUDP("udp", &addr)
		if err != nil {
			log.Printf("⚠️  Failed to start UDP discovery listener: %v", err)
			return
		}
		defer conn.Close()

		log.Println("✓ UDP Discovery listener started on port 9999")

		buffer := make([]byte, 1024)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				log.Printf("UDP read error: %v", err)
				continue
			}

			message := string(buffer[:n])
			if message == "WHO_IS_NAS_API?" {
				// Respond with API_HERE:3000
				response := "API_HERE:3000"
				_, err := conn.WriteToUDP([]byte(response), remoteAddr)
				if err != nil {
					log.Printf("UDP write error: %v", err)
				}
				log.Printf("📡 Responded to discovery request from %s", remoteAddr.String())
			}
		}
	}()
}

func main() {
	// Check and setup sudoers configuration
	checkSudoersConfig()

	// Initialize Database Connection and Auto-Migrate
	database.ConnectDB()

	// Initialize existing RAID arrays from system
	database.InitializeRaidArraysFromSystem()

	// Initialize File Watcher Service
	services.InitFileWatcher()

	// Start UDP Discovery Listener
	startUDPDiscoveryListener()

	// Initialize Fiber App
	app := fiber.New()

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*", // Frontend Dev Server Route
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		ExposeHeaders:    "Content-Length, Content-Type",
		AllowCredentials: false,
	}))

	// Serve uploaded files statically
	app.Static("/uploads", "./data/uploads")

	// Setup Routes
	routes.SetupSetup(app)

	// Start Server
	log.Fatal(app.Listen(":3000"))
}
