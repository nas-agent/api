# Linux Package Audit for Go API (RPi4/Debian)

This list is derived from command usage in Go API source (controllers/services/main setup scripts).

## Required Packages

- util-linux
  - Commands used: lsblk, mount, umount, blkid, wipefs, blockdev
- psmisc
  - Commands used: fuser
- mdadm
  - Commands used: mdadm --create/--stop/--query/--examine
- parted
  - Commands used: parted, partprobe
- fdisk
  - Commands used: fdisk -l
- dmsetup
  - Commands used: dmsetup remove
- e2fsprogs
  - Commands used: mkfs.ext3, mkfs.ext4
- dosfstools
  - Commands used: mkfs.vfat
- ntfs-3g
  - Commands used: mkfs.ntfs
- udev
  - Commands used: udevadm settle
- initramfs-tools
  - Commands used: update-initramfs -u
- samba
  - Commands/services used: smbd
- samba-common-bin
  - Commands used: smbpasswd, testparm
- passwd
  - Commands used: useradd, usermod, userdel, groupadd
- python3
  - Needed by setup fallback path in api/main.go (setup-nas-sudo.py)

## NAS Protocol Helper Packages (Recommended)

- cifs-utils
  - Helper: mount.cifs
- nfs-common
  - Helper: mount.nfs

## Notes

- The bootstrap script `setup-api-deps.sh` installs only packages that are missing.
- This audit focuses on Linux host/runtime dependencies for the Go API process on Raspberry Pi OS (Debian-based).
- Go toolchain installation is intentionally out of scope here.
