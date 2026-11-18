# Test report

This branch is the test report site and holds no Relay code.

The test report is a page that shows the verdict for every test case in the
test plan: https://cachebag.github.io/relay/

It reads one file, `results.csv`, on the `test-report` branch. To record a
result you edit that file. No pull request is needed.

## Record a result

1. Open [results.csv](https://github.com/cachebag/relay/edit/test-report/results.csv).
2. Find the row for your test case and fill it in.
3. Click **Commit changes** and commit to the `test-report` branch.

The page updates about a minute later.

## What a row looks like

```
id,tester,verdict,date,build,notes
UT-001,Omar Almasri,Pass,2026-11-18,v0.3.3,CI run passed
```

| Column | What to put |
|---|---|
| `id` | The test case ID. Don't change it. |
| `tester` | Your name. |
| `verdict` | `Pass`, `Fail`, `Blocked` or `Not run`. |
| `date` | The day you ran it. |
| `build` | The release tag or commit you ran it on. |
| `notes` | What happened, or a link to a log or screenshot. |

If your note has a comma in it, put the whole note in double quotes:

```
ST-008,Rylan van de Wetering,Blocked,2026-11-19,v0.3.3,"Tailscale was down, will rerun"
```

## Which verdict to use

- **Pass**: everything in the case's expected result happened.
- **Fail**: something in the expected result did not happen. Open a GitHub
  issue and put its link in the notes.
- **Blocked**: you could not run the case because of something outside
  Relay, like no internet.
- **Not run**: nobody has tried it yet.

## Changing a case's title

Titles and requirements come from `cases.json` on the same branch. Edit it the
same way if a case is renamed in the test plan.
