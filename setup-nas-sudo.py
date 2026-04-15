#!/usr/bin/env python3
"""
NAS Mount Sudo Setup Script
Configures passwordless sudo for NAS mounting operations

Usage: sudo python3 setup-nas-sudo.py
"""

import os
import sys
import subprocess
import shutil
from pathlib import Path

# Colors for output
class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    NC = '\033[0m'  # No Color

def print_header():
    print(f"{Colors.YELLOW}=== NAS Mount Sudo Configuration Setup ==={Colors.NC}")
    print()

def print_success(msg):
    print(f"{Colors.GREEN}✓ {msg}{Colors.NC}")

def print_warning(msg):
    print(f"{Colors.YELLOW}⚠ {msg}{Colors.NC}")

def print_error(msg):
    print(f"{Colors.RED}✗ {msg}{Colors.NC}")

def check_root():
    """Check if running as root"""
    if os.geteuid() != 0:
        print_error("This script must be run with sudo")
        print("Usage: sudo python3 setup-nas-sudo.py")
        sys.exit(1)

def create_sudoers_dir():
    """Create /etc/sudoers.d if it doesn't exist"""
    sudoers_d = Path('/etc/sudoers.d')
    if not sudoers_d.exists():
        print_warning("Creating /etc/sudoers.d directory...")
        sudoers_d.mkdir(parents=True, mode=0o755)
    return sudoers_d

def backup_existing(sudoers_file):
    """Backup existing sudoers file"""
    if sudoers_file.exists():
        print_warning(f"Backing up existing {sudoers_file}...")
        backup_file = Path(f"{sudoers_file}.backup")
        shutil.copy2(sudoers_file, backup_file)
        print(f"  Backup saved to: {backup_file}")
        return backup_file
    return None

def write_sudoers_config(sudoers_file):
    """Write the NAS mount sudoers configuration"""
    print_warning("Writing sudoers configuration...")
    
    config_content = """# Passwordless sudo for NAS mounting operations
# This allows the Go API server to run mount/mkdir/umount without password prompts

%sudo ALL=(ALL) NOPASSWD: /bin/mkdir
%sudo ALL=(ALL) NOPASSWD: /bin/mount
%sudo ALL=(ALL) NOPASSWD: /bin/umount
"""
    
    sudoers_file.write_text(config_content)
    sudoers_file.chmod(0o440)

def validate_sudoers(sudoers_file):
    """Validate sudoers configuration"""
    print_warning("Validating sudoers configuration...")
    
    try:
        result = subprocess.run(
            ['visudo', '-c', '-f', str(sudoers_file)],
            capture_output=True,
            text=True
        )
        if result.returncode == 0:
            print_success("Sudoers configuration is valid")
            return True
        else:
            print_error("Sudoers configuration validation failed!")
            print(result.stderr)
            return False
    except FileNotFoundError:
        print_error("visudo command not found")
        return False

def test_sudo():
    """Test if passwordless sudo is working"""
    print_warning("Testing sudo configuration...")
    
    try:
        result = subprocess.run(
            ['sudo', '-n', 'test', '-w', '/mnt'],
            capture_output=True,
            timeout=5
        )
        if result.returncode == 0:
            print_success("Passwordless sudo is working correctly")
            return True
        else:
            print_warning("Note: Full sudo test may require appropriate permissions")
            return False
    except Exception as e:
        print_warning(f"Could not fully test sudo: {e}")
        return False

def main():
    print_header()
    
    # Check if running as root
    check_root()
    
    # Create sudoers.d directory
    sudoers_d = create_sudoers_dir()
    sudoers_file = sudoers_d / 'nas-mount'
    
    # Backup existing file
    backup_file = backup_existing(sudoers_file)
    
    try:
        # Write new configuration
        write_sudoers_config(sudoers_file)
        
        # Validate configuration
        if not validate_sudoers(sudoers_file):
            # Restore backup on validation failure
            print_warning("Restoring backup due to validation failure...")
            if backup_file and backup_file.exists():
                shutil.copy2(backup_file, sudoers_file)
                sudoers_file.chmod(0o440)
                print_warning("Backup restored.")
            sys.exit(1)
        
        # Test sudo
        test_sudo()
        
        # Success message
        print()
        print(f"{Colors.GREEN}=== Setup Complete ==={Colors.NC}")
        print()
        print("The following commands are now available without password prompts:")
        print("  • sudo /bin/mkdir")
        print("  • sudo /bin/mount")
        print("  • sudo /bin/umount")
        print()
        print("Your Go API can now mount NAS devices without password prompts.")
        print()
        
    except Exception as e:
        print_error(f"An error occurred: {e}")
        if backup_file and backup_file.exists():
            print_warning("Restoring backup...")
            shutil.copy2(backup_file, sudoers_file)
            sudoers_file.chmod(0o440)
        sys.exit(1)

if __name__ == '__main__':
    main()
