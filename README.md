# TTY Hub

A unified terminal workspace, control center, and CLI apps/games suite combining **5514 TTY Menu** (`Menu/`) and **CLIG** (`CLIG/`).

## Components

### 1. `Menu/` — 5514 TTY Control Center
A modern terminal control center built with Python and [Textual](https://textual.textualize.io/):
- **System Stats**: Real-time CPU, RAM, disk monitors, and interactive process manager.
- **File Commander**: Dual-pane file browser with syntax highlighting and editor integration.
- **Productivity**: Task organizer and Todo manager with keyboard shortcuts.
- **Quick Actions**: Configurable terminal commands and interactive runner.
- **Apps**: Integrated launcher for terminal apps and games (**WP-cli**, **Snake**, **Tetris**, **2048**, **Type**).

#### Run the Menu
```bash
cd Menu
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
./run.sh
```

### 2. `CLIG/` — Command Line Games
A collection of terminal games written in C using `ncurses`:
- **Snake** (`CLIG/snake`)
- **Tetris** (`CLIG/tetris`)
- **2048** (`CLIG/2048`)
- **Type** (`CLIG/type`)

#### Build CLIG Games
```bash
cd CLIG
./setup.sh
```
