package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// Simple test UDP listener to verify discovery broadcasts
// Run this on Raspberry Pi: go run test-udp-discovery.go
func main() {
	fmt.Println("🟢 UDP Discovery Test Listener")
	fmt.Println("================================")

	addr := net.UDPAddr{
		Port: 9999,
		IP:   net.ParseIP("0.0.0.0"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		fmt.Printf("❌ Failed to listen: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Println("✅ Listening on port 9999...")
	fmt.Println("Waiting for WHO_IS_NAS_API? broadcasts...")
	fmt.Println("\nPress Ctrl+C to exit\n")

	// Set up signal handler for graceful exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nShutting down...")
		conn.Close()
		os.Exit(0)
	}()

	buffer := make([]byte, 1024)
	count := 0

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("❌ Read error: %v\n", err)
			continue
		}

		message := string(buffer[:n])
		count++

		fmt.Printf("\n[%d] 📨 Received from %s\n", count, remoteAddr.String())
		fmt.Printf("    Message: %q\n", message)

		if message == "WHO_IS_NAS_API?" {
			fmt.Println("    ✅ Valid discovery request!")

			// Send response
			response := "API_HERE:3000"
			_, err := conn.WriteToUDP([]byte(response), remoteAddr)
			if err != nil {
				fmt.Printf("    ❌ Failed to send response: %v\n", err)
			} else {
				fmt.Printf("    ✓ Responded with: %q\n", response)
			}
		} else {
			fmt.Printf("    ⚠️  Unknown message format\n")
		}
	}
}
