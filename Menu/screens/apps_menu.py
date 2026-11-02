from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, Button, Static, Label
from textual.containers import Container, Grid
from textual import events
import subprocess
import shutil
import os
import sys

APP_INFO = {
    "btn_snake": "🐍 [bold ansi_green]Snake[/] — Classic snake navigation. Eat food, grow longer, avoid walls. Controls: [bold]WASD / Arrows[/].",
    "btn_tetris": "🧱 [bold ansi_blue]Tetris[/] — Modern block stacking with SRS kicks & 7-bag randomizer. Controls: [bold]WASD, Space (Hard Drop), C (Hold)[/].",
    "btn_2048": "🔢 [bold ansi_yellow]2048[/] — Slide matching number tiles to reach the 2048 tile. Controls: [bold]WASD / Arrow Keys[/].",
    "btn_type": "⌨️ [bold ansi_red]Type[/] — Fast-paced typing game. Type moving words before they reach the left edge. Controls: [bold]Keyboard letters[/].",
    "btn_wp": "💬 [bold ansi_bright_green]WP-cli[/] — Full-screen terminal WhatsApp direct-message client.",
    "btn_back": "🔙 Return to the main 5514 TTY dashboard.",
}

class AppsMenu(Screen):
    """Modern redesigned apps & arcade screen."""

    BINDINGS = [
        ("escape", "back", "Back"),
        ("b", "back", "Back"),
        ("1", "play_snake", "Snake"),
        ("2", "play_tetris", "Tetris"),
        ("3", "play_2048", "2048"),
        ("4", "play_type", "Type"),
        ("5", "launch_wp", "WP-cli"),
    ]

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield Container(
            Static("[bold ansi_yellow]📱 TERMINAL APPS & ARCADE[/]", id="apps_title"),
            Static("[dim ansi_bright_black]Choose an app to launch • Use [1-5], WASD, or Arrow Keys[/]", id="sub_banner"),
            Grid(
                Button("[1] 🐍 Snake", id="btn_snake", classes="app_btn"),
                Button("[2] 🧱 Tetris", id="btn_tetris", classes="app_btn"),
                Button("[3] 🔢 2048", id="btn_2048", classes="app_btn"),
                Button("[4] ⌨️ Type", id="btn_type", classes="app_btn"),
                Button("[5] 💬 WP-cli", id="btn_wp", classes="app_btn"),
                Button("[B] 🔙 Back to Menu", id="btn_back", classes="app_btn"),
                id="apps_grid"
            ),
            Static(APP_INFO["btn_snake"], id="desc_panel"),
            id="apps_container"
        )
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#btn_snake", Button).focus()

    def on_descendant_focus(self, event: events.DescendantFocus) -> None:
        widget_id = getattr(event.control, "id", None)
        if widget_id and widget_id in APP_INFO:
            self.query_one("#desc_panel", Static).update(APP_INFO[widget_id])

    def on_key(self, event) -> None:
        focused = self.app.focused
        if not focused or not hasattr(focused, "id"):
            return
            
        current_id = focused.id
        button_ids = ["btn_snake", "btn_tetris", "btn_2048", "btn_type", "btn_wp", "btn_back"]
        
        if current_id in button_ids:
            idx = button_ids.index(current_id)
            row = idx // 2
            col = idx % 2
            next_id = None
            
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
                self.query_one(f"#{next_id}", Button).focus()
                self.query_one("#desc_panel", Static).update(APP_INFO.get(next_id, ""))

    def on_button_pressed(self, event: Button.Pressed) -> None:
        btn_id = event.button.id
        if btn_id == "btn_back":
            self.action_back()
        elif btn_id == "btn_snake":
            self.launch_app("snake")
        elif btn_id == "btn_tetris":
            self.launch_app("tetris")
        elif btn_id == "btn_2048":
            self.launch_app("2048")
        elif btn_id == "btn_type":
            self.launch_app("type")
        elif btn_id == "btn_wp":
            self.launch_app("wp")

    def action_back(self) -> None:
        self.app.pop_screen()

    def action_play_snake(self) -> None:
        self.launch_app("snake")

    def action_play_tetris(self) -> None:
        self.launch_app("tetris")

    def action_play_2048(self) -> None:
        self.launch_app("2048")

    def action_play_type(self) -> None:
        self.launch_app("type")

    def action_launch_wp(self) -> None:
        self.launch_app("wp")

    def launch_app(self, app_name: str) -> None:
        tty_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
        clig_path = os.path.join(tty_root, "CLIG")
        wp_candidates = [
            os.path.join(tty_root, "WP-cli/wp"),
            os.path.expanduser("~/Projects/WP-cli/wp"),
            shutil.which("wp") or "",
        ]
        wp_path = next((p for p in wp_candidates if p and os.path.exists(p)), wp_candidates[0])

        app_paths = {
            "snake": os.path.join(clig_path, "snake/snake"),
            "tetris": os.path.join(clig_path, "tetris/tetris"),
            "2048": os.path.join(clig_path, "2048/2048"),
            "type": os.path.join(clig_path, "type/type"),
            "wp": wp_path,
        }
        
        app_path = app_paths.get(app_name)
        
        if app_path and os.path.exists(app_path):
            with self.app.suspend():
                try:
                    subprocess.run([app_path], check=False)
                except Exception as e:
                    print(f"Error launching {app_name}: {e}")
                    input("Press Enter to continue...")
        else:
            self.app.notify(f"App binary not found: {app_path}", severity="error")
