from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, Static, Button, Input, ListItem, ListView, Label
from textual.containers import Container, Vertical
import json
import os

TODO_FILE = "todo.json"

class Productivity(Screen):
    """Modern redesigned Productivity & Task Manager suite."""

    CSS_PATH = "../styles.css"
    BINDINGS = [
        ("escape", "back", "Back"),
        ("b", "back", "Back"),
        ("d", "delete_task", "Delete"),
        ("e", "edit_task", "Edit"),
        ("space", "toggle_done", "Toggle Done"),
    ]
    
    def compose(self) -> ComposeResult:
        yield Header()
        yield Container(
            Static("[bold ansi_yellow]✅ PRODUCTIVITY & TASK MANAGER[/]", id="prod_title"),
            Static("[dim ansi_bright_black]Shortcuts: [bold]Enter[/] Add Task • [bold]Space[/] Toggle • [bold]E[/] Edit • [bold]D[/] Delete • [bold]Esc/B[/] Back[/]", id="sub_banner"),
            Static("Loading tasks...", id="prod_stats_bar"),
            Input(placeholder="Type a task and press Enter...", id="todo_input"),
            ListView(id="todo_list"),
            Button("🔙 Back to Menu (Esc)", id="btn_back", classes="back_btn"),
            id="prod_container"
        )
        yield Footer()

    def on_mount(self) -> None:
        self.todos = []
        self.editing_index = None
        self.load_todos()
        self.query_one("#todo_input", Input).focus()

    def update_stats_bar(self) -> None:
        total = len(self.todos)
        done_count = sum(1 for t in self.todos if t.get("done"))
        pending_count = total - done_count
        stats_text = f"📊 [bold ansi_default]{total}[/] Total Tasks  •  [bold ansi_green]{done_count}[/] Completed  •  [bold ansi_yellow]{pending_count}[/] Pending"
        self.query_one("#prod_stats_bar", Static).update(stats_text)

    def load_todos(self) -> None:
        list_view = self.query_one("#todo_list", ListView)
        list_view.clear()
        self.todos = []
        
        if os.path.exists(TODO_FILE):
            try:
                with open(TODO_FILE, "r") as f:
                    raw_data = json.load(f)
                    
                    processed_data = []
                    for item in raw_data:
                        if isinstance(item, str):
                            processed_data.append({"text": item, "done": False})
                        elif isinstance(item, dict):
                            processed_data.append(item)
                    
                    self.todos = processed_data
                    
                    for todo in self.todos:
                        list_view.append(ListItem(self.render_todo(todo)))
            except Exception:
                pass
                
        self.update_stats_bar()

    def render_todo(self, todo: dict) -> Label:
        is_done = todo.get("done", False)
        text = todo.get("text", "")
        if is_done:
            return Label(f"[bold ansi_green][✓ DONE][/] [strike dim ansi_bright_black]{text}[/]")
        else:
            return Label(f"[bold ansi_yellow][○ TODO][/] [bold ansi_default]{text}[/]")

    def save_todos(self) -> None:
        try:
            with open(TODO_FILE, "w") as f:
                json.dump(self.todos, f, indent=2)
        except Exception as e:
            self.app.notify(f"Error saving tasks: {e}", severity="error")
        self.update_stats_bar()

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        value = event.value.strip()
        if value:
            list_view = self.query_one("#todo_list", ListView)
            
            if self.editing_index is not None:
                if 0 <= self.editing_index < len(self.todos):
                    self.todos[self.editing_index]["text"] = value
                    item = list_view.children[self.editing_index]
                    item.remove()
                    new_item = ListItem(self.render_todo(self.todos[self.editing_index]))
                    list_view.insert(self.editing_index, [new_item])
                    
                self.editing_index = None
                self.app.notify("Task updated successfully", severity="information")
            else:
                new_todo = {"text": value, "done": False}
                self.todos.append(new_todo)
                await list_view.append(ListItem(self.render_todo(new_todo)))
                self.app.notify("Task added", severity="information")
            
            event.input.value = ""
            self.save_todos()

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        self.action_toggle_done()

    def action_delete_task(self) -> None:
        list_view = self.query_one("#todo_list", ListView)
        idx = list_view.index
        
        if idx is not None and 0 <= idx < len(self.todos):
            removed = self.todos.pop(idx)
            list_view.children[idx].remove()
            if self.editing_index == idx:
                self.editing_index = None
                self.query_one("#todo_input", Input).value = ""
            
            self.save_todos()
            self.app.notify(f"Deleted task: {removed.get('text', '')}", severity="information")

    def action_toggle_done(self) -> None:
        list_view = self.query_one("#todo_list", ListView)
        idx = list_view.index
        
        if idx is not None and 0 <= idx < len(self.todos):
            todo = self.todos[idx]
            todo["done"] = not todo["done"]
            
            item = list_view.children[idx]
            item.remove()
            new_item = ListItem(self.render_todo(todo))
            list_view.insert(idx, [new_item])
            list_view.index = idx
            
            self.save_todos()

    def action_edit_task(self) -> None:
        list_view = self.query_one("#todo_list", ListView)
        idx = list_view.index
        
        if idx is not None and 0 <= idx < len(self.todos):
            self.editing_index = idx
            todo = self.todos[idx]
            inp = self.query_one("#todo_input", Input)
            inp.value = todo["text"]
            inp.focus()
            self.app.notify(f"Editing task: {todo['text']}", severity="information")

    def action_back(self) -> None:
        self.app.pop_screen()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "btn_back":
            self.action_back()
