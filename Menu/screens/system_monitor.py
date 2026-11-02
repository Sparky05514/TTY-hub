from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Header, Footer, Static, Button, ProgressBar, Label, DataTable
from textual.containers import Container, Vertical, Horizontal
import psutil

class SystemMonitor(Screen):
    """Modern redesigned System Monitor with live metrics & process manager."""

    CSS_PATH = "../styles.css"
    BINDINGS = [
        ("escape", "back", "Back"),
        ("b", "back", "Back"),
        ("r", "refresh_data", "Refresh"),
        ("k", "kill_selected", "Kill Process"),
    ]
    
    def compose(self) -> ComposeResult:
        yield Header()
        yield Container(
            Static("[bold ansi_cyan]📊 REAL-TIME SYSTEM MONITOR[/]", id="monitor_title"),
            
            # Metric Cards Row
            Container(
                Vertical(
                    Label("CPU Load:", classes="stat_label"),
                    ProgressBar(id="cpu_bar", total=100, show_eta=False),
                    Label("0%", id="cpu_val"),
                    classes="monitor_metric_card"
                ),
                Vertical(
                    Label("Memory Usage:", classes="stat_label"),
                    ProgressBar(id="mem_bar", total=100, show_eta=False),
                    Label("0%", id="mem_val"),
                    classes="monitor_metric_card"
                ),
                Vertical(
                    Label("Disk Usage (/):", classes="stat_label"),
                    ProgressBar(id="disk_bar", total=100, show_eta=False),
                    Label("0%", id="disk_val"),
                    classes="monitor_metric_card"
                ),
                id="monitor_metrics_grid"
            ),
            
            Static("[bold ansi_yellow]⚡ Top Processes (Sorted by CPU %)[/]", classes="group_title"),
            DataTable(id="process_table"),
            
            Horizontal(
                Button("❌ Kill Selected (K)", id="btn_kill", variant="error"),
                Button("🔄 Refresh (R)", id="btn_refresh", variant="primary"),
                Button("🔙 Back (Esc)", id="btn_back", classes="back_btn"),
                classes="action_row"
            ),
            id="monitor_container"
        )
        yield Footer()

    def on_mount(self) -> None:
        self.setup_table()
        self.update_stats()
        self.update_processes()
        self.set_interval(1, self.update_stats)
        self.set_interval(4, self.update_processes)

    def setup_table(self) -> None:
        table = self.query_one("#process_table", DataTable)
        table.cursor_type = "row"
        table.zebra_stripes = True
        table.add_columns("PID", "Process Name", "CPU %", "Memory %")

    def update_processes(self) -> None:
        table = self.query_one("#process_table", DataTable)
        current_cursor = table.cursor_coordinate
        table.clear()
        
        procs = []
        for proc in psutil.process_iter(['pid', 'name', 'cpu_percent', 'memory_percent']):
            try:
                pinfo = proc.info
                if pinfo['cpu_percent'] is None:
                    pinfo['cpu_percent'] = 0.0
                if pinfo['memory_percent'] is None:
                    pinfo['memory_percent'] = 0.0
                procs.append(pinfo)
            except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
                pass
        
        procs.sort(key=lambda x: x['cpu_percent'], reverse=True)
        
        for p in procs[:20]:
            table.add_row(
                str(p['pid']),
                str(p['name']),
                f"{p['cpu_percent']:.1f}%",
                f"{p['memory_percent']:.1f}%"
            )
            
        if current_cursor and current_cursor.row < len(procs[:20]):
            table.cursor_coordinate = current_cursor

    def update_stats(self) -> None:
        try:
            cpu = psutil.cpu_percent()
            vmem = psutil.virtual_memory()
            disk = psutil.disk_usage('/')

            mem_used_gb = (vmem.total - vmem.available) / (1024 ** 3)
            mem_total_gb = vmem.total / (1024 ** 3)
            disk_used_gb = disk.used / (1024 ** 3)
            disk_total_gb = disk.total / (1024 ** 3)

            self.query_one("#cpu_bar", ProgressBar).update(total=100, progress=cpu)
            self.query_one("#cpu_val", Label).update(f"{cpu:.1f}%")

            self.query_one("#mem_bar", ProgressBar).update(total=100, progress=vmem.percent)
            self.query_one("#mem_val", Label).update(f"{vmem.percent:.1f}% ({mem_used_gb:.1f}/{mem_total_gb:.1f} GB)")

            self.query_one("#disk_bar", ProgressBar).update(total=100, progress=disk.percent)
            self.query_one("#disk_val", Label).update(f"{disk.percent:.1f}% ({disk_used_gb:.1f}/{disk_total_gb:.1f} GB)")
        except Exception:
            pass

    def action_back(self) -> None:
        self.app.pop_screen()

    def action_refresh_data(self) -> None:
        self.update_stats()
        self.update_processes()
        self.app.notify("Process list refreshed", severity="information")

    def action_kill_selected(self) -> None:
        self.kill_process()

    def kill_process(self) -> None:
        table = self.query_one("#process_table", DataTable)
        try:
            row_key = table.coordinate_to_cell_key(table.cursor_coordinate)
            if not row_key:
                self.app.notify("No process selected", severity="warning")
                return
                
            row = table.get_row(row_key.row_key)
            pid = int(row[0])
            
            proc = psutil.Process(pid)
            proc.terminate()
            self.app.notify(f"Successfully terminated PID {pid} ({row[1]})", severity="information")
            self.update_processes()
        except psutil.NoSuchProcess:
            self.app.notify("Process no longer exists", severity="error")
        except psutil.AccessDenied:
            self.app.notify("Permission denied (requires root/sudo)", severity="error")
        except Exception as e:
            self.app.notify(f"Error terminating process: {e}", severity="error")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "btn_back":
            self.action_back()
        elif event.button.id == "btn_refresh":
            self.action_refresh_data()
        elif event.button.id == "btn_kill":
            self.action_kill_selected()
