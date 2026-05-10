# NAS Infrastructure \u0026 OS-Level Technical Report

This document outlines the low-level operating system integration, protocol management, and infrastructure orchestration implemented in the NAS Agent backend (`nas_controller.go`, `samba_controller.go`, `network_controller.go`). It details how the Go backend interfaces directly with Linux kernel subsystems and storage protocols to create a fully functional, headless Network Attached Storage appliance.

---

## 1. Storage \u0026 Hardware Integration

The NAS Agent acts as an orchestration layer over traditional Linux storage commands, providing a safe, automated interface for disk management without requiring users to use the CLI.

### 1.1 Device Discovery (\`lsblk\` \u0026 \`/proc\`)
*   **Block Device Polling**: The system continuously polls hardware states using `lsblk --json -o NAME,KNAME,LABEL,MOUNTPOINT,SIZE,FSTYPE...`. 
*   **Mount Tracking**: It cross-references `lsblk` output with `/proc/mounts` to dynamically detect logically mounted volumes, bind mounts, and active file systems, safely filtering out kernel filesystems (e.g., `tmpfs`, `sysfs`, `cgroup`, `overlay`).
*   **System Protection (Guardrail)**: Before any destructive action, `isSystemDisk()` validates the target against `df /` and critical system mount paths (`/boot`, `/usr`, `/var`, `/etc`). This prevents the system from accidentally wiping the OS drive.

### 1.2 "The Nuclear Cleanup" \u0026 Disk Formatting
When a user requests a disk format (`FormatAndMount`), the system executes a deeply defensive **Nuclear Cleanup** (`hardenDeviceCleanup`) to ensure no residual RAID signatures, LVM headers, or kernel locks prevent formatting:
1.  **Swap Disable**: Executes `swapoff` on the device and its partitions.
2.  **RAID Superblock Wiping**: Executes `mdadm --zero-superblock --force` to break any orphaned software RAID arrays holding the disk hostage.
3.  **Partition Table Zapping**: Uses `sgdisk --zap-all` to destroy MBR/GPT structures, followed by `parted -s <dev> mklabel gpt` to enforce a clean GPT partition layout.
4.  **Header Obliteration**: Uses `dd if=/dev/zero bs=1M count=50` to physically overwrite the first 50MB of the disk, followed by `wipefs -a -f`.
5.  **Kernel Refresh**: Forces the kernel to recognize the clean state using `udevadm settle`, `partprobe`, and `sync`.
6.  **Formatting**: Executes the appropriate `mkfs` command (e.g., `mkfs.ext4`, `mkfs.ntfs`, `mkfs.vfat -I`).

### 1.3 Persistent Mounting (\`/etc/fstab\`)
To ensure disks survive a reboot:
*   The Go backend queries the new file system's UUID via `blkid -s UUID -o value`.
*   It securely edits `/etc/fstab` using `sed` to remove old conflicting entries, then injects the new mount.
*   **Resilience**: The injected `fstab` entry uses the `nofail,x-systemd.device-timeout=5` flags. This ensures that if an external USB drive is unplugged while the Raspberry Pi reboots, it will not drop into Emergency Maintenance mode and brick the headless server.

---

## 2. File Sharing \u0026 Protocol Layer

The system acts as a controller for the Samba (`smbd`) daemon, automatically generating configuration entries and Linux system permissions to expose files over the SMB protocol.

### 2.1 Protocol Integration (SMB)
*   **Samba Controller**: Exposes the physical mounts to Windows/macOS clients dynamically. It restarts/reloads the `smbd` daemon via `systemctl` when shares are added or removed.
*   **Diagnostic Validation**: Automatically verifies system health by running `testparm -s` to validate the generated `smb.conf` syntax, checking if `systemctl is-active smbd` is running, and executing `test -w` (via `sudo`) to ensure write accessibility.

### 2.2 Dynamic Permissions \u0026 ACLs
When a share is created, the Go backend natively modifies Linux user groups and permissions:
*   **Global Share Group**: Ensures the `sambashare` group exists (`groupadd -f sambashare`).
*   **Private Shares**: 
    *   Adds the specific user to the `sambashare` group (`usermod -a -G`).
    *   Executes `chown -R <username>:sambashare`.
    *   Enforces a strict mask with `chmod -R 2770` (Group sticky bit + User/Group RWX + No access for others).
*   **Public Shares**: 
    *   Executes `chown -R nobody:sambashare`.
    *   Enforces `chmod -R 2775` (Group sticky bit + User/Group RWX + RX for others), allowing open read/write access.

---

## 3. Network \u0026 Security Layer

The network controller interacts with the underlying OS network stack and system services to provide remote security management.

### 3.1 Network Monitoring
*   **Interface Discovery**: Uses Go's native `net.Interfaces()` to list MAC addresses and IP blocks, filtering out loopbacks.
*   **Link Speeds**: Parses the `/sys/class/net/<iface>/speed` file directly from the Linux virtual file system to report physical hardware connection speeds (e.g., 1000 Mbps Ethernet).

### 3.2 System Hardening
*   **Daemon Control**: Users can securely toggle OS-level services (like SSH) from the web UI. The backend translates this into `systemctl start/stop ssh` commands.
*   **Firewall Orchestration**: Tracks required NAS ports (TCP 80/443 for Web, TCP 22 for SSH, TCP 445 for SMB) and is architected to interface with `iptables` / `ufw` to dynamically adjust rules.

---

## Summary
The NAS infrastructure layer abstracts complex, potentially destructive Linux administration commands behind a safe, RESTful API. By directly interacting with the kernel (`/sys`, `/proc`, `udev`), storage abstractions (`lsblk`, `mdadm`, `parted`), and network daemons (`smbd`), the Go backend transforms generic hardware (like a Raspberry Pi) into a fully managed, resilient storage appliance.
