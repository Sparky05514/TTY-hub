from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, Static, Button, Label
from textual.containers import Container, Grid
from textual import events
import datetime
import requests
import psutil

MENU_DESCRIPTIONS = {
    "btn_stats": "📊 Real-time CPU, RAM, Disk monitors & interactive process manager",
    "btn_files": "📂 Dual-pane file commander with syntax highlighting & editor integration",
    "btn_prod": "✅ Task organizer & Todo manager with keyboard shortcuts",
    "btn_actions": "⚡ Configurable quick terminal commands & interactive runner",
    "btn_apps": "📱 Terminal apps & arcade: WP-cli, Snake, Tetris, 2048, Type",
    "btn_quit": "🚪 Exit 5514 TTY terminal suite",
}

class MainMenu(Screen):
    """Modern redesigned main menu for 5514 TTY."""

    BINDINGS = [
        ("1", "nav_stats", "System Stats"),
        ("2", "nav_files", "File Commander"),
        ("3", "nav_prod", "Productivity"),
        ("4", "nav_actions", "Quick Actions"),
        ("5", "nav_apps", "Apps"),
        ("q", "nav_quit", "Quit"),
    ]

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        yield Container(
            # Sleek 5514 TTY ASCII Logo
            Static(
                r"""[bold ansi_bright_cyan]  ███████╗███████╗ ██╗██╗  ██╗    ████████╗████████╗██╗   ██╗[/]
[bold ansi_cyan]  ██╔════╝██╔════╝███║██║  ██║    ╚══██╔══╝╚══██╔══╝╚██╗ ██╔╝[/]
[bold ansi_bright_blue]  ███████╗███████╗╚██║███████║       ██║      ██║    ╚████╔╝ [/]
[bold ansi_blue]  ╚════██║╚════██║ ██║╚════██║       ██║      ██║     ╚██╔╝  [/]
[bold ansi_magenta]  ███████║███████║ ██║     ██║       ██║      ██║      ██║   [/]
[dim ansi_bright_black]  ╚══════╝╚══════╝ ╚═╝     ╚═╝       ╚═╝      ╚═╝      ╚═╝   [/]""",
                id="logo"
            ),
            Static("[dim ansi_bright_black]— Terminal Workspace & Control Center —[/]", id="sub_banner"),
            
            # Live Dashboard (3 Cards)
            Container(
                Container(
                    Label("🕒 SYSTEM TIME", classes="dash_title"),
                    Label("Loading...", id="clock_val", classes="dash_value"),
                    classes="dash_card"
                ),
                Container(
                    Label("⚡ HARDWARE LOAD", classes="dash_title"),
                    Label("Loading...", id="hw_val", classes="dash_value"),
                    classes="dash_card"
                ),
                Container(
                    Label("🌤️ WEATHER", classes="dash_title"),
                    Label("Loading...", id="weather_val", classes="dash_value"),
                    classes="dash_card"
                ),
                id="dashboard_grid"
            ),
            
            # Main Menu Grid (2x3 symmetrical layout)
            Container(
                Button("[1] 📊 System Stats", id="btn_stats", classes="menu_btn"),
                Button("[2] 📂 File Commander", id="btn_files", classes="menu_btn"),
                Button("[3] ✅ Productivity", id="btn_prod", classes="menu_btn"),
                Button("[4] ⚡ Quick Actions", id="btn_actions", classes="menu_btn"),
                Button("[5] 📱 Apps", id="btn_apps", classes="menu_btn"),
                Button("[Q] 🚪 Quit 5514 TTY", id="btn_quit", classes="menu_btn"),
                id="menu_grid"
            ),
            
            # Context / Tooltip description panel
            Static(
                MENU_DESCRIPTIONS["btn_stats"],
                id="desc_panel"
            ),
            
            id="main_container"
        )
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#btn_stats", Button).focus()
        self.update_clock()
        self.update_hw_stats()
        self.set_interval(1, self.update_clock)
        self.set_interval(2, self.update_hw_stats)
        self.fetch_weather_async()
        self.set_interval(600, self.fetch_weather_async)

    def update_clock(self) -> None:
        now = datetime.datetime.now()
        time_str = now.strftime("%H:%M:%S  •  %Y-%m-%d")
        try:
            self.query_one("#clock_val", Label).update(time_str)
        except Exception:
            pass

    def update_hw_stats(self) -> None:
        try:
            cpu = psutil.cpu_percent()
            mem = psutil.virtual_memory().percent
            hw_str = f"CPU: {cpu:.0f}%  |  RAM: {mem:.0f}%"
            self.query_one("#hw_val", Label).update(hw_str)
        except Exception:
            pass

    def fetch_weather_async(self) -> None:
        self.run_worker(self._fetch_weather_thread, thread=True)

    def _fetch_weather_thread(self) -> None:
        weather_text = "🌤️ Offline / Unknown"
        try:
            resp = requests.get("https://wttr.in/?format=%C+%t", timeout=4)
            if resp.status_code == 200 and resp.text.strip():
                clean_text = resp.text.strip()
                weather_text = f"🌤️ {clean_text}"
            else:
                weather_text = "🌤️ Weather unavailable"
        except Exception:
            weather_text = "🌤️ Weather (Offline)"
        
        self.app.call_from_thread(self._update_weather_ui, weather_text)

    def _update_weather_ui(self, text: str) -> None:
        try:
            self.query_one("#weather_val", Label).update(text)
        except Exception:
            pass

    def on_descendant_focus(self, event: events.DescendantFocus) -> None:
        widget_id = getattr(event.control, "id", None)
        if widget_id and widget_id in MENU_DESCRIPTIONS:
            self.query_one("#desc_panel", Static).update(MENU_DESCRIPTIONS[widget_id])

    def on_key(self, event) -> None:
        focused = self.app.focused
        if not focused or not hasattr(focused, "id"):
            return
            
        current_id = focused.id
        button_ids = ["btn_stats", "btn_files", "btn_prod", "btn_actions", "btn_apps", "btn_quit"]
        
        if current_id in button_ids:
            idx = button_ids.index(current_id)
            row = idx // 2
            col = idx % 2
            next_id = None
            
            # WASD, Arrow keys, Vim keys (hjkl)
            if event.key in ("up", "w", "k"):
                if row > 0:
                    next_id = button_ids[idx - 2]
            elif event.key in ("down", "s", "j"):
                if row < 2:
                    next_id = button_ids[idx + 2]
            elif event.key in ("left", "a", "h"):
                if col > 0:
                    next_id = button_ids[idx - 1]
            elif event.key in ("right", "d", "l"):
                if col < 1:
                    next_id = button_ids[idx + 1]
            
            if next_id:
                target_btn = self.query_one(f"#{next_id}", Button)
                target_btn.focus()
                self.query_one("#desc_panel", Static).update(MENU_DESCRIPTIONS.get(next_id, ""))

    def on_button_pressed(self, event: Button.Pressed) -> None:
        btn_id = event.button.id
        if btn_id == "btn_quit":
            self.app.exit()
        elif btn_id == "btn_stats":
            self.app.push_screen("system_monitor")
        elif btn_id == "btn_files":
            self.app.push_screen("file_commander")
        elif btn_id == "btn_prod":
            self.app.push_screen("productivity")
        elif btn_id == "btn_actions":
            self.app.push_screen("quick_actions")
        elif btn_id == "btn_apps":
            self.app.push_screen("apps_menu")

    def action_nav_stats(self) -> None:
        self.app.push_screen("system_monitor")

    def action_nav_files(self) -> None:
        self.app.push_screen("file_commander")

    def action_nav_prod(self) -> None:
        self.app.push_screen("productivity")

    def action_nav_actions(self) -> None:
        self.app.push_screen("quick_actions")

    def action_nav_apps(self) -> None:
        self.app.push_screen("apps_menu")

    def action_nav_quit(self) -> None:
        self.app.exit()
