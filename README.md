# Relay test report

This branch is the test report site, published with GitHub Pages at
https://cachebag.github.io/relay/. It is separate from `main` and holds no
Relay code.

## Recording a result

Edit `results.csv` here on GitHub (the pencil button) and commit to this
branch. The page updates about a minute later.

Each row is one test case:

| Column | What goes in it |
|---|---|
| `id` | The case ID from the test plan. Don't change it. |
| `tester` | Who runs the case. |
| `verdict` | `Pass`, `Fail`, `Blocked` or `Not run`. |
| `date` | The day it was run, like `2026-11-20`. |
| `build` | The commit or release tag it was run on. |
| `notes` | What happened, or a link to the evidence. Put the text in double quotes if it has a comma. |

`cases.json` holds each case's title, priority and requirements, taken from
Section 5 of the test plan.
