#!/usr/bin/env bash
# macOS Advanced Memory & Leak Diagnostic Tool

set -euo pipefail

# Standard ANSI escape sequences using safe printf
RED=$(printf '\033[1;31m')
GREEN=$(printf '\033[1;32m')
YELLOW=$(printf '\033[1;33m')
CYAN=$(printf '\033[1;36m')
BOLD=$(printf '\033[1m')
NC=$(printf '\033[0m') # No Color

HEADER_FORMAT="${BOLD}${CYAN}================================================================================${NC}"

print_header() {
    printf "\n%s\n" "${HEADER_FORMAT}"
    printf "%s  %s%s\n" "${BOLD}${CYAN}" "$1" "${NC}"
    printf "%s\n\n" "${HEADER_FORMAT}"
}

if [[ $EUID -ne 0 ]]; then
    printf "%sError: This script must be run as root to access process memory footprints.%s\n" "${RED}" "${NC}"
    printf "Please run using: sudo %s\n" "$0"
    exit 1
fi

print_header "1. ADVANCED MEMORY & SYSTEM PRESSURE OVERVIEW"

PAGE_SIZE=$(pagesize)
VM_STAT=$(vm_stat)
TOTAL_RAM_BYTES=$(sysctl -n hw.memsize)
TOTAL_RAM_GB=$(awk "BEGIN {printf \"%.2f\", $TOTAL_RAM_BYTES / 1073741824}")

get_vmstat_val() {
    echo "$VM_STAT" | awk -F: -v key="$1" '$1 ~ key {gsub("[^0-9]", "", $2); print $2}'
}

PAGES_FREE=$(get_vmstat_val "Pages free")
PAGES_ACTIVE=$(get_vmstat_val "Pages active")
PAGES_INACTIVE=$(get_vmstat_val "Pages inactive")
PAGES_SPECULATIVE=$(get_vmstat_val "Pages speculative")
PAGES_WIRED=$(get_vmstat_val "Pages wired down")
PAGES_COMPRESSED=$(get_vmstat_val "Pages occupied by compressor")
PAGES_PURGEABLE=$(get_vmstat_val "Pages purgeable")
PAGES_THROTTLED=$(get_vmstat_val "Pages throttled")
PAGES_FILEBACKED=$(get_vmstat_val "File-backed pages")
PAGES_ANONYMOUS=$(get_vmstat_val "Anonymous pages")
COMPRESSOR_PAGEINS=$(get_vmstat_val "Decompressions")
COMPRESSOR_PAGEOUTS=$(get_vmstat_val "Compressions")
SWAP_INS=$(get_vmstat_val "Swapins")
SWAP_OUTS=$(get_vmstat_val "Swapouts")

MEM_FREE_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_FREE * $PAGE_SIZE) / 1073741824}")
MEM_ACTIVE_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_ACTIVE * $PAGE_SIZE) / 1073741824}")
MEM_INACTIVE_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_INACTIVE * $PAGE_SIZE) / 1073741824}")
MEM_SPECULATIVE_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_SPECULATIVE * $PAGE_SIZE) / 1073741824}")
MEM_WIRED_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_WIRED * $PAGE_SIZE) / 1073741824}")
MEM_COMPRESSED_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_COMPRESSED * $PAGE_SIZE) / 1073741824}")
MEM_PURGEABLE_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_PURGEABLE * $PAGE_SIZE) / 1073741824}")
MEM_FILEBACKED_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_FILEBACKED * $PAGE_SIZE) / 1073741824}")
MEM_ANONYMOUS_GB=$(awk "BEGIN {printf \"%.2f\", ($PAGES_ANONYMOUS * $PAGE_SIZE) / 1073741824}")

# Physical Usage & Pressure Index
MEM_USED_GB=$(awk "BEGIN {printf \"%.2f\", $TOTAL_RAM_GB - $MEM_FREE_GB - $MEM_INACTIVE_GB}")
UTILIZATION_PCT=$(awk "BEGIN {printf \"%.1f\", (($TOTAL_RAM_GB - $MEM_FREE_GB) / $TOTAL_RAM_GB) * 100}")

printf "%sHardware Profile:%s\n" "${BOLD}" "${NC}"
printf "  %-30s %8s GB\n" "Total Installed Physical RAM:" "$TOTAL_RAM_GB"
printf "  %-30s %8s%%\n" "Current Memory Utilization:" "$UTILIZATION_PCT"

printf "\n%sDetailed Page Allocation:%s\n" "${BOLD}" "${NC}"
printf "  %-30s %8s GB\n" "Wired Memory (Kernel/Drivers):" "$MEM_WIRED_GB"
printf "  %-30s %8s GB\n" "Active Memory (In Active Use):" "$MEM_ACTIVE_GB"
printf "  %-30s %8s GB\n" "Inactive Memory (Reclaimable):" "$MEM_INACTIVE_GB"
printf "  %-30s %8s GB\n" "Compressed Memory (RAM Cache):" "$MEM_COMPRESSED_GB"
printf "  %-30s %8s GB\n" "Free Memory (Unallocated):" "$MEM_FREE_GB"
printf "  %-30s %8s GB\n" "Purgeable Memory (Volatile):" "$MEM_PURGEABLE_GB"
printf "  %-30s %8s GB\n" "File-Backed Cache:" "$MEM_FILEBACKED_GB"
printf "  %-30s %8s GB\n" "Anonymous (App Heap/Stack):" "$MEM_ANONYMOUS_GB"

printf "\n%sCompressor & Page Fault Metrics:%s\n" "${BOLD}" "${NC}"
printf "  %-30s %10s operations\n" "Total Memory Compressions:" "$COMPRESSOR_PAGEOUTS"
printf "  %-30s %10s operations\n" "Total Memory Decompressions:" "$COMPRESSOR_PAGEINS"
printf "  %-30s %10s pages\n" "Cumulative Swap Ins:" "$SWAP_INS"
printf "  %-30s %10s pages\n" "Cumulative Swap Outs:" "$SWAP_OUTS"

printf "\n%sSwap Usage Status:%s\n" "${BOLD}" "${NC}"
sysctl sysctl.proc_native sysctl.proc_translation vm.swapusage 2>/dev/null | awk '{print "  " $0}'


print_header "2. TOP 15 PROCESSES BY MEMORY FOOTPRINT & VIRTUAL SIZE"
printf "%-8s %-12s %-12s %-12s %s\n" "PID" "FOOTPRINT(MB)" "RSS (MB)" "VSZ (MB)" "COMMAND"
printf "--------------------------------------------------------------------------------\n"

# Uses footprint CLI if available, otherwise falls back to precise ps parsing
if command -v footprint >/dev/null 2>&1; then
    footprint -a -s | head -n 15 2>/dev/null || ps -eo pid,rss,vsz,comm | sort -k2 -nr | head -n 16 | awk 'NR>1 {printf "%-8s %-12.2f %-12.2f %-12.2f %s\n", $1, $2/1024, $2/1024, $3/1024, $4}'
else
    ps -eo pid,rss,vsz,comm | sort -k2 -nr | head -n 16 | awk 'NR>1 {printf "%-8s %-12s %-12.2f %-12.2f %s\n", $1, "N/A", $2/1024, $3/1024, $4}'
fi


print_header "3. APPLICATION FAMILY OVERHEAD (TREE AGGREGATION)"
printf "Aggregating memory across process trees (Main + Renderers + Helpers):\n"
printf "--------------------------------------------------------------------------------\n"

get_deep_app_memory() {
    local app_pattern=$1
    local display_name=$2
    
    local matched_pids
    matched_pids=$(pgrep -f -i "$app_pattern" || true)
    
    if [[ -n "$matched_pids" ]]; then
        local proc_count
        proc_count=$(echo "$matched_pids" | wc -l | xargs)
        
        local total_rss
        total_rss=$(ps -p $(echo "$matched_pids" | tr '\n' ',') -o rss= 2>/dev/null | awk '{sum+=$1} END {print sum}')
        
        local total_vsz
        total_vsz=$(ps -p $(echo "$matched_pids" | tr '\n' ',') -o vsz= 2>/dev/null | awk '{sum+=$1} END {print sum}')
        
        if [[ -n "$total_rss" && "$total_rss" -gt 0 ]]; then
            local total_rss_mb
            local total_vsz_mb
            total_rss_mb=$(awk "BEGIN {printf \"%.2f\", $total_rss / 1024}")
            total_vsz_mb=$(awk "BEGIN {printf \"%.2f\", $total_vsz / 1024}")
            
            printf "  %-25s | Procs: %-3s | RSS: %9s MB | VSZ: %10s MB\n" "$display_name" "$proc_count" "$total_rss_mb" "$total_vsz_mb"
        fi
    else
        printf "  %-25s | Procs: 0   | Status: Not Running\n" "$display_name"
    fi
}

get_deep_app_memory "Google Chrome|Chrome Helper" "Google Chrome"
get_deep_app_memory "Safari|SafariShared" "Apple Safari"
get_deep_app_memory "Slack|Slack Helper" "Slack"
get_deep_app_memory "Microsoft Teams|Teams Helper" "Microsoft Teams"
get_deep_app_memory "Docker|com.docker" "Docker Desktop"
get_deep_app_memory "Code|VSCode|Code Helper" "VS Code"
get_deep_app_memory "Electron" "Generic Electron Apps"
get_deep_app_memory "ollama" "Ollama Runner"
get_deep_app_memory "LM Studio|lmstudio" "LM Studio"
get_deep_app_memory "java|idea|goland|pycharm" "Java / JetBrains IDEs"


print_header "4. KERNEL ARCHITECTURE & SYSTEM DAEMONS"
printf "%-8s %-16s %-12s %s\n" "PID" "USER" "RSS (MB)" "COMMAND"
printf "--------------------------------------------------------------------------------\n"
ps -eo pid,user,rss,comm | grep -E "root|_driverkit|_windowserver|_locationd|_coredaemon" | sort -k3 -nr | head -n 10 | awk '{printf "%-8s %-16s %-12.2f %s\n", $1, $2, $3/1024, $4}'


print_header "5. DIAGNOSTICS & SYSTEM LEAK RECOMMENDATIONS"

SWAP_USED=$(sysctl -n vm.swapusage | awk '{print $6}' | sed 's/M//')
SWAP_VAL=$(awk -v val="$SWAP_USED" 'BEGIN {print int(val)}')

if [ "$SWAP_VAL" -gt 4090 ]; then
    printf "%s[CRITICAL] High Swap Allocation (%sM).%s\n" "${RED}" "${SWAP_USED}" "${NC}"
    printf "  High swap activity detected. Active processes exceed physical RAM.\n"
elif [ "$SWAP_VAL" -gt 1024 ]; then
    printf "%s[WARNING] Moderate Swap Allocation (%sM).%s\n" "${YELLOW}" "${SWAP_USED}" "${NC}"
    printf "  Memory pressure is causing non-active process pages to swap to disk.\n"
else
    printf "%s[OK] Healthy Swap Allocation (%sM).%s\n" "${GREEN}" "${SWAP_USED}" "${NC}"
    printf "  No harmful paging activity detected.\n"
fi

# WindowServer Heap Inspection
WS_PID=$(pgrep -x "WindowServer" || true)
if [ -n "$WS_PID" ]; then
    WS_MEM=$(ps -p "$WS_PID" -o rss= | awk '{print int($1/1024)}')
    if [ "$WS_MEM" -gt 3072 ]; then
        printf "\n%s[LEAK WARNING] WindowServer Footprint High (%s MB).%s\n" "${RED}" "${WS_MEM}" "${NC}"
        printf "  - Causes: High display scaling, multi-monitor setups, or unreleased display context buffers.\n"
    else
        printf "\n%s[OK] WindowServer Footprint Normal (%s MB).%s\n" "${GREEN}" "${WS_MEM}" "${NC}"
    fi
fi

# Compressor Efficiency Check
if [ "$PAGES_COMPRESSED" -gt "$PAGES_ACTIVE" ]; then
    printf "\n%s[NOTICE] Memory Compressor Active%s\n" "${YELLOW}" "${NC}"
    printf "  Compressed memory (%s GB) exceeds active RAM (%s GB). CPU cycles are being spent compressing page tables.\n" "$MEM_COMPRESSED_GB" "$MEM_ACTIVE_GB"
fi

printf "\n%sTargeted Remedies:%s\n" "${BOLD}" "${NC}"
printf "  1. Flush Inactive RAM & Purgeable Buffers:\n"
printf "     %ssudo purge%s\n" "${YELLOW}" "${NC}"
printf "  2. Inspect Detailed Memory Map for Specific PID:\n"
printf "     %ssudo vmmap -summary <PID>%s\n" "${YELLOW}" "${NC}"
printf "  3. Reclaim WindowServer Memory Buffers:\n"
printf "     %ssudo killall -HUP WindowServer%s\n" "${YELLOW}" "${NC}"