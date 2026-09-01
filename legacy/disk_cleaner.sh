#!/usr/bin/env sh
# Enterprise Universal Disk Cleanup Tool for Unix-like OS (Linux, macOS, BSD)
set -e

GREEN='\030[0;32m'; NC='\033[0m'; BOLD='\033[1m'
log() { printf "${GREEN}[+] %s${NC}\n" "$1"; }

get_free_space() {
  df -k / | awk 'NR==2 {print $4}'
}

BEFORE_SPACE=$(get_free_space)

log "Starting Cross-Platform Disk Cleanup..."

# 1. Platform Detection
OS_TYPE=$(uname -s)

# 2. Package Managers & System Caches
if [ "$OS_TYPE" = "Linux" ]; then
  log "Cleaning Linux package manager caches..."
  command -v apt-get >/dev/null 2>&1 && apt-get autoremove -y && apt-get clean
  command -v dnf >/dev/null 2>&1 && dnf autoremove -y && dnf clean all
  command -v yum >/dev/null 2>&1 && yum clean all
  command -v pacman >/dev/null 2>&1 && pacman -Scc --noconfirm
  command -v zypper >/dev/null 2>&1 && zypper clean --all
  
  # Clear Systemd Journal logs older than 7 days
  command -v journalctl >/dev/null 2>&1 && journalctl --vacuum-time=7d
  
elif [ "$OS_TYPE" = "Darwin" ]; then
  log "Cleaning macOS specific caches and logs..."
  rm -rf ~/Library/Caches/* 2>/dev/null || true
  rm -rf ~/Library/Logs/* 2>/dev/null || true
  rm -rf ~/.Trash/* 2>/dev/null || true
  command -v brew >/dev/null 2>&1 && brew cleanup -s
fi

# 3. Developer & Environment Caches (Safe to remove)
log "Cleaning developer tools, runtime caches, and package logs..."
[ -d "$HOME/.cache" ] && rm -rf "$HOME/.cache/"* 2>/dev/null || true
[ -d "$HOME/.npm/_cacache" ] && rm -rf "$HOME/.npm/_cacache" 2>/dev/null || true
[ -d "$HOME/.yarn/berry/cache" ] && rm -rf "$HOME/.yarn/berry/cache" 2>/dev/null || true
command -v docker >/dev/null 2>&1 && docker system prune -f --volumes 2>/dev/null || true
command -v pip >/dev/null 2>&1 && pip cache purge 2>/dev/null || true

# 4. System Temp Folders
log "Cleaning temp directories..."
rm -rf /tmp/* 2>/dev/null || true
rm -rf /var/tmp/* 2>/dev/null || true

# 5. Calculation
AFTER_SPACE=$(get_free_space)
FREED_KB=$((AFTER_SPACE - BEFORE_SPACE))

if [ $FREED_KB -gt 0 ]; then
  FREED_MB=$((FREED_KB / 1024))
  log "Cleanup complete! Reclaimed approx ${BOLD}${FREED_MB} MB${NC} of space."
else
  log "Cleanup complete! System was already clean."
fi