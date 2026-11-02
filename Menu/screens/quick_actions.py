from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, Static, Button, RichLog, Label
from textual.containers import Container, Horizontal, Vertical
import subprocess
import json
import os
import datetime

COMMANDS_FILE = "commands.json"

class QuickActions(Screen):
    """Modern redesigned Quick Actions & command runner."""

    CSS_PATH = "../styles.css"
    commands_data = []
    BINDINGS = [
        ("escape", "back", "Back"),
        ("b", "back", "Back"),
        ("c", "clear_log", "Clear Log"),
    ]

    def compose(self) -> ComposeResult:
        self.load_commands()
        
        yield Header()
        yield Container(
            Static("[bold ansi_magenta]⚡ QUICK ACTIONS & COMMAND RUNNER[/]", id="actions_title"),
            Static("[dim ansi_bright_black]Execute common system utilities, inspect output, or run interactive tools • [bold]C[/] Clear • [bold]Esc/B[/] Back[/]", id="sub_banner"),
            Horizontal(
                Vertical(
                    *[Button(cmd["label"], id=cmd["id"], variant=cmd.get("variant", "default")) for cmd in self.commands_data],
                    Button("🧹 Clear Output (C)", id="btn_clear", variant="error"),
                    id="actions_menu"
                ),
                Vertical(
                    RichLog(id="command_log", highlight=True, markup=True),
                    id="log_container"
                ),
                id="actions_split"
            ),
            Button("🔙 Back to Menu (Esc)", id="btn_back", classes="back_btn"),
            id="actions_container"
        )
        yield Footer()

    def load_commands(self) -> None:
        if os.path.exists(COMMANDS_FILE):
            try:
                with open(COMMANDS_FILE, "r") as f:
                    self.commands_data = json.load(f)
            except Exception as e:
                self.commands_data = [{"id": "error", "label": f"Error loading JSON: {e}", "command": "echo error"}]
        else:
            self.commands_data = []

    def on_mount(self) -> None:
        log = self.query_one("#command_log", RichLog)
        log.write("[bold blue]5514 TTY Quick Actions Terminal ready.[/]")
        log.write("[dim bright_black]Select any command on the left panel to execute.[/]\n")
        if self.commands_data:
            first_id = self.commands_data[0]["id"]
            try:
                self.query_one(f"#{first_id}", Button).focus()
            except Exception:
                pass

    def run_command(self, cmd_str: str, interactive: bool = False) -> None:
        log = self.query_one("#command_log", RichLog)
        timestamp = datetime.datetime.now().strftime("%H:%M:%S")
        
        if interactive:
            log.write(f"[[bold bright_yellow]{timestamp}[/]] [bold yellow]⚡ Launching interactive process:[/] {cmd_str}")
            with self.app.suspend():
                try:
                    subprocess.run(cmd_str, shell=True)
                except Exception as e:
                    print(f"Error executing command: {e}")
                    input("Press Enter to return to 5514 TTY...")
            log.write(f"[[bold green]{timestamp}[/]] [bold green]✓ Interactive process returned.[/]\n")
        else:
            log.write(f"[[bold blue]{timestamp}[/]] [bold cyan]$ {cmd_str}[/]")
            try:
                result = subprocess.run(cmd_str, shell=True, capture_output=True, text=True, timeout=15)
                if result.stdout:
                    log.write(result.stdout.rstrip())
                if result.stderr:
                    log.write(f"[bold red]{result.stderr.rstrip()}[/]")
                if result.returncode == 0:
                    log.write("[dim green]── Command completed with code 0 ──[/]\n")
                else:
                    log.write(f"[dim red]── Command exited with code {result.returncode} ──[/]\n")
            except subprocess.TimeoutExpired:
                log.write("[bold red]⚠️ Execution timed out after 15 seconds.[/]\n")
            except Exception as e:
                log.write(f"[bold red]Error: {e}[/]\n")

    def action_back(self) -> None:
        self.app.pop_screen()

    def action_clear_log(self) -> None:
        self.query_one("#command_log", RichLog).clear()
        self.app.notify("Output log cleared", severity="information")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        btn_id = event.button.id
        
        if btn_id == "btn_back":
            self.action_back()
            return
        elif btn_id == "btn_clear":
            self.action_clear_log()
            return

        for cmd in self.commands_data:
            if cmd["id"] == btn_id:
                self.run_command(cmd["command"], cmd.get("interactive", False))
                break
