#!/usr/bin/env bash
# macOS Target & Kill Memory Diagnostic Tool

set -euo pipefail

# 1. AUTO-SETUP TOUCH ID FOR SUDO IF NOT ALREADY CONFIGURED
setup_touch_id() {
    if ! grep -q "pam_tid.so" /etc/pam.d/sudo /etc/pam.d/sudo_local 2>/dev/null; then
        if [[ -f /etc/pam.d/sudo_local.template ]]; then
            sudo cp /etc/pam.d/sudo_local.template /etc/pam.d/sudo_local
            sudo sed -i '' 's/^#auth/auth/' /etc/pam.d/sudo_local 2>/dev/null || true
        else
            sudo sed -i '' '1s/^/auth       sufficient     pam_tid.so\n/' /etc/pam.d/sudo 2>/dev/null || true
        fi
    fi
}

# 2. AUTO-ELEVATE TO SUDO IF NOT RUN AS ROOT
if [[ $EUID -ne 0 ]]; then
    printf "Requesting root privileges (Touch ID / Password required)...\n"
    setup_touch_id
    exec sudo "$0" "$@"
fi

# Standard ANSI escape sequences
RED=$(printf '\033[1;31m')
GREEN=$(printf '\033[1;32m')
YELLOW=$(printf '\033[1;33m')
CYAN=$(printf '\033[1;36m')
BOLD=$(printf '\033[1m')
NC=$(printf '\033[0m')

HEADER_LINE="================================================================================"

print_header() {
    printf "\n%s%s%s\n" "${BOLD}${CYAN}" "${HEADER_LINE}" "${NC}"
    printf "%s  %s%s\n" "${BOLD}${CYAN}" "$1" "${NC}"
    printf "%s%s%s\n\n" "${BOLD}${CYAN}" "${HEADER_LINE}" "${NC}"
}

print_header "1. APPLICATION FAMILY FOOTPRINTS"
printf "%-22s %-8s %-12s %-12s %s\n" "APP FAMILY" "PROCS" "TOTAL RAM" "TOP PID" "ACTION COMMAND"
printf "%s\n" "--------------------------------------------------------------------------------"

analyze_tree() {
    local name="$1"
    local pattern="$2"
    local stop_cmd_template="$3"

    local pids
    pids=$(pgrep -f -i "$pattern" || true)

    if [[ -n "$pids" ]]; then
        local count
        count=$(echo "$pids" | wc -l | xargs)

        local pid_list
        pid_list=$(echo "$pids" | tr '\n' ',' | sed 's/,$//')

        local total_rss_kb
        total_rss_kb=$(ps -p "$pid_list" -o rss= 2>/dev/null | awk '{sum+=$1} END {print sum}')

        if [[ -n "$total_rss_kb" && "$total_rss_kb" -gt 0 ]]; then
            local total_mb
            total_mb=$(awk "BEGIN {printf \"%.1f\", $total_rss_kb / 1024}")

            local top_pid
            top_pid=$(ps -p "$pid_list" -o pid=,rss= 2>/dev/null | sort -k2 -nr | head -n 1 | awk '{print $1}')

            local formatted_cmd
            formatted_cmd=$(echo "$stop_cmd_template" | sed "s/{PID}/$top_pid/g")

            printf "%-22s %-8s %-10s MB %-12s %s\n" "$name" "$count" "$total_mb" "PID $top_pid" "${YELLOW}$formatted_cmd${NC}"
        fi
    fi
}

analyze_tree "Google Chrome" "Google Chrome|Chrome Helper" "pkill -f 'Chrome Helper (Renderer)'"
analyze_tree "VS Code" "Code|VSCode|Code Helper" "pkill -f 'Code Helper'"
analyze_tree "Node / Web Servers" "node|next-server|vite" "kill -9 {PID}"
analyze_tree "Docker Desktop" "com.docker|Docker" "docker stop \$(docker ps -q)"
analyze_tree "Safari" "Safari|SafariShared" "pkill -f 'Safari'"
analyze_tree "Ollama AI" "ollama" "ollama stop <model> / kill -9 {PID}"
analyze_tree "LM Studio" "lmstudio|LM Studio" "Quit app / kill -9 {PID}"
analyze_tree "Java / JetBrains" "java|idea|goland|pycharm" "kill -9 {PID}"


print_header "2. TOP 12 HEAVIEST INDIVIDUAL PROCESSES (EXACT PIDs TO KILL)"
printf "%-8s %-12s %-32s %s\n" "PID" "RAM (MB)" "PROCESS / HELPER TYPE" "RECOMMENDED ACTION"
printf "%s\n" "--------------------------------------------------------------------------------"

# Filter out ps headers explicitly and parse top 12 RSS consumers
ps -eo pid=,rss=,args= | sort -k2 -nr | head -n 12 | while read -r pid rss args; do
    if [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]]; then
        mb=$(awk "BEGIN {printf \"%.1f\", $rss / 1024}")
        
        # Extract process or helper descriptor
        proc_desc=$(echo "$args" | awk '{print $1}')
        proc_name=$(basename "$proc_desc")

        if [[ "$args" == *"Chrome Helper (Renderer)"* ]]; then
            proc_name="Chrome Tab (Renderer)"
        elif [[ "$args" == *"Chrome Helper (GPU)"* ]]; then
            proc_name="Chrome GPU Process"
        elif [[ "$args" == *"Code Helper"* ]]; then
            proc_name="VS Code Language/Extension Host"
        fi

        action="kill -9 $pid"
        if [[ "$proc_name" == *"WindowServer"* ]]; then
            action="Close display windows (Do NOT kill)"
        elif [[ "$proc_name" == *"kernel_task"* ]]; then
            action="System thermal control (Do NOT kill)"
        fi

        printf "%-8s %-10s MB %-32s %s\n" "$pid" "$mb" "$proc_name" "${YELLOW}$action${NC}"
    fi
done


print_header "3. TARGETED REMEDIES FOR YOUR SYSTEM"

printf "%s[1] Reclaim ~4-5 GB from Chrome without closing browser window:%s\n" "${BOLD}" "${NC}"
printf "  Kill all background tab renderers at once (Chrome will auto-reload tabs when clicked):\n"
printf "  %spkill -f 'Chrome Helper (Renderer)'%s\n\n" "${YELLOW}" "${NC}"

printf "%s[2] Reclaim VS Code Extension Host RAM (~1.5 GB):%s\n" "${BOLD}" "${NC}"
printf "  Restart heavy language servers/TS Server workers:\n"
printf "  %spkill -f 'Code Helper (Plugin)'%s\n\n" "${YELLOW}" "${NC}"

printf "%s[3] Free Cache Buffers Instantly:%s\n" "${BOLD}" "${NC}"
printf "  %ssudo purge%s\n" "${YELLOW}" "${NC}"