from typing import ClassVar

from textual.app import App, ComposeResult
from textual.binding import BindingType
from textual.containers import Center, Horizontal, Vertical
from textual.widgets import Footer, Label, Static

TITLE_ART = r"""
          _
 _ __ ___| | __ _ _   _
| '__/ _ \ |/ _` | | | |
| | |  __/ | (_| | |_| |
|_|  \___|_|\__,_|\__, |
                  |___/
""".strip("\n")


class HomeApp(App):
    CSS = """
    Screen { align: center middle; }
    #frame { width: 84; height: auto; border: heavy $primary; padding: 1 2; }
    #title-box { width: 100%; height: auto; border: round $accent; padding: 0 2; }
    #title { color: $accent; text-style: bold; width: auto; }
    #tagline { color: $text-muted; width: 100%; text-align: center; }
    #buttons { height: auto; margin-top: 2; }
    """

    BINDINGS: ClassVar[list[BindingType]] = [("q", "quit", "Quit")]

    def compose(self) -> ComposeResult:
        with Vertical(id="frame"):
            with Vertical(id="title-box"):
                with Center():
                    yield Static(TITLE_ART, id="title")
                yield Label(
                    "turn idle laptops into a shared compute pool", id="tagline"
                )
            # Buttons go here later.
            yield Horizontal(id="buttons")
        yield Footer()

    def on_mount(self) -> None:
        self.title = "relay"