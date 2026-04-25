package main

import (
	"api/config"
	"api/database"
	"api/routes"
	"api/services"
	"embed"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	websocket "github.com/gofiber/websocket/v2"
)

//go:embed public/*
var embeddedFiles embed.FS

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

func resolveSambaHomeBase() string {
	const defaultSambaHomeBase = "/srv/samba/homes"

	homeBase := strings.TrimSpace(os.Getenv("SAMBA_HOME_BASE"))
	if homeBase == "" {
		homeBase = defaultSambaHomeBase
		if err := os.Setenv("SAMBA_HOME_BASE", homeBase); err != nil {
			log.Printf("⚠️  Failed to set SAMBA_HOME_BASE env: %v", err)
		} else {
			log.Printf("SAMBA_HOME_BASE was not set; using default: %s", homeBase)
		}
	} else {
		log.Printf("Using SAMBA_HOME_BASE from environment: %s", homeBase)
	}

	services.Samba.HomeBase = homeBase
	return homeBase
}

func getPathOwner(path string) (string, error) {
	cmd := exec.Command("stat", "-c", "%U:%G", path)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func ensureSambaHomeBase(path string) {
	changed := false

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Creating Samba home base directory: %s", path)
			if out, mkdirErr := exec.Command("sudo", "mkdir", "-p", path).CombinedOutput(); mkdirErr != nil {
				log.Printf("⚠️  Failed to create Samba home base %s: %v (%s)", path, mkdirErr, strings.TrimSpace(string(out)))
				return
			}
			changed = true
			info, err = os.Stat(path)
			if err != nil {
				log.Printf("⚠️  Failed to stat Samba home base %s after create: %v", path, err)
				return
			}
		} else {
			log.Printf("⚠️  Failed to stat Samba home base %s: %v", path, err)
			return
		}
	}

	if !info.IsDir() {
		log.Printf("⚠️  SAMBA_HOME_BASE is not a directory: %s", path)
		return
	}

	if info.Mode().Perm() != 0o755 {
		log.Printf("Fixing permissions for Samba home base %s (current=%#o, target=0755)", path, info.Mode().Perm())
		if out, chmodErr := exec.Command("sudo", "chmod", "755", path).CombinedOutput(); chmodErr != nil {
			log.Printf("⚠️  Failed to chmod Samba home base %s: %v (%s)", path, chmodErr, strings.TrimSpace(string(out)))
		} else {
			changed = true
		}
	}

	if owner, ownerErr := getPathOwner(path); ownerErr != nil {
		log.Printf("⚠️  Failed to read owner for %s: %v", path, ownerErr)
	} else if owner != "root:root" {
		log.Printf("Fixing owner for Samba home base %s (current=%s, target=root:root)", path, owner)
		if out, chownErr := exec.Command("sudo", "chown", "root:root", path).CombinedOutput(); chownErr != nil {
			log.Printf("⚠️  Failed to chown Samba home base %s: %v (%s)", path, chownErr, strings.TrimSpace(string(out)))
		} else {
			changed = true
		}
	}

	if changed {
		if err := services.Samba.RestartService(); err != nil {
			log.Printf("⚠️  Failed to restart smbd after Samba home base updates: %v", err)
		} else {
			log.Println("✓ Restarted smbd after Samba home base updates")
		}
	} else {
		log.Printf("✓ Samba home base already configured: %s", path)
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

		// Set socket to reuse address
		if err := conn.SetReadBuffer(1024); err != nil {
			log.Printf("⚠️  Failed to set read buffer: %v", err)
		}

		log.Println("✓ UDP Discovery listener started on port 9999")
		log.Println("  Waiting for discovery broadcasts...")

		buffer := make([]byte, 1024)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				log.Printf("UDP read error: %v", err)
				continue
			}

			message := string(buffer[:n])
			log.Printf("📡 Received UDP message from %s: %q", remoteAddr.String(), message)

			if message == "WHO_IS_NAS_API?" {
				// Respond with API_HERE:3000
				response := "API_HERE:3000"
				_, err := conn.WriteToUDP([]byte(response), remoteAddr)
				if err != nil {
					log.Printf("❌ UDP write error: %v", err)
					continue
				}
				log.Printf("✅ Successfully responded to discovery request from %s", remoteAddr.String())
			} else {
				log.Printf("⚠️  Ignored non-discovery message: %q", message)
			}
		}
	}()
}

// NotificationBroadcaster manages WebSocket connections and broadcasts notifications
type NotificationBroadcaster struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan notification
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

type notification struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Message string `json:"message,omitempty"`
}

var websocketConfig = websocket.Config{
	Origins: []string{"*"},
}

var notificationBroadcaster = &NotificationBroadcaster{
	clients:    make(map[*websocket.Conn]bool),
	broadcast:  make(chan notification, 256),
	register:   make(chan *websocket.Conn),
	unregister: make(chan *websocket.Conn),
}

// Start runs the notification broadcaster loop
func (nb *NotificationBroadcaster) Start() {
	go func() {
		for {
			select {
			case client := <-nb.register:
				nb.mu.Lock()
				nb.clients[client] = true
				nb.mu.Unlock()
				log.Println("📡 Client connected to notifications")

			case client := <-nb.unregister:
				nb.mu.Lock()
				if _, ok := nb.clients[client]; ok {
					delete(nb.clients, client)
					client.Close()
				}
				nb.mu.Unlock()
				log.Println("📡 Client disconnected from notifications")

			case notif := <-nb.broadcast:
				nb.mu.RLock()
				for client := range nb.clients {
					go func(c *websocket.Conn) {
						err := c.WriteJSON(notif)
						if err != nil {
							nb.unregister <- c
						}
					}(client)
				}
				nb.mu.RUnlock()
			}
		}
	}()
}

// Broadcast sends a notification to all connected clients
func (nb *NotificationBroadcaster) Broadcast(notif interface{}) {
	// Handle both notification struct and map[string]interface{}
	switch n := notif.(type) {
	case notification:
		select {
		case nb.broadcast <- n:
		default:
			log.Println("⚠️  Broadcast channel full, notification dropped")
		}
	case map[string]interface{}:
		// Convert map to notification
		converted := notification{
			Type:    toString(n["type"]),
			Title:   toString(n["title"]),
			Body:    toString(n["body"]),
			Message: toString(n["message"]),
		}
		select {
		case nb.broadcast <- converted:
		default:
			log.Println("⚠️  Broadcast channel full, notification dropped")
		}
	default:
		log.Println("⚠️  Unknown notification type")
	}
}

// toString safely converts interface{} to string
func toString(val interface{}) string {
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func handleWebSocketNotifications(c *websocket.Conn) {
	notificationBroadcaster.register <- c

	defer func() {
		notificationBroadcaster.unregister <- c
	}()

	// Keep connection alive - listen for incoming messages (for ping/pong keeping connection alive)
	for {
		var msg notification
		err := c.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
	}
}

func main() {
	// Define command-line flags
	var adminUser string
	var adminPass string
	flag.StringVar(&config.AIServiceURLFlag, "ai-url", "", "AI Service Base URL")
	flag.StringVar(&adminUser, "admin-user", "", "Admin username to create/update")
	flag.StringVar(&adminPass, "admin-pass", "", "Admin password to create/update")
	flag.Parse()

	// Check and setup sudoers configuration
	checkSudoersConfig()

	// Resolve and prepare Samba base path on startup (idempotent)
	homeBase := resolveSambaHomeBase()
	ensureSambaHomeBase(homeBase)

	// Initialize Database Connection and Auto-Migrate
	database.ConnectDB()

	// Seed admin user if flags are provided
	if adminUser != "" && adminPass != "" {
		database.EnsureAdminUser(adminUser, adminPass)
	}

	// Initialize existing RAID arrays from system
	database.InitializeRaidArraysFromSystem()

	// Initialize File Watcher Service
	services.InitFileWatcher()

	// Start UDP Discovery Listener
	startUDPDiscoveryListener()

	// Start Notification Broadcaster and connect to services package
	notificationBroadcaster.Start()
	services.SetNotificationBroadcaster(notificationBroadcaster)

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

	// Setup WebSocket Notifications Endpoint
	app.Get("/api/notifications/ws", websocket.New(handleWebSocketNotifications, websocketConfig))

	// Setup Routes
	routes.SetupSetup(app)

	// Serve React/Svelte SPA via embed
	app.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(embeddedFiles),
		PathPrefix: "public",
		Browse:     false,
	}))

	// Fallback to index.html for SPA routing (must be placed AFTER API routes)
	app.Use(func(c *fiber.Ctx) error {
		// Only redirect to index.html for GET requests that don't start with /api
		if c.Method() == fiber.MethodGet && !strings.HasPrefix(c.Path(), "/api/") {
			file, err := embeddedFiles.ReadFile("public/index.html")
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).SendString("index.html not found")
			}
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.Send(file)
		}
		return c.Next()
	})

	// Start Server
	log.Fatal(app.Listen(":3000"))
}
