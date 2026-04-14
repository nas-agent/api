package services

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// SambaService handles interaction with the Linux system for file sharing
type SambaService struct {
	ConfigPath string
	HomeBase   string
}

var Samba = &SambaService{
	ConfigPath: "/etc/samba/smb.conf",
	HomeBase:   "/mnt/raid1/homes",
}

// SyncSambaUser creates/updates a Linux system user and a Samba user
func (s *SambaService) SyncSambaUser(username, password string) error {
	// 1. Create Linux User if not exists
	// -M: no home dir (we manage it), -s /usr/sbin/nologin: no shell login
	cmd := exec.Command("sudo", "useradd", "-M", "-s", "/usr/sbin/nologin", username)
	_ = cmd.Run() // Ignore error if user already exists

	// 2. Create/Update Samba Password
	// (echo "pass"; echo "pass") | sudo smbpasswd -s -a user
	input := fmt.Sprintf("%s\n%s\n", password, password)
	smbCmd := exec.Command("sudo", "smbpasswd", "-s", "-a", username)
	smbCmd.Stdin = strings.NewReader(input)
	if err := smbCmd.Run(); err != nil {
		return fmt.Errorf("failed to set samba password: %v", err)
	}

	// 3. Ensure Home Directory exists and has proper permissions
	homePath := fmt.Sprintf("%s/%s", s.HomeBase, username)
	if err := exec.Command("sudo", "mkdir", "-p", homePath).Run(); err != nil {
		return fmt.Errorf("failed to create home directory: %v", err)
	}
	if err := exec.Command("sudo", "chown", fmt.Sprintf("%s:sambashare", username), homePath).Run(); err != nil {
		log.Printf("Warning: failed to chown %s: %v", homePath, err)
	}
	if err := exec.Command("sudo", "chmod", "2770", homePath).Run(); err != nil {
		log.Printf("Warning: failed to chmod %s: %v", homePath, err)
	}

	return nil
}

// RemoveSambaUser deletes the Samba account and Linux user
func (s *SambaService) RemoveSambaUser(username string) error {
	_ = exec.Command("sudo", "smbpasswd", "-x", username).Run()
	_ = exec.Command("sudo", "userdel", username).Run()
	return nil
}

// RegisterShare appends a new share section to smb.conf
func (s *SambaService) RegisterShare(name, path, owner string, isPublic bool) error {
	// Construct the share entry
	comment := "NAS Agent Managed Share"
	if isPublic {
		comment = "NAS Agent Public Share"
	}

	entry := fmt.Sprintf("\n[%s]\n   comment = %s\n   path = %s\n   browseable = yes\n   read only = no\n   guest ok = %s\n",
		name, comment, path, map[bool]string{true: "yes", false: "no"}[isPublic])

	if !isPublic && owner != "" {
		entry += fmt.Sprintf("   valid users = %s\n", owner)
	}

	// Append to smb.conf using tee
	echoCmd := exec.Command("echo", entry)
	teeCmd := exec.Command("sudo", "tee", "-a", s.ConfigPath)

	pipe, err := echoCmd.StdoutPipe()
	if err != nil {
		return err
	}
	teeCmd.Stdin = pipe

	if err := echoCmd.Start(); err != nil {
		return err
	}
	if err := teeCmd.Run(); err != nil {
		return err
	}

	return s.RestartService()
}

// UnregisterShare removes a share section from smb.conf
func (s *SambaService) UnregisterShare(name string) error {
	// 1. Read the current configuration
	cmd := exec.Command("sudo", "cat", s.ConfigPath)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read smb.conf: %v", err)
	}

	// 2. Filter out the target section
	lines := strings.Split(string(out), "\n")
	var newLines []string
	skip := false
	targetSection := fmt.Sprintf("[%s]", name)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect start of a new section
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if trimmed == targetSection {
				skip = true
				continue
			}
			skip = false
		}
		
		if !skip {
			newLines = append(newLines, line)
		}
	}

	// 3. Write back the filtered content
	newContent := strings.Join(newLines, "\n")
	teeCmd := exec.Command("sudo", "tee", s.ConfigPath)
	teeCmd.Stdin = strings.NewReader(newContent)
	if err := teeCmd.Run(); err != nil {
		return fmt.Errorf("failed to write smb.conf: %v", err)
	}

	return s.RestartService()
}

// RestartService reloads the Samba daemon
func (s *SambaService) RestartService() error {
	return exec.Command("sudo", "systemctl", "restart", "smbd").Run()
}
