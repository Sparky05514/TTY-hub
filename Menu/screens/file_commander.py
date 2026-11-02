from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, DirectoryTree, Static, Button, Label
from textual.containers import Container, Horizontal, VerticalScroll
from pathlib import Path
from typing import Iterable
import os
import datetime
import subprocess
from rich.syntax import Syntax
from rich.console import Group
from rich.text import Text

class AllFilesDirectoryTree(DirectoryTree):
    def filter_paths(self, paths: Iterable[Path]) -> Iterable[Path]:
        return paths

class FileCommander(Screen):
    """Modern redesigned File Commander with dual-pane preview & editor integration."""

    CSS_PATH = "../styles.css"
    BINDINGS = [
        ("escape", "back", "Back"),
        ("b", "back", "Back"),
        ("w", "cursor_up", "Up"),
        ("s", "cursor_down", "Down"),
        ("a", "go_parent", "Parent/Back"),
        ("d", "select_cursor", "Select/Enter"),
        ("e", "edit_file", "Edit (nvim)"),
    ]
    
    def compose(self) -> ComposeResult:
        yield Header()
        yield Container(
            Static("[bold ansi_green]📂 FILE COMMANDER & EXPLORER[/]", id="commander_title"),
            Static("[dim ansi_bright_black]Navigation: [bold]W/S/A/D[/] or Arrows • [bold]E[/] Edit in nvim • [bold]Esc/B[/] Back[/]", id="sub_banner"),
            Horizontal(
                Container(
                    AllFilesDirectoryTree(os.path.expanduser("~"), id="tree_view"),
                    id="left_pane"
                ),
                VerticalScroll(
                    Static("[dim ansi_bright_black]Select any file on the left tree to inspect syntax-highlighted preview and metadata.[/]", id="file_preview"),
                    id="right_pane"
                ),
                id="commander_split"
            ),
            Button("🔙 Back to Menu (Esc)", id="btn_back", classes="back_btn"),
            id="commander_container"
        )
        yield Footer()

    def on_directory_tree_file_selected(self, event: DirectoryTree.FileSelected) -> None:
        file_path = event.path
        self.update_preview(file_path)

    def on_directory_tree_directory_selected(self, event: DirectoryTree.DirectorySelected) -> None:
        preview = self.query_one("#file_preview", Static)
        dir_path = event.path
        try:
            items = len(list(dir_path.iterdir()))
            preview.update(f"[bold ansi_blue]📁 Directory:[/] {dir_path}\n[dim]Contains {items} items[/]")
        except Exception:
            preview.update(f"[bold ansi_blue]📁 Directory:[/] {dir_path}")

    def update_preview(self, file_path: Path) -> None:
        preview = self.query_one("#file_preview", Static)
        
        try:
            stats = file_path.stat()
            size = stats.st_size
            mtime = datetime.datetime.fromtimestamp(stats.st_mtime).strftime('%Y-%m-%d %H:%M:%S')
            
            info_text = f"[bold blue]File:[/] [bold]{file_path.name}[/]\n"
            info_text += f"[bold bright_black]Path:[/] {file_path}\n"
            info_text += f"[bold bright_black]Size:[/] {size:,} bytes  •  [bold bright_black]Modified:[/] {mtime}\n"
            info_text += "─" * 40 + "\n\n"
            
            if size < 50000:
                try:
                    content = file_path.read_text(encoding="utf-8")
                    lexer = Syntax.guess_lexer(file_path.name, code=content)
                    syntax = Syntax(content, lexer, theme="ansi_dark", line_numbers=True, word_wrap=True)
                    preview.update(Group(Text.from_markup(info_text), syntax))
                    return
                except UnicodeDecodeError:
                    info_text += "[italic red]⚠️ Binary file or unsupported encoding (cannot render preview).[/]"
                except Exception as e:
                    info_text += f"[italic red]Error reading file: {e}[/]"
            else:
                info_text += "[italic yellow]⚠️ File size exceeds preview limit (>50KB). Press 'E' to open in nvim.[/]"
                
            preview.update(Text.from_markup(info_text))
            
        except Exception as e:
            preview.update(f"[bold ansi_red]Error accessing file:[/] {e}")

    def action_back(self) -> None:
        self.app.pop_screen()

    def action_cursor_up(self) -> None:
        self.query_one("#tree_view", DirectoryTree).action_cursor_up()

    def action_cursor_down(self) -> None:
        self.query_one("#tree_view", DirectoryTree).action_cursor_down()

    def action_go_parent(self) -> None:
        self.query_one("#tree_view", DirectoryTree).action_cursor_left()

    def action_select_cursor(self) -> None:
        self.query_one("#tree_view", DirectoryTree).action_select_cursor()

    def action_edit_file(self) -> None:
        tree = self.query_one("#tree_view", DirectoryTree)
        if tree.cursor_node and tree.cursor_node.data:
            file_path = tree.cursor_node.data.path
            if file_path.is_file():
                with self.app.suspend():
                    editor = os.environ.get("EDITOR", "nvim")
                    try:
                        subprocess.run([editor, str(file_path)])
                    except FileNotFoundError:
                        try:
                            subprocess.run(["nano", str(file_path)])
                        except FileNotFoundError:
                            subprocess.run(["vi", str(file_path)])
                self.update_preview(file_path)

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "btn_back":
            self.action_back()
