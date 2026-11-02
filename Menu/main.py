from textual.app import App
from screens.main_menu import MainMenu
from screens.system_monitor import SystemMonitor
from screens.file_commander import FileCommander
from screens.productivity import Productivity
from screens.quick_actions import QuickActions
from screens.apps_menu import AppsMenu

class TTY5514(App):
    """5514 TTY: A modern multipurpose terminal control center."""

    TITLE = "5514 TTY"
    SUB_TITLE = "Terminal Control Center"
    CSS_PATH = "styles.css"
    BINDINGS = [
        ("q", "quit", "Quit"),
    ]
    SCREENS = {
        "main_menu": MainMenu,
        "system_monitor": SystemMonitor,
        "file_commander": FileCommander,
        "productivity": Productivity,
        "quick_actions": QuickActions,
        "apps_menu": AppsMenu,
    }

    def __init__(self, **kwargs) -> None:
        super().__init__(ansi_color=True, **kwargs)

    def on_mount(self) -> None:
        self.theme = "ansi-dark"
        self.push_screen("main_menu")

if __name__ == "__main__":
    app = TTY5514()
    app.run()
