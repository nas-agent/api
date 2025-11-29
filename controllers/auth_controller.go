package controllers

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
)

type RegisterInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

func RegisterUser(c *gin.Context) {
	var input RegisterInput
	if err := c.BindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	username := input.Username
	password := "123456"

	// CONFIGURATION: Change this to your actual external drive mount point
	baseStoragePath := "/mnt"
	userStoragePath := fmt.Sprintf("%s/%s", baseStoragePath, username)

	fmt.Println("------------------------------------------------")
	fmt.Printf("Received request to register user: %s\n", username)

	// 1) Create Linux user (System User)
	fmt.Printf("[1/6] Creating Linux user '%s'...\n", username)
	cmdAddUser := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	if out, err := cmdAddUser.CombinedOutput(); err != nil {
		// Ignore "already exists" error
		if !strings.Contains(string(out), "already exists") {
			fmt.Printf("Error creating user: %s\n", string(out))
			c.JSON(500, gin.H{"error": "Failed to create linux user: " + string(out)})
			return
		}
		fmt.Println(" -> User already exists (skipping creation).")
	} else {
		fmt.Println(" -> User created successfully.")
	}

	// 2) Create the Specific Storage Directory
	fmt.Printf("[2/6] Creating storage directory at: %s\n", userStoragePath)
	cmdMkdir := exec.Command("sudo", "mkdir", "-p", userStoragePath)
	if out, err := cmdMkdir.CombinedOutput(); err != nil {
		fmt.Printf("Error mkdir: %s\n", string(out))
		c.JSON(500, gin.H{"error": "Failed to create directory: " + string(out)})
		return
	}
	fmt.Println(" -> Directory created.")

	// 3) Change Ownership & Permissions
	fmt.Println("[3/6] Setting permissions (750) and ownership...")

	// chmod 750 (owner: rwx, group: r-x, others: none)
	cmdChmod := exec.Command("sudo", "chmod", "750", userStoragePath)
	if out, err := cmdChmod.CombinedOutput(); err != nil {
		fmt.Printf("Error chmod: %s\n", string(out))
		c.JSON(500, gin.H{"error": "Failed to chmod: " + string(out)})
		return
	}

	// chown user:user
	cmdChown := exec.Command("sudo", "chown", "-R", fmt.Sprintf("%s:%s", username, username), userStoragePath)
	if out, err := cmdChown.CombinedOutput(); err != nil {
		fmt.Printf("Error chown: %s\n", string(out))
		c.JSON(500, gin.H{"error": "Failed to set folder ownership: " + string(out)})
		return
	}

	// Get the admin username
	fmt.Println(" -> Getting admin username and adding to user group...")
	cmdWhoami := exec.Command("whoami")
	adminUser, err := cmdWhoami.Output()
	if err != nil {
		fmt.Printf("Warning: Could not get admin username: %v\n", err)
	} else {
		adminUsername := strings.TrimSpace(string(adminUser))
		fmt.Printf(" -> Admin user detected: %s\n", adminUsername)

		// Add admin user to the user's group
		cmdUsermod := exec.Command("sudo", "usermod", "-a", "-G", username, adminUsername)
		if out, err := cmdUsermod.CombinedOutput(); err != nil {
			fmt.Printf("Warning: Could not add admin to group: %s\n", string(out))
		} else {
			fmt.Printf(" -> Admin user '%s' added to group '%s'\n", adminUsername, username)
		}
	}

	fmt.Println(" -> Permissions and Ownership secured.")

	// 4) Set Linux & Samba Passwords
	fmt.Println("[4/6] Setting System and Samba passwords...")
	if err := setPasswords(username, password); err != nil {
		fmt.Printf("Error setting passwords: %v\n", err)
		c.JSON(500, gin.H{"error": "Failed to set passwords"})
		return
	}
	fmt.Println(" -> Passwords set.")

	// 5) Append Config pointing to the NEW path
	fmt.Println("[5/6] Updating smb.conf...")
	newShareConfig := fmt.Sprintf(`
[%s]
   path = %s
   browseable = yes
   writeable = yes
   valid users = %s
   create mask = 0700
   directory mask = 0700
   public = no
`, username, userStoragePath, username)

	// Write config
	cmdConfig := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/samba/smb.conf", newShareConfig))
	if err := cmdConfig.Run(); err != nil {
		fmt.Println("Error updating smb.conf")
		c.JSON(500, gin.H{"error": "Failed to update samba config"})
		return
	}
	fmt.Println(" -> Config updated.")

	// 6) Restart Samba
	fmt.Println("[6/6] Restarting Samba service...")
	if err := exec.Command("sudo", "systemctl", "restart", "smbd").Run(); err != nil {
		fmt.Println("Error restarting smbd service")
		c.JSON(500, gin.H{"error": "Failed to restart samba"})
		return
	}
	fmt.Println(" -> Samba restarted.")
	fmt.Println("SUCCESS: User registration complete.")
	fmt.Println("------------------------------------------------")

	c.JSON(http.StatusOK, gin.H{
		"message": "User created",
		"path":    userStoragePath,
	})
}

// Helper to keep main logic clean
// Now returns error to help with logging
func setPasswords(username, password string) error {
	// Set Linux Pass
	cmdLinux := exec.Command("sudo", "chpasswd")
	stdinLinux, err := cmdLinux.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmdLinux.Start(); err != nil {
		return err
	}
	io.WriteString(stdinLinux, fmt.Sprintf("%s:%s", username, password))
	stdinLinux.Close()
	if err := cmdLinux.Wait(); err != nil {
		return err
	}

	// Set Samba Pass
	cmdSmb := exec.Command("sudo", "smbpasswd", "-a", "-s", username)
	stdinSmb, err := cmdSmb.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmdSmb.Start(); err != nil {
		return err
	}
	// smbpasswd expects password\npassword\n
	io.WriteString(stdinSmb, fmt.Sprintf("%s\n%s\n", password, password))
	stdinSmb.Close()
	if err := cmdSmb.Wait(); err != nil {
		return err
	}
	return nil
}
