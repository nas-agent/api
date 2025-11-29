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
	password := "123456" // Hardcoded for testing

	// CONFIGURATION: Base path
	baseStoragePath := "/mnt"
	userStoragePath := fmt.Sprintf("%s/%s", baseStoragePath, username)

	fmt.Println("------------------------------------------------")
	fmt.Printf("Registering User (PUBLIC MODE): %s\n", username)

	// =========================================================================
	// 0) Open the "Hallway" (Base Path)
	// =========================================================================
	// chmod 777: Everyone can Read, Write, and Execute (Traverse)
	fmt.Printf("[0/6] Setting base path '%s' to 777 (Public)...\n", baseStoragePath)
	cmdOpenHallway := exec.Command("sudo", "chmod", "777", baseStoragePath)
	if out, err := cmdOpenHallway.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Could not set permissions on base path: %s\n", string(out))
	}

	// =========================================================================
	// 1) Create Linux User
	// =========================================================================
	fmt.Printf("[1/6] Creating Linux user '%s'...\n", username)
	cmdAddUser := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	if out, err := cmdAddUser.CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "already exists") {
			c.JSON(500, gin.H{"error": "Failed to create linux user: " + string(out)})
			return
		}
		fmt.Println(" -> User already exists.")
	}

	// =========================================================================
	// 2) Create the Directory
	// =========================================================================
	fmt.Printf("[2/6] Creating storage directory at: %s\n", userStoragePath)
	cmdMkdir := exec.Command("sudo", "mkdir", "-p", userStoragePath)
	if out, err := cmdMkdir.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to create directory: " + string(out)})
		return
	}

	// =========================================================================
	// 3) Set Global Permissions (777)
	// =========================================================================
	fmt.Println("[3/6] Setting PUBLIC permissions (777)...")

	// Change Owner: We still set the user as owner for file tracking,
	// but permissions will allow everyone else to touch it too.
	cmdChown := exec.Command("sudo", "chown", "-R", fmt.Sprintf("%s:%s", username, username), userStoragePath)
	if out, err := cmdChown.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to set folder ownership: " + string(out)})
		return
	}

	// chmod 777:
	// Owner: RWX (Full)
	// Group: RWX (Full)
	// Others: RWX (Full) <-- This allows Admin, Bob, Alice, Everyone to access.
	cmdChmod := exec.Command("sudo", "chmod", "777", userStoragePath)
	if out, err := cmdChmod.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to chmod: " + string(out)})
		return
	}
	fmt.Println(" -> Permissions set to 777 (Everyone has access).")

	// =========================================================================
	// 4) Set Passwords
	// =========================================================================
	fmt.Println("[4/6] Setting passwords...")
	if err := setPasswords(username, password); err != nil {
		c.JSON(500, gin.H{"error": "Failed to set passwords"})
		return
	}

	// =========================================================================
	// 5) Update Samba Config (Open Access)
	// =========================================================================
	fmt.Println("[5/6] Updating smb.conf...")
	// We REMOVED 'valid users'. Now anyone can connect.
	// We ADDED 'guest ok = yes'.
	// We set masks to 0777 so new files are created as public.
	newShareConfig := fmt.Sprintf(`
[%s]
   path = %s
   browseable = yes
   writeable = yes
   guest ok = yes
   public = yes
   create mask = 0777
   directory mask = 0777
`, username, userStoragePath)

	cmdConfig := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/samba/smb.conf", newShareConfig))
	if err := cmdConfig.Run(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to update samba config"})
		return
	}

	// =========================================================================
	// 6) Restart Samba
	// =========================================================================
	fmt.Println("[6/6] Restarting Samba...")
	if err := exec.Command("sudo", "systemctl", "restart", "smbd").Run(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to restart samba"})
		return
	}

	fmt.Println("SUCCESS: User folder created (Accessible by ALL).")
	fmt.Println("------------------------------------------------")

	c.JSON(http.StatusOK, gin.H{
		"message": "User created in Public Mode",
		"path":    userStoragePath,
	})
}

func setPasswords(username, password string) error {
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

	cmdSmb := exec.Command("sudo", "smbpasswd", "-a", "-s", username)
	stdinSmb, err := cmdSmb.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmdSmb.Start(); err != nil {
		return err
	}
	io.WriteString(stdinSmb, fmt.Sprintf("%s\n%s\n", password, password))
	stdinSmb.Close()
	if err := cmdSmb.Wait(); err != nil {
		return err
	}
	return nil
}
