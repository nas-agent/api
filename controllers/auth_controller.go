package controllers

import (
	"fmt"
	"net/http"
	"os/exec"

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

	// 1) Create Linux user
	cmd1 := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	if err := cmd1.Run(); err != nil {
		c.JSON(500, gin.H{"error": "failed to create linux user: " + err.Error()})
		return
	}

	// 2) Set password
	cmd2 := exec.Command("bash", "-c", fmt.Sprintf("echo '%s:%s' | sudo chpasswd", username, password))
	if err := cmd2.Run(); err != nil {
		c.JSON(500, gin.H{"error": "failed to set password: " + err.Error()})
		return
	}

	// 3) Create Samba user
	cmd3 := exec.Command("bash", "-c", fmt.Sprintf("echo -e \"%s\n%s\" | sudo smbpasswd -a %s", password, password, username))
	if err := cmd3.Run(); err != nil {
		c.JSON(500, gin.H{"error": "failed to create samba user: " + err.Error()})
		return
	}

	// 4) Append Samba config
	addConfig := fmt.Sprintf(`
[%s]
   path = /home/%s
   browseable = yes
   writable = yes
   valid users = %s
   force user = %s
   create mask = 0700
   directory mask = 0700
`, username, username, username, username)

	cmd4 := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/samba/smb.conf", addConfig))
	if err := cmd4.Run(); err != nil {
		c.JSON(500, gin.H{"error": "failed to update samba config: " + err.Error()})
		return
	}

	// 5) Restart Samba
	cmd5 := exec.Command("sudo", "systemctl", "restart", "smbd")
	if err := cmd5.Run(); err != nil {
		c.JSON(500, gin.H{"error": "failed to restart samba: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User created successfully"})
}
