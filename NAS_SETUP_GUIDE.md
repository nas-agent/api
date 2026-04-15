# NAS Mount Setup Guide

This guide explains how to configure your system for NAS mounting operations through the Go API.

## Overview

The NAS mounting feature in your Go API requires passwordless `sudo` access to the following commands:
- `/bin/mkdir` - To create mount point directories
- `/bin/mount` - To mount NAS devices
- `/bin/umount` - To unmount NAS devices

This document provides two setup options.

---

## Option 1: Automatic Setup (Recommended)

### Using Bash Script

1. **Make the script executable:**
   ```bash
   chmod +x ./setup-nas-sudo.sh
   ```

2. **Run the setup script:**
   ```bash
   sudo ./setup-nas-sudo.sh
   ```

3. **Verify the output:**
   - You should see `✓ Sudoers configuration is valid`
   - You should see `✓ Passwordless sudo is working correctly`

### Using Python Script

1. **Run the setup script:**
   ```bash
   sudo python3 ./setup-nas-sudo.py
   ```

2. **Verify the output:**
   - You should see `✓ Sudoers configuration is valid`
   - You should see `✓ Passwordless sudo is working correctly`

---

## Option 2: Manual Setup

If you prefer to manually configure sudoers:

1. **Open the sudoers configuration editor:**
   ```bash
   sudo visudo -f /etc/sudoers.d/nas-mount
   ```

2. **Add these lines to the file:**
   ```
   # Passwordless sudo for NAS mounting operations
   %sudo ALL=(ALL) NOPASSWD: /bin/mkdir
   %sudo ALL=(ALL) NOPASSWD: /bin/mount
   %sudo ALL=(ALL) NOPASSWD: /bin/umount
   ```

3. **Save and exit** (in nano: `Ctrl+X`, then `Y`, then `Enter`)

4. **Verify the configuration:**
   ```bash
   sudo visudo -c -f /etc/sudoers.d/nas-mount
   ```

---

## Verification

### Test if the configuration is working:

```bash
sudo -n test -w /mnt
echo $?
```

- If output is `0`: ✓ Configuration is working
- If output is `1`: ✗ Configuration needs adjustment

### Monitor API startup messages:

When you start your Go API server, it will check the sudoers configuration:

**Success:**
```
✓ Sudoers configuration verified
```

**Warning (if not configured):**
```
⚠️  WARNING: Passwordless sudo not configured!
To enable NAS mounting, run this command on the server:
  sudo tee -a /etc/sudoers.d/nas-mount << EOF
  %sudo ALL=(ALL) NOPASSWD: /bin/mkdir
  %sudo ALL=(ALL) NOPASSWD: /bin/mount
  %sudo ALL=(ALL) NOPASSWD: /bin/umount
  EOF
```

---

## Troubleshooting

### Error: "Sudoers configuration validation failed"

1. **Check the sudoers file:**
   ```bash
   sudo cat /etc/sudoers.d/nas-mount
   ```

2. **Restore from backup if available:**
   ```bash
   sudo cp /etc/sudoers.d/nas-mount.backup /etc/sudoers.d/nas-mount
   sudo chmod 440 /etc/sudoers.d/nas-mount
   ```

3. **Run the setup script again:**
   ```bash
   sudo ./setup-nas-sudo.sh
   ```

### Error: "Permission denied" during mounting

This usually means the sudoers configuration wasn't applied. Verify with:
```bash
sudo -u your_username sudo -n /bin/mkdir /mnt/test
```

If this fails, re-run the setup script.

### Error: "visudo command not found"

Your system is missing `sudo`. Install it:
```bash
# Ubuntu/Debian
sudo apt-get install sudo

# CentOS/RHEL
sudo yum install sudo

# Alpine
sudo apk add sudo
```

---

## Security Considerations

This configuration allows members of the `sudo` group to run specific commands without password prompts. This is:

- **Safe** - Limited to specific commands (`mkdir`, `mount`, `umount`)
- **Group-restricted** - Only applies to users in the `%sudo` group
- **Necessary** - Required for unattended NAS mounting in the Go API

If you need more restrictive permissions (e.g., only for a specific user), modify the sudoers file:

```bash
# Instead of %sudo (entire group), restrict to specific user:
your_username ALL=(ALL) NOPASSWD: /bin/mkdir, /bin/mount, /bin/umount

# Or restrict to specific mount directories:
your_username ALL=(ALL) NOPASSWD: /bin/mount /dev/sda1 /mnt/*
```

---

## After Setup

1. **Rebuild the Go API** (if you modified the codebase):
   ```bash
   cd d:\senior-project\api
   go build -o api.exe
   ```

2. **Restart the API server:**
   ```bash
   ./api.exe
   ```

3. **Test NAS mounting** from the web-admin UI at `http://192.168.100.192:5174/nas/storage`

The system should now successfully mount NAS devices without permission errors!
