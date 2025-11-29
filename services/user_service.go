package services

import (
	"api/config"
	"api/entity"
	"fmt"
	"io"
	"os/exec"
)

type UserService struct{}

func NewUserService() *UserService {
	return &UserService{}
}

func (s *UserService) CreateUser(username, password, email string, storageLimit, aiLimit int64) (*entity.User, error) {
	// CONFIGURATION: Base path
	// 	baseStoragePath := "/mnt"
	// 	userStoragePath := fmt.Sprintf("%s/%s", baseStoragePath, username)

	// 	fmt.Println("------------------------------------------------")
	// 	fmt.Printf("Registering User (PUBLIC MODE): %s\n", username)

	// 	// 0) Open the "Hallway" (Base Path)
	// 	fmt.Printf("[0/6] Setting base path '%s' to 777 (Public)...\n", baseStoragePath)
	// 	cmdOpenHallway := exec.Command("sudo", "chmod", "777", baseStoragePath)
	// 	if out, err := cmdOpenHallway.CombinedOutput(); err != nil {
	// 		fmt.Printf("Warning: Could not set permissions on base path: %s\n", string(out))
	// 	}

	// 	// 1) Create Linux User
	// 	fmt.Printf("[1/6] Creating Linux user '%s'...\n", username)
	// 	cmdAddUser := exec.Command("sudo", "useradd", "-m", "-s", "/bin/bash", username)
	// 	if out, err := cmdAddUser.CombinedOutput(); err != nil {
	// 		if !strings.Contains(string(out), "already exists") {
	// 			return nil, "", fmt.Errorf("failed to create linux user: %s", string(out))
	// 		}
	// 		fmt.Println(" -> User already exists.")
	// 	}

	// 	// 2) Create the Directory
	// 	fmt.Printf("[2/6] Creating storage directory at: %s\n", userStoragePath)
	// 	cmdMkdir := exec.Command("sudo", "mkdir", "-p", userStoragePath)
	// 	if out, err := cmdMkdir.CombinedOutput(); err != nil {
	// 		return nil, "", fmt.Errorf("failed to create directory: %s", string(out))
	// 	}

	// 	// 3) Set Global Permissions (777)
	// 	fmt.Println("[3/6] Setting PUBLIC permissions (777)...")
	// 	cmdChown := exec.Command("sudo", "chown", "-R", fmt.Sprintf("%s:%s", username, username), userStoragePath)
	// 	if out, err := cmdChown.CombinedOutput(); err != nil {
	// 		return nil, "", fmt.Errorf("failed to set folder ownership: %s", string(out))
	// 	}

	// 	cmdChmod := exec.Command("sudo", "chmod", "777", userStoragePath)
	// 	if out, err := cmdChmod.CombinedOutput(); err != nil {
	// 		return nil, "", fmt.Errorf("failed to chmod: %s", string(out))
	// 	}
	// 	fmt.Println(" -> Permissions set to 777 (Everyone has access).")

	// 	// 4) Set Passwords
	// 	fmt.Println("[4/6] Setting passwords...")
	// 	if err := s.setPasswords(username, password); err != nil {
	// 		return nil, "", fmt.Errorf("failed to set passwords: %v", err)
	// 	}

	// 	// 5) Update Samba Config
	// 	fmt.Println("[5/6] Updating smb.conf...")
	// 	newShareConfig := fmt.Sprintf(`
	// [%s]
	//    path = %s
	//    browseable = yes
	//    writeable = yes
	//    guest ok = yes
	//    public = yes
	//    create mask = 0777
	//    directory mask = 0777
	// `, username, userStoragePath)

	// 	cmdConfig := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo tee -a /etc/samba/smb.conf", newShareConfig))
	// 	if err := cmdConfig.Run(); err != nil {
	// 		return nil, "", fmt.Errorf("failed to update samba config: %v", err)
	// 	}

	// 	// 6) Restart Samba
	// 	fmt.Println("[6/6] Restarting Samba...")
	// 	if err := exec.Command("sudo", "systemctl", "restart", "smbd").Run(); err != nil {
	// 		return nil, "", fmt.Errorf("failed to restart samba: %v", err)
	// 	}

	// 7) Save to Database
	fmt.Println("[7/7] Saving user to database...")
	newUser := entity.User{
		Username: username,
		Password: password, // In a real app, hash this!
		Email:    email,
		Role:     entity.UserRoleUser,
	}

	if err := config.DB.Create(&newUser).Error; err != nil {
		return nil, fmt.Errorf("failed to save user to database: %v", err)
	}

	// 8) Initialize User Usage
	fmt.Println("[8/8] Initializing User Usage...")
	newUsage := entity.UserUsage{
		UserID:       newUser.ID,
		StorageUsed:  0,
		StorageLimit: storageLimit,
		AiUsage:      0,
		AiLimit:      aiLimit,
	}
	if err := config.DB.Create(&newUsage).Error; err != nil {
		// Log error but don't fail the whole process? Or maybe fail?
		// For now, let's log it.
		fmt.Printf("Warning: Failed to create UserUsage: %v\n", err)
	}

	fmt.Println("SUCCESS: User folder created and saved to DB.")
	fmt.Println("------------------------------------------------")

	return &newUser, nil
}

func (s *UserService) GetAllUsers() ([]entity.User, error) {
	var users []entity.User
	if err := config.DB.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *UserService) GetUserByID(id string) (*entity.User, error) {
	var user entity.User
	if err := config.DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) GetUserByUsername(username string) (*entity.User, error) {
	var user entity.User
	if err := config.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) UpdateUser(id string, email, role string) (*entity.User, error) {
	var user entity.User
	if err := config.DB.First(&user, id).Error; err != nil {
		return nil, err
	}

	if email != "" {
		user.Email = email
	}
	if role != "" {
		user.Role = entity.UserRole(role)
	}

	if err := config.DB.Save(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) DeleteUser(id string) error {
	var user entity.User
	if err := config.DB.First(&user, id).Error; err != nil {
		return err
	}

	// 1. Delete Linux User
	cmdDelUser := exec.Command("sudo", "userdel", "-r", user.Username)
	if out, err := cmdDelUser.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Failed to delete linux user: %s\n", string(out))
	}

	// 2. Delete from Samba
	exec.Command("sudo", "smbpasswd", "-x", user.Username).Run()

	// 3. Delete from DB
	if err := config.DB.Delete(&user).Error; err != nil {
		return err
	}

	return nil
}

func (s *UserService) setPasswords(username, password string) error {
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

func (s *UserService) ChangePassword(username, currentPassword, newPassword string) error {
	// 1. Verify current password
	user, err := s.Authenticate(username, currentPassword)
	if err != nil {
		return fmt.Errorf("incorrect current password")
	}

	// 2. Update Linux & Samba passwords
	if err := s.setPasswords(username, newPassword); err != nil {
		return fmt.Errorf("failed to update system passwords: %v", err)
	}

	// 3. Update Database password
	user.Password = newPassword // In real app, hash this!
	if err := config.DB.Save(user).Error; err != nil {
		return fmt.Errorf("failed to update database password: %v", err)
	}

	return nil
}

func (s *UserService) Authenticate(username, password string) (*entity.User, error) {
	var user entity.User
	if err := config.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found")
	}

	// In a real app, compare hashed passwords!
	if user.Password != password {
		return nil, fmt.Errorf("invalid password")
	}

	return &user, nil
}
