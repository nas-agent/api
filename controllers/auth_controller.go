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
	password := input.Password

	// CONFIGURATION: Change this to your actual external drive mount point
	baseStoragePath := "/mnt/my_drive"
	userStoragePath := fmt.Sprintf("%s/%s", baseStoragePath, username)

	// 1) Create Linux user (System User)
	// We still use -m to give them a small /home/user for config files,
	// even if their data is elsewhere.
	cmdAddUser := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	if out, err := cmdAddUser.CombinedOutput(); err != nil {
		// Ignore "already exists" error for idempotency, or handle as needed
		if !strings.Contains(string(out), "already exists") {
			c.JSON(500, gin.H{"error": "Failed to create linux user: " + string(out)})
			return
		}
	}

	// 2) Create the Specific Storage Directory
	// mkdir -p /mnt/my_drive/faan
	cmdMkdir := exec.Command("sudo", "mkdir", "-p", userStoragePath)
	if out, err := cmdMkdir.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to create directory: " + string(out)})
		return
	}

	// 3) Change Ownership (Critical Step)
	// If we don't do this, root owns the folder and 'faan' cannot write to it.
	// chown faan:faan /mnt/my_drive/faan
	cmdChmod := exec.Command("sudo", "chmod", "700", userStoragePath)
	if out, err := cmdChmod.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to chmod: " + string(out)})
		return
	}

	cmdChown := exec.Command("sudo", "chown", "-R", fmt.Sprintf("%s:%s", username, username), userStoragePath)
	if out, err := cmdChown.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to set folder ownership: " + string(out)})
		return
	}

	// 4) Set Linux & Samba Passwords (using pipes for security)
	setPasswords(username, password) // (I moved the pipe logic to a helper for cleanliness)

	// 5) Append Config pointing to the NEW path
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
		c.JSON(500, gin.H{"error": "Failed to update samba config"})
		return
	}

	// 6) Restart Samba
	exec.Command("sudo", "systemctl", "restart", "smbd").Run()

	c.JSON(http.StatusOK, gin.H{
		"message": "User created",
		"path":    userStoragePath,
	})
}

// Helper to keep main logic clean
func setPasswords(username, password string) {
	// Set Linux Pass
	cmdLinux := exec.Command("sudo", "chpasswd")
	stdinLinux, _ := cmdLinux.StdinPipe()
	cmdLinux.Start()
	io.WriteString(stdinLinux, fmt.Sprintf("%s:%s", username, password))
	stdinLinux.Close()
	cmdLinux.Wait()

	// Set Samba Pass
	cmdSmb := exec.Command("sudo", "smbpasswd", "-a", "-s", username)
	stdinSmb, _ := cmdSmb.StdinPipe()
	cmdSmb.Start()
	io.WriteString(stdinSmb, fmt.Sprintf("%s\n%s\n", password, password))
	stdinSmb.Close()
	cmdSmb.Wait()
}
