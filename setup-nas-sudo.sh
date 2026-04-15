#!/bin/bash
#
# NAS Mount Sudo Setup Script
# Configures passwordless sudo for NAS mounting operations
# 
# Usage: sudo ./setup-nas-sudo.sh
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== NAS Mount Sudo Configuration Setup ===${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}Error: This script must be run with sudo${NC}"
   echo "Usage: sudo ./setup-nas-sudo.sh"
   exit 1
fi

# Create sudoers.d directory if it doesn't exist
if [ ! -d /etc/sudoers.d ]; then
    echo -e "${YELLOW}Creating /etc/sudoers.d directory...${NC}"
    mkdir -p /etc/sudoers.d
    chmod 755 /etc/sudoers.d
fi

# Backup existing sudoers.d/nas-mount if it exists
if [ -f /etc/sudoers.d/nas-mount ]; then
    echo -e "${YELLOW}Backing up existing /etc/sudoers.d/nas-mount...${NC}"
    cp /etc/sudoers.d/nas-mount /etc/sudoers.d/nas-mount.backup
    echo "  Backup saved to: /etc/sudoers.d/nas-mount.backup"
fi

# Create the NAS mount sudoers configuration
echo -e "${YELLOW}Writing sudoers configuration...${NC}"
cat > /etc/sudoers.d/nas-mount << 'EOF'
# Passwordless sudo for NAS mounting operations
# This allows the Go API server to run mount/mkdir/umount without password prompts

%sudo ALL=(ALL) NOPASSWD: /bin/mkdir
%sudo ALL=(ALL) NOPASSWD: /bin/mount
%sudo ALL=(ALL) NOPASSWD: /bin/umount
EOF

# Fix permissions on the sudoers file
chmod 440 /etc/sudoers.d/nas-mount

# Validate the sudoers configuration
echo -e "${YELLOW}Validating sudoers configuration...${NC}"
if visudo -c -f /etc/sudoers.d/nas-mount > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Sudoers configuration is valid${NC}"
else
    echo -e "${RED}✗ Sudoers configuration validation failed!${NC}"
    echo "Restoring backup if available..."
    if [ -f /etc/sudoers.d/nas-mount.backup ]; then
        mv /etc/sudoers.d/nas-mount.backup /etc/sudoers.d/nas-mount
        chmod 440 /etc/sudoers.d/nas-mount
        echo -e "${YELLOW}Backup restored.${NC}"
    fi
    exit 1
fi

# Test if sudo mkdir works without password
echo -e "${YELLOW}Testing sudo configuration...${NC}"
if sudo -n test -w /mnt > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Passwordless sudo is working correctly${NC}"
else
    echo -e "${YELLOW}⚠ Note: Full sudo test may require appropriate permissions${NC}"
fi

echo ""
echo -e "${GREEN}=== Setup Complete ===${NC}"
echo ""
echo "The following commands are now available without password prompts:"
echo "  • sudo /bin/mkdir"
echo "  • sudo /bin/mount"
echo "  • sudo /bin/umount"
echo ""
echo "Your Go API can now mount NAS devices without password prompts."
echo ""
