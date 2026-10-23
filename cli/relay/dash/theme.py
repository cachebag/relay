"""One palette for the whole dashboard, so every screen and modal matches."""

from textual.theme import Theme

# Cell colours for the tables. Taken from the theme below so a row looks
# the same whether or not the cursor is on it.
ONLINE = "#7bd88f"
OFFLINE = "#ff6b6b"
JOB_STYLES = {
    "queued": "#f0b35a",
    "assigned": "#7fb4e6",
    "running": "#7fb4e6",
    "done": "#7bd88f",
    "failed": "#ff6b6b",
}

RELAY = Theme(
    name="relay",
    primary="#7fb4e6",  # rules under titles, table cursor, focused input
    secondary="#8aa3b8",
    accent="#f0b35a",  # the relay wordmark and the invite line
    foreground="#e6e9ef",
    background="#0e1116",
    surface="#151a21",
    panel="#1b222b",
    success="#7bd88f",
    warning="#f0b35a",
    error="#ff6b6b",
    dark=True,
    variables={
        "footer-key-foreground": "#f0b35a",
        "footer-description-foreground": "#8aa3b8",
        "footer-background": "#151a21",
        "input-cursor-background": "#7fb4e6",
        "block-cursor-background": "#7fb4e6",
        "block-cursor-foreground": "#0e1116",
    },
)

# Shared rules. Titles get a thin line under them instead of boxes around
# everything, so the eye has one kind of edge to follow.
CSS = """
Screen { background: $background; }

#topbar {
    height: 1;
    background: $surface;
    padding: 0 1;
}
#topbar .wordmark { color: $accent; text-style: bold; width: auto; }
#topbar .where { color: $text; width: 1fr; padding: 0 1; }
#topbar .totals { color: $text-muted; width: auto; }

#invite-bar { height: 1; padding: 0 1; }
#invite-bar .key { color: $text-muted; width: auto; padding-right: 1; }
#invite-bar .invite { color: $accent; width: 1fr; }

.title {
    height: 2;
    margin: 1 1 0 1;
    color: $text;
    text-style: bold;
    border-bottom: solid $primary;
}

DataTable { background: $background; margin: 0 1; }
#nodes { height: auto; max-height: 40%; }
#jobs { height: auto; max-height: 100%; }
DataTable > .datatable--header { background: $background; color: $text-muted; text-style: bold; }
DataTable > .datatable--cursor { background: $primary 25%; }

.empty { height: auto; margin: 0 1; color: $text-muted; }
#error { height: auto; margin: 0 1; color: $error; }

ModalScreen { align: center middle; background: $background 60%; }
.modal {
    width: 90;
    max-width: 100%;
    height: auto;
    background: $surface;
    border: solid $primary;
    padding: 1 2;
}
.modal .title { margin: 0 0 1 0; }
.modal .hint { color: $text-muted; height: auto; margin-bottom: 1; }
.modal .label { color: $text-muted; height: 1; margin-top: 1; }
.modal Input { border: tall $panel; background: $panel; }
.modal Input:focus { border: tall $primary; }
.modal .error { color: $error; height: auto; margin-top: 1; }
.modal .result { height: auto; margin-top: 1; }
.modal .keys { color: $text-muted; height: 1; margin-top: 1; }
"""
