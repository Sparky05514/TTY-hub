# TTY Hub

A unified terminal workspace, control center, and CLI apps/games suite combining **5514 TTY Menu** (`Menu/`), **WP-cli** (`WP-cli/`), and **CLIG** (`CLIG/`).

## Quick Start

After cloning the repository, run the unified `setup` script at the root to compile all C games, build the `WP-cli` Go binary, and create/install the Python virtual environment for `Menu`:

```bash
chmod +x setup
./setup
```

Then start the **5514 TTY Menu**:
```bash
cd Menu && ./run.sh
```

---

## Components

### 1. `Menu/` — 5514 TTY Control Center
A modern terminal control center built with Python and [Textual](https://textual.textualize.io/):
- **System Stats**: Real-time CPU, RAM, disk monitors, and interactive process manager.
- **File Commander**: Dual-pane file browser with syntax highlighting and editor integration.
- **Productivity**: Task organizer and Todo manager with keyboard shortcuts.
- **Quick Actions**: Configurable terminal commands and interactive runner.
- **Apps**: Integrated launcher for terminal apps and games (**WP-cli**, **Snake**, **Tetris**, **2048**, **Type**).

### 2. `WP-cli/` — Terminal WhatsApp Direct-Message Client
A full-screen terminal WhatsApp client written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea) and `whatsmeow`:
- Launch directly from the **Apps** screen in `Menu` (`[5] 💬 WP-cli`) or run `./WP-cli/wp`.

### 3. `CLIG/` — Command Line Games
A collection of terminal games written in C using `ncurses`:
- **Snake** (`CLIG/snake`)
- **Tetris** (`CLIG/tetris`)
- **2048** (`CLIG/2048`)
- **Type** (`CLIG/type`)
