#!/bin/bash

# CLIG Setup Script - Modernized

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting CLIG Setup...${NC}"

# OS Detection
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
else
    OS=$(uname -s)
fi

echo -e "Detected OS: ${YELLOW}$OS${NC}"

# Function to provide install instructions
get_install_cmd() {
    case $OS in
        ubuntu|debian|pop|mint)
            echo "sudo apt update && sudo apt install $1"
            ;;
        arch|manjaro)
            echo "sudo pacman -S $1"
            ;;
        fedora)
            echo "sudo dnf install $1"
            ;;
        *)
            echo "your package manager to install $1"
            ;;
    esac
}

# 1. Basic Build Tools
echo "Checking for requirements..."
for cmd in gcc make pkg-config; do
    if ! command -v $cmd &> /dev/null; then
        echo -e "${RED}Error: $cmd is not installed.${NC}"
        echo -e "Suggested command: $(get_install_cmd $cmd)"
        exit 1
    fi
done

# 2. Library Checks
echo "Checking for libraries..."
libraries=("ncurses")
for lib in "${libraries[@]}"; do
    if ! pkg-config --exists "$lib"; then
        echo -e "${YELLOW}Warning: $lib development files not found by pkg-config.${NC}"
        case $lib in
            ncurses) pkg="ncurses-devel (or libncurses-dev)";;
        esac
        echo -e "You might need to install: ${YELLOW}$pkg${NC}"
        echo -e "Try: $(get_install_cmd "$pkg")"
    fi
done

# 3. Global Build
echo -e "${GREEN}Building all games and launcher...${NC}"

declare -A build_status
GAMES=("2048" "snake" "tetris" "type")

# Build individual games
for game in "${GAMES[@]}"; do
    if [ -d "$game" ] && [ -f "$game/Makefile" ]; then
        echo -e "Building ${YELLOW}$game${NC}..."
        if (cd "$game" && make > /dev/null 2>&1); then
            build_status["$game"]="SUCCESS"
        else
            build_status["$game"]="FAILED"
        fi
    else
        build_status["$game"]="MISSING"
    fi
done

# Build launcher
echo -e "Building ${YELLOW}launcher${NC}..."
if gcc -Wall -Wextra launcher.c -o launcher -lncurses > /dev/null 2>&1; then
    build_status["launcher"]="SUCCESS"
else
    build_status["launcher"]="FAILED"
fi

# Summary Report
echo -e "\n${GREEN}--- Setup Summary ---${NC}"
for item in "${GAMES[@]}" "launcher"; do
    status=${build_status[$item]}
    case $status in
        SUCCESS|READY*) color=$GREEN ;;
        *) color=$RED ;;
    esac
    printf "%-12s: [%b%-15s%b]\n" "$item" "$color" "$status" "$NC"
done
echo -e "${GREEN}----------------------${NC}"

if [ "${build_status["launcher"]}" == "SUCCESS" ]; then
    echo -e "Run ${YELLOW}./launcher${NC} to play."
else
    echo -e "${RED}Launcher build failed. Check dependencies (ncurses).${NC}"
fi


