#!/usr/bin/env bash
#
# Go API Linux dependency bootstrap (Debian/Raspberry Pi OS)
# - Checks required commands used by this API
# - Installs missing apt packages
#
# Usage:
#   sudo ./setup-api-deps.sh
#

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

if [[ "${EUID}" -ne 0 ]]; then
  echo -e "${RED}Error: run as root (sudo ./setup-api-deps.sh)${NC}"
  exit 1
fi

echo -e "${YELLOW}=== Go API Dependency Check (Debian/RPi) ===${NC}"

if ! command -v apt-get >/dev/null 2>&1; then
  echo -e "${RED}Error: apt-get not found. This script supports Debian/Raspberry Pi OS.${NC}"
  exit 1
fi

declare -A CMD_TO_PKG=(
  [lsblk]=util-linux
  [mount]=util-linux
  [umount]=util-linux
  [blkid]=util-linux
  [wipefs]=util-linux
  [blockdev]=util-linux
  [fuser]=psmisc
  [mdadm]=mdadm
  [parted]=parted
  [partprobe]=parted
  [fdisk]=fdisk
  [dmsetup]=dmsetup
  [mkfs.ext4]=e2fsprogs
  [mkfs.ext3]=e2fsprogs
  [mkfs.vfat]=dosfstools
  [mkfs.ntfs]=ntfs-3g
  [udevadm]=udev
  [update-initramfs]=initramfs-tools
  [smbd]=samba
  [smbpasswd]=samba-common-bin
  [testparm]=samba-common-bin
  [useradd]=passwd
  [usermod]=passwd
  [userdel]=passwd
  [groupadd]=passwd
  [python3]=python3
)

# Feature helpers for common NAS protocols.
# mount.cifs and mount.nfs are helper binaries (often in /sbin).
FEATURE_CHECKS=(
  "mount.cifs:cifs-utils"
  "mount.nfs:nfs-common"
)

missing_packages=()
record_missing_pkg() {
  local pkg="$1"
  for existing in "${missing_packages[@]:-}"; do
    [[ "${existing}" == "${pkg}" ]] && return 0
  done
  missing_packages+=("${pkg}")
}

echo -e "${YELLOW}Scanning command dependencies...${NC}"
for cmd in "${!CMD_TO_PKG[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    record_missing_pkg "${CMD_TO_PKG[$cmd]}"
    echo "  - Missing command '${cmd}' (package: ${CMD_TO_PKG[$cmd]})"
  fi
done

echo -e "${YELLOW}Scanning NAS protocol helpers...${NC}"
for item in "${FEATURE_CHECKS[@]}"; do
  helper="${item%%:*}"
  pkg="${item##*:}"
  if ! command -v "${helper}" >/dev/null 2>&1; then
    record_missing_pkg "${pkg}"
    echo "  - Missing helper '${helper}' (package: ${pkg})"
  fi
done

if [[ "${#missing_packages[@]}" -eq 0 ]]; then
  echo -e "${GREEN}All required packages are already installed.${NC}"
  exit 0
fi

echo -e "${YELLOW}Installing missing packages:${NC} ${missing_packages[*]}"
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends "${missing_packages[@]}"

echo -e "${YELLOW}Verifying required commands...${NC}"
verify_failed=0
for cmd in "${!CMD_TO_PKG[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo -e "${RED}  - Still missing: ${cmd}${NC}"
    verify_failed=1
  fi
done
for item in "${FEATURE_CHECKS[@]}"; do
  helper="${item%%:*}"
  if ! command -v "${helper}" >/dev/null 2>&1; then
    echo -e "${RED}  - Still missing helper: ${helper}${NC}"
    verify_failed=1
  fi
done

if [[ "${verify_failed}" -ne 0 ]]; then
  echo -e "${RED}Dependency bootstrap completed with missing tools.${NC}"
  exit 1
fi

echo -e "${GREEN}Dependency bootstrap complete. All required tools are available.${NC}"
