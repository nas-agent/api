#!/usr/bin/env bash
#
# One-shot bootstrap for Go API host (Debian/Raspberry Pi OS)
#
# Usage:
#   sudo ./bootstrap-api.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"${SCRIPT_DIR}/setup-api-deps.sh"
"${SCRIPT_DIR}/setup-nas-sudo.sh"

echo "Bootstrap finished: dependencies + sudoers are configured."
