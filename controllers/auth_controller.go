package controllers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
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
	password := "1234"

	// =========================================================================
	// 0) Get the Admin User (Group Owner)
	// =========================================================================
	// We need to determine which user acts as the "Admin" (e.g., "pi").
	// This user will become the 'Group Owner' of all folders.
	currentUser, err := user.Current()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to determine current user"})
		return
	}
	adminUser := currentUser.Username

	// Fix for running with SUDO:
	// If app runs as root, we want the "pi" user (or whoever ran sudo) to be the group owner.
	if adminUser == "root" {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			adminUser = sudoUser
		} else {
			// Fallback: If no SUDO_USER found, you might want to hardcode "pi"
			// adminUser = "pi"
			fmt.Println("Warning: Running as root. Group owner will be root.")
		}
	}

	baseStoragePath := "/mnt/my_drive"
	userStoragePath := fmt.Sprintf("%s/%s", baseStoragePath, username)

	fmt.Printf("Registering: %s | Admin Group: %s\n", username, adminUser)

	// =========================================================================
	// 1) Create Linux User
	// =========================================================================
	cmdAddUser := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	if out, err := cmdAddUser.CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "already exists") {
			c.JSON(500, gin.H{"error": "Failed to create user: " + string(out)})
			return
		}
	}
	fmt.Println(" -> User created (or already exists).")

	// =========================================================================
	// 2) Create Directory
	// =========================================================================
	cmdMkdir := exec.Command("sudo", "mkdir", "-p", userStoragePath)
	if out, err := cmdMkdir.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to mkdir: " + string(out)})
		return
	}
	fmt.Println(" -> Directory created.")

	// =========================================================================
	// 3) Set Ownership (The "Group Strategy")
	// =========================================================================
	// Owner: New User (so they can access it)
	// Group: Admin User (so YOU can access it)
	// Example: chown -R john:pi /mnt/drive/john
	ownershipSpec := fmt.Sprintf("%s:%s", username, adminUser)
	cmdChown := exec.Command("sudo", "chown", "-R", ownershipSpec, userStoragePath)
	if out, err := cmdChown.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to chown: " + string(out)})
		return
	}
	fmt.Println(" -> Ownership set.")

	// =========================================================================
	// 4) Set Permissions (SetGID + 770)
	// =========================================================================
	// chmod 2770
	// 2 (SetGID bit) -> Forces new files to inherit group 'pi' (Crucial!)
	// 7 (Owner/User) -> R W X (Full Access)
	// 7 (Group/Admin)-> R W X (Full Access)
	// 0 (Others)     -> No Access (Strict Isolation)
	cmdChmod := exec.Command("sudo", "chmod", "2770", userStoragePath)
	if out, err := cmdChmod.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to chmod: " + string(out)})
		return
	}
	fmt.Println(" -> Permissions set (2770).")

	// =========================================================================
	// 5) Set Passwords
	// =========================================================================
	if err := setPasswords(username, password); err != nil {
		c.JSON(500, gin.H{"error": "Failed to set passwords"})
		return
	}
	fmt.Println(" -> Passwords set.")

	// =========================================================================
	// 6) Samba Config
	// =========================================================================
	// 'force group': Ensures that when the user uploads a file, it is saved
	// with the 'pi' group (backup to SetGID).
	// 'create mask 0770': Ensures the Admin Group gets Read/Write permissions.
	newShareConfig := fmt.Sprintf(`
[%s]
   path = %s
   browseable = yes
   writeable = yes
   valid users = %s
   create mask = 0770
   directory mask = 0770
   force group = %s
   public = no
`, username, userStoragePath, username, adminUser)

	cmdConfig := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/samba/smb.conf", newShareConfig))
	if err := cmdConfig.Run(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to update samba config"})
		return
	}
	fmt.Println(" -> Samba config updated.")

	// 7) Restart Samba
	exec.Command("sudo", "systemctl", "restart", "smbd").Run()
	fmt.Println(" -> Samba restarted.")

	c.JSON(http.StatusOK, gin.H{
		"message":     "User created via Group Strategy",
		"path":        userStoragePath,
		"owner":       username,
		"admin_group": adminUser,
	})
}

func setPasswords(username, password string) error {
	// Linux Password
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

	// Samba Password
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
