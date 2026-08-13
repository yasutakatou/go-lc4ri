# code-lc4ri (CLI / TUI)

**Run Markdown runbooks from your terminal — split-screen TUI + headless CI runner.**

`code-lc4ri` turns an ordinary Markdown document into an executable, reproducible
operations runbook. List items become shell commands, command output streams back
live, and variables / assertions / retries let a document double as a test.

This is the **standalone Go** implementation. It has **no Node.js dependency** and
ships as a single static binary. It implements the same runbook grammar as the
[code-lc4ri VS Code extension](https://github.com/yasutakatou/code-lc4ri), so a
document behaves identically in the editor and in the terminal.

```
┌─ ① Runbook — runbook.md · F5: run the block at the cursor · Ctrl-S: save ─┐
│   # Deploy                                                                 │
│ ▸ - kubectl get nodes          ← cursor; press F5 to run                   │
│   ```output                       ③ output streamed back in                │
│   NAME     STATUS  ROLES  AGE        as an editable block                  │
│   node-1   Ready   <none> 12d                                              │
│   ```                                                                      │
├─ ② Terminal — zsh · commands run here (F2: focus) ────────────────────────┤
│ $ kubectl get nodes                                                        │
│ NAME       STATUS   ROLES    AGE        ← a real OS shell                  │
│ node-1     Ready    <none>   12d          (live)                           │
├───────────────────────────────────────────────────────────────────────────┤
│ ▶ F5 runs: kubectl get nodes → output to the ```output block               │
│ F1:help F5:run Ctrl-N/Ctrl-P:step Ctrl-S:save F2:switch F9:run-all …       │
└───────────────────────────────────────────────────────────────────────────┘
```

## How it works — 30 seconds

Three things happen, always in this order:

| | | |
|---|---|---|
| **①** | **Runbook** (top pane) | Put the cursor on a step and press **`F5`**. `Ctrl-N` / `Ctrl-P` jump to the next / previous step, so you never scroll looking for one. |
| **②** | **Terminal** (bottom pane) | The step runs there, in a real shell you can also type into yourself. |
| **③** | ` ```output ` **block** | Its output is written **back into the runbook**, right under the step. Then the cursor moves on to the next step by itself. |

A **step** is one of exactly three things — everything else in the document is
documentation that is never executed:

| Runnable | Not runnable |
|---|---|
| `- kubectl get nodes` (list item) | ` ```shell ` / ` ```console ` / a plain ` ``` ` block |
| `1. hostname @ {host}` (numbered, binds `{1}`) | Indented code blocks, prose, headings, tables |
| ` ```bash ` … ` ``` ` (fenced script; ` ```yaml `, `json`, … are written to disk) | Text inside an ` ```output ` block |

If you press `F5` where there is nothing runnable, the status bar says so and
the document is left untouched — no empty ` ```output ` block, no green
"executed" marker. Press `Ctrl-N` to jump to the nearest real step.

> **Note on code blocks.** The examples in this README are wrapped in
> ` ```markdown ` fences *for display only* — copy what is **inside** them.
> A command written inside an ordinary ` ``` ` code block is documentation, not
> a step; use a `- ` list item, or tag the fence ` ```bash ` to execute the
> whole block as a script.

---

## Features

- **Interactive TUI** — an always-editable Markdown editor on top and a live OS
  shell on the bottom. Press `F5` / `Ctrl-R` to run the whole block from the
  cursor to the next boundary through the **full LC4RI engine** (AND-chain, variables,
  assertions, parallel, retry, `write:`, `include:`, …); commands run in the
  visible shell and their output streams **back into the document** as an
  editable ` ```output ` block; `Ctrl-S` saves. `F9` runs the **whole document**
  top to bottom; `F8` **cancels** a run in progress.
- **Step-by-step navigation** — `Ctrl-N` / `Ctrl-P` jump straight to the next /
  previous runnable step, and finishing a run parks the cursor on the step that
  comes after it: a whole runbook can be worked through with `F5` alone, without
  scrolling. The status bar always names the command `F5` would run from where
  the cursor is — and tells you when there is none, instead of running nothing
  silently.
- **Text selection** — hold `Shift` while moving the cursor (or `Shift`+click)
  in the editor to select a range of text, then cut / copy / paste it.
- **Markdown preview** — `F3` renders the document full-screen as styled,
  read-only Markdown (headings, bold/italic, lists, quotes, code fences,
  tables); `Esc` / `F3` returns to the editor.
- **Headless runner** — `code-lc4ri run` for CI / scripting, sharing the exact
  same engine, with a non-zero exit code on any failure and optional report
  export as **HTML**, **Markdown**, **JUnit XML** (`.xml`) or **JSON** (`.json`) —
  each carrying per-command exit codes, durations and a pass/fail summary.
- **AND-chain execution** — indentation expresses dependencies; a step only runs
  if its parent succeeded.
- **Variables** — numbered (`{1}`–`{9}`), named (`@ {name}`), built-ins
  (`{$PREV}`, `{$STATUS}`, `{$CWD}`, …), `.env` loading and interactive `prompt:`.
- **Control flow** — `[parallel]` groups, `[retry: N, interval]`, `assert:`,
  horizon / blank-line section boundaries.
- **File ops** — `write:` a fenced block to disk, ` ```bash ` block execution,
  ` ```yaml file.yml ` auto-write, `include:` another runbook.
- **Safety** — `denyList` / `allowList` / `dangerousPatterns`, with a confirm
  modal in the TUI.
- **Cross-platform** — Linux / macOS / Windows (PowerShell / Git Bash / CMD).

---

## Install

### Build from source

```bash
git clone https://github.com/yasutakatou/go-lc4ri
cd go-lc4ri 
go build -o go-lc4ri .
```

or download binary from [release page](https://github.com/yasutakatou/go-lc4ri/releases). save binary file, copy to entryed execute path directory.

---

## Usage

| Command | What it does |
|---|---|
| `code-lc4ri <file.md>` | Launch the interactive **TUI** (default) |
| `code-lc4ri tui <file.md> [--profile NAME]` | Launch the TUI explicitly |
| `code-lc4ri run <file.md> [options]` | Run **headlessly** (CI / scripting) |
| `code-lc4ri --help` / `--version` | Help / version |

### `run` options

| Option | Description |
|---|---|
| `--dry-run` | Show the resolved commands without executing them |
| `--profile NAME` | Wrap every command with the named profile from config |
| `--report FILE` | Write an execution report; format is chosen by extension: `.html`, `.md`, `.xml` (JUnit) or `.json` |

```bash
code-lc4ri run runbook.md
code-lc4ri run runbook.md --dry-run
code-lc4ri run runbook.md --profile prod-ssh --report report.html
code-lc4ri run runbook.md --report results.xml    # JUnit XML for CI test reporting
code-lc4ri run runbook.md --report results.json   # machine-readable JSON
```

The exit code is non-zero if any command failed or any `assert:` failed, so it
slots into CI directly. A `.xml` report is standard **JUnit** — GitHub Actions,
GitLab and Jenkins render each command as a test case with its duration and, on
failure, its captured output.

---

## The interactive TUI

The TUI is a split screen with two panes plus a two-row status bar. Each pane's
border states its role, so the screen explains itself:

- **① Runbook (top pane) — Markdown editor.** A normal, always-editable text
  editor holding your `.md` document. Type and edit Markdown freely; `Ctrl-S`
  saves to disk. Hold `Shift` while moving the cursor (arrows, Home/End,
  word-jumps) or `Shift`+click to select a range of text — `Ctrl-Q` copies it,
  `Ctrl-X` cuts it, `Ctrl-V` replaces it (or inserts at the cursor if nothing is
  selected).
- **② Terminal (bottom pane) — live OS shell.** A real terminal attached to your
  shell (zsh / bash / PowerShell …) and **where every step actually runs**. When
  focused it behaves like any terminal — keystrokes (incl. `Ctrl-C`) go straight
  to the shell.
- **Status bar** — two rows. The first says what `F5` would do *right now*:
  the command under the cursor (`▶ F5 runs: kubectl get nodes …`), a `running…`
  indicator during a run, a note that the shell has focus, or a warning that
  there is nothing runnable at the cursor. The second is the shortcut reminder,
  ordered so the keys that matter most survive on a narrow terminal, preceded by
  the `*unsaved` flag.

`F2` (or a mouse click) moves focus between the two panes. `Tab` is left for the
focused pane — shell completion in the terminal, indentation in the editor.

### Moving between steps

Reaching the next command never requires scrolling through the documentation
around it:

- **`Ctrl-N` / `Ctrl-P`** jump the cursor to the next / previous runnable step
  (a `- command`, a `1. command` or a ` ```bash ` / ` ```yaml ` fence). Text
  inside ` ```output ` blocks is skipped, so captured output that happens to
  look like a list item is never mistaken for a step. Both keys are consumed
  **only while the editor is focused** — the shell keeps `Ctrl-N` / `Ctrl-P`
  for its own history navigation.
- **After a run finishes the cursor advances by itself** to the first step past
  the ` ```output ` block that was just written. Working through a runbook is
  therefore `F5`, `F5`, `F5` — with `Ctrl-N` available whenever you want to skip
  a step rather than run it.

If there is no step in the requested direction the cursor stays where it is and
the status bar says so.

### Running a block with F5 / Ctrl-R

Put the cursor anywhere in a block of steps (or at its first line) and press
**`F5`** (or **`Ctrl-R`**). Execution runs **from the cursor down to the next boundary** (a blank
line, `***` / `---` horizon, or an output fence) as one batch, driving the
**same LC4RI engine** as headless `run`: the AND-chain, numbered/named variables,
`assert:`, `[parallel]`, `[retry:]`, `prompt:`, `write:`, `include:`, `# env:`
and fenced ` ```bash ` / ` ```yaml ` blocks all apply.

If the region from the cursor to that boundary contains **no runnable step**,
`F5` does nothing at all and says so in the status bar:

````
nothing to run here — a step is - command, 1. command or a ```bash block
````

The document is left byte-for-byte unchanged — no ` ```output ` block appears
and no line is flagged as executed. (The two situations that used to produce a
silent, empty output block: the cursor sitting in prose, and the cursor sitting
*inside* a fenced block instead of on its opening ` ```bash ` line.)

Shell commands run in the visible bottom terminal (a leading `- ` / `1. ` prefix
is stripped), and their output — together with per-command headers, `---`
separators and directive markers — streams **back into the document** in
real-time as a single editable ` ```output ` block placed at the boundary.

Each command is sent to the shell as a [bracketed paste][bp] so the whole
(possibly multi-line) command runs as one unit. That keeps the shell's prompt,
input echo and the internal capture wrapper out of the captured text — the
` ```output ` block holds **only the command's real stdout/stderr**, never a
stray ` printf … ` wrapper line. (Modern POSIX shells and PowerShell/PSReadLine
honour bracketed paste; if a shell ignores it, any leaked wrapper line is
stripped as a fallback.)

[bp]: https://en.wikipedia.org/wiki/Bracketed-paste

Once a block has been run, its **first line is flagged with two leading spaces
and drawn in green**, so on the narrow TUI screen you can tell at a glance how
far down the document you've executed (every line beginning with two spaces is
highlighted):

````markdown
  1. hostname @ {host}          ← two leading spaces, drawn green = already run
- echo deploying to {host}
- kubectl get nodes

```output
[ hostname ] Mon Jun 29 14:32:00 2026
node-1
---
[ echo deploying to node-1 ] Mon Jun 29 14:32:00 2026
deploying to node-1
---
[ kubectl get nodes ] Mon Jun 29 14:32:01 2026
NAME       STATUS   ROLES    AGE
node-1     Ready    <none>   12d
```
````

When the run finishes, the cursor moves down to the **first step after the
output block** so the next `F5` continues where this one left off; if the block
that just ran was the last one in the document, the cursor stays put.

Re-running the same block replaces its previous ` ```output ` block in place and
clears the two-space marker for the run (so the command still parses), then
re-flags it. Two leading spaces are used rather
than a visible glyph like `* ` (a bullet list item) or a leading tab (an
indented code block), either of which would change how the line renders as
Markdown; two spaces leave the command text untouched. If you `Ctrl-S` save, the
markers are written to the file along with the captured output.

Because output is written into the buffer, the document **is** modified in the
TUI — use `Ctrl-S` to persist it (or just don't save to discard the captured
output). Headless `run` never touches the source file.

> A `prompt:` directive opens a modal input box, and a command matching a
> dangerous pattern opens a confirm modal — both block until you answer.
> `[parallel]` groups run sequentially in the TUI (a single visible shell can't
> interleave captures); headless `run` runs them concurrently.

### Running the whole document with F9

Press **`F9`** to run the **entire document** top to bottom — the same as headless
`run`, but in the live shell. Existing ` ```output ` blocks and executed-line
markers are cleared first, then every command's output streams into a single
` ```output ` block appended at the end of the document. (Unavailable on
`cmd.exe`, which can't be captured.)

### Cancelling a run with F8

Press **`F8`** while a block (or a full-document `F9` run) is executing to cancel
it: the AND-chain and any `[retry:]` loop stop advancing, the command currently
running in the shell is interrupted (a `Ctrl-C` is sent), and the captured output
so far is kept and marked `[cancelled]`. `F8` works from either pane, so a long
`[retry: 20, 5s]` wait or a hung command can always be stopped without switching
focus.

### Markdown preview with F3

Press **`F3`** to render the current buffer as styled Markdown, full-screen and
read-only — headings, **bold**/*italic* emphasis, `inline code`, lists,
block quotes, fenced code blocks and GFM tables each get distinct styling.
Tables are redrawn as box-drawn grids with a bold header row and honour
per-column alignment (`:---`, `---:`, `:---:`). It is a snapshot: editing is
blocked while the preview is open. Scroll it with the arrow keys / `PgUp` /
`PgDn` / `Home` / `End`; press **`Esc`** or **`F3`** again to return to the
editor exactly where you left it.

### Keyboard shortcuts

The keys below are the **defaults**. Every one of them is reassignable via
`keybindings` in `~/.go-lc4ri/config.json` — see
[Keybindings](#keybindings) — and the in-app `F1` help overlay / status bar
always reflect whatever is actually configured, not these defaults.

| Key | Action | `keybindings` action name |
|---|---|---|
| `F2` | Switch focus: editor ⇄ terminal | `focus` |
| `Tab` | Passes through to the focused pane (shell completion / editor indent) | — (not bindable) |
| `Shift`+cursor / `Shift`+click | Select a range of text in the editor (`Ctrl-Q` copy, `Ctrl-X` cut, `Ctrl-V` paste) | — (not bindable) |
| `Ctrl-S` | Save the document to disk (any time) | `save` |
| `F5` / `Ctrl-R` | Run the block from the cursor to the next boundary; stream output back into the doc and flag the block's first line with two leading spaces (drawn green) as executed. `Ctrl-R` is consumed only while the editor is focused, so the shell keeps `Ctrl-R` for reverse history search | `run` (`Ctrl-R` is a fixed alias, always active regardless of how `run` is bound) |
| `Ctrl-Enter` | Also runs the block, on terminals that report the modifier (iTerm2 / kitty / …); a plain `Enter` still inserts a newline | — (fixed alias for `run`, not bindable) |
| `Ctrl-N` / `Ctrl-P` | Jump to the next / previous runnable step. Consumed only while the editor is focused, so the shell keeps both keys for history navigation | `nextStep` / `prevStep` |
| `F9` | Run the whole document top to bottom; output appended as one ` ```output ` block at the end | `runAll` |
| `F8` | Cancel the running block / document run (stops the AND-chain and retries, interrupts the current command) | `cancel` |
| `F3` | Toggle the read-only Markdown preview (dismiss with `Esc` or `F3`) | `preview` |
| `F6` / `F7` | Shrink / grow the terminal pane (widen / narrow the editor) | `resizeShrink` / `resizeGrow` |
| mouse click | Focus a pane | — (not bindable) |
| `F1` | Help overlay (dismiss with `Esc` or `F1`) | `help` |
| `F10` | Quit (or type `exit` in the shell) | `quit` |

> On macOS, `Ctrl-↑` / `Ctrl-↓` are reserved by the system (Mission Control /
> App Exposé), so resize is bound to **`F6` / `F7`**. `Ctrl-↑/↓` still works as a
> fixed fallback on terminals that deliver those keys to the application,
> regardless of how `resizeShrink` / `resizeGrow` are configured.

---

## Runbook syntax

A runbook is plain Markdown. The constructs below are interpreted at run time;
everything else is treated as documentation.

### List commands & the AND-chain

A list item is a command. Indentation (2 spaces = one level) expresses
dependency — an indented command only runs if its parent succeeded.

```markdown
- echo a            ← always runs
  - echo b          ← runs only if a succeeded
    - echo c        ← runs only if b succeeded
- echo d            ← top level: always runs
```

### Section boundaries

In **run-from-cursor** mode, execution stops at the first boundary once at least
one command has run:

| Boundary | Meaning |
|---|---|
| `***` or `---` (3+ chars) | Horizon separator |
| Blank line | Stops execution at that point |
| Closing ` ``` ` output fence | End of an output block |

### Numbered variables & bindings

```markdown
1. hostname @ {host}      ← stores stdout in {1} and {host}
- echo working on {host}
- ls @ {files}            ← bind a list command's output to {files}
```

The binding operator is **`@`** — a single half-width character that is easy to
type and, unlike `>` / `<` / `|`, never collides with a command's own pipeline
or redirection. It must be preceded by a space, so an `ssh user@{host}` target is
left untouched. The full-width arrow `→` and the ASCII `->` are still accepted for
backward compatibility.

Variables (`1`–`9` and `{name}`) and built-ins are expanded in any command:

| Token | Value |
|---|---|
| `{$PREV}` | stdout of the previous command |
| `{$STATUS}` | exit code of the previous command |
| `{$CWD}` / `{$USER}` / `{$HOST}` / `{$DATE}` | runtime values |

### Assertions

```markdown
- curl -s http://api.local/health
    - assert: contains "ok"
    - assert: status == 0
    - assert: regex /version: \d+/
```

A failed assertion breaks the AND-chain just like a failed command.

### Parallel & retry

```markdown
- [parallel] ssh server1 uptime
- [parallel] ssh server2 uptime

- [retry: 5, 2s] kubectl rollout status deployment/app
```

`[retry: N]` re-runs up to N times; `, interval` waits between attempts
(`500` = ms, `2s` = seconds). Combine `[parallel]` and `[retry:]` in any order.

### Prompt, env & include

```markdown
# env: .env.prod                 ← load KEY=VALUE pairs into variables
- prompt: {TARGET} Enter host    ← ask interactively (add `secret` to mask)
- prompt: secret {PASS} Password
- include: setup.md              ← inline another runbook (bindings propagate)
- ssh {TARGET} uptime
```

### File output

```markdown
- write: output/config.yaml
  ```yaml
  host: {DB_HOST}
  ```
```

````markdown
```bash
echo "run this whole block in one shell"
curl -sL https://example.com/install.sh | sh
```

```yaml config/app.yml
name: demo          ← auto-written to config/app.yml
```
````

**The fence's language tag decides what happens** — this is the single most
common surprise, so the full list:

| Fence | What the engine does |
|---|---|
| ` ```bash ` ` ```sh ` ` ```zsh ` | **Executed** as one script in the shell |
| ` ```yaml ` `json` `conf` `ini` `toml` | **Written to disk**, to the filename in the fence info string (` ```yaml config/app.yml `) or an auto-generated name |
| ` ```output ` | The block this tool writes captured output into; also a section boundary |
| anything else — ` ```shell `, ` ```console `, ` ```text `, or a bare ` ``` ` | **Documentation.** Not executed, not written, silently skipped |

So a command that "does nothing" inside a code block is almost always a fence
tagged something other than `bash` / `sh` / `zsh` — or a bare ` ``` `. Either
retag the fence, or write the command as a `- ` list item.

Put the cursor on the fence's **opening line** to run a fenced block with `F5`
(`Ctrl-N` lands there for you). From inside the block there is nothing to run,
and the status bar will say so.

### Terminal passthrough & file open

```markdown
- ! command          ← run in the active terminal (output captured like any step)
- open: notes.md     ← editor-only; reported as skipped in the CLI
```

---

## Configuration

`code-lc4ri` reads `~/.go-lc4ri/config.json` — its own file, kept separate from
the VS Code extension's `~/.code-lc4ri`. On first run, if the file is missing it
is auto-generated under `~/.go-lc4ri/` populated with the defaults below, ready
to edit. All keys are optional.

```jsonc
{
  "timeout": 10000,                       // inactivity timeout per command (ms)
  "profiles": {                           // chosen with `tui --profile NAME`
    "prod-ssh": "ssh ops@prod {COMMAND}",
    "docker":   "docker exec -i app sh -c \"{COMMAND}\""
  },
  "template": {                           // per-OS wrapper when no profile is active
    "linux":  "ssh ops@prod {COMMAND}",
    "win32":  "wsl -e {COMMAND}"
  },
  "changeWord": { "#HOME#": "/home/user" }, // pre→post substitution map
  "shell": null,                          // null=auto | "bash" | "powershell" | "cmd"
  "dangerousPatterns": [],                // regexes that prompt a confirm modal
  "allowList": [],                        // if non-empty, only matching commands run
  "denyList": [],                         // matching commands never run
  "confirmDangerous": true,               // show the confirm modal in the TUI
  "keybindings": {                        // TUI shortcut overrides — see below
    "quit": "F10",
    "help": "F1",
    "preview": "F3",
    "save": "Ctrl-S",
    "run": "F5",
    "runAll": "F9",
    "cancel": "F8",
    "focus": "F2",
    "resizeShrink": "F6",
    "resizeGrow": "F7"
  }
}
```

A sensible default `dangerousPatterns` list ships built-in
(`rm -rf /`, `dd if=`, `mkfs.`, fork bombs, `curl | sh`, Windows `format`/`del`,
`Remove-Item -Recurse -Force`, …).

### Keybindings

`keybindings` reassigns the TUI's shortcut keys. It only needs the actions
you want to change — anything omitted keeps its built-in default (the values
shown above). The in-app `F1` help overlay and the status bar always reflect
the resolved bindings, not the hardcoded defaults.

| Action | Default | Does |
|---|---|---|
| `quit` | `F10` | Quit the TUI |
| `help` | `F1` | Toggle the help overlay |
| `preview` | `F3` | Toggle the read-only Markdown preview |
| `save` | `Ctrl-S` | Save the document to disk |
| `run` | `F5` | Run the block from the cursor to the next boundary |
| `runAll` | `F9` | Run the whole document top to bottom |
| `cancel` | `F8` | Cancel the running block / document run |
| `focus` | `F2` | Switch focus between the editor and the terminal |
| `resizeShrink` | `F6` | Shrink the terminal pane |
| `resizeGrow` | `F7` | Grow the terminal pane |
| `nextStep` | `Ctrl-N` | Jump to the next runnable step (editor focus only) |
| `prevStep` | `Ctrl-P` | Jump to the previous runnable step (editor focus only) |

A key spec is a function key (`F1`–`F12`), a named key (`Esc`, `Enter`, `Tab`,
`Up`/`Down`/`Left`/`Right`, `Home`/`End`, `PgUp`/`PgDn`, `Backspace`,
`Delete`), a `Ctrl-` + letter combo (`Ctrl-A`–`Ctrl-Z`), or a single printable
character. Printable-character bindings intercept that character globally, so
they will interfere with typing in the editor — function keys and `Ctrl-`
combos are recommended.

`Ctrl-R` and `Ctrl-Enter` are fixed convenience aliases for `run` and are not
reassignable; avoid binding another action to them, since that action would
take priority and shadow the alias.

**Two actions cannot share the same key.** If `keybindings` resolves to a
duplicate assignment (or names an unknown action, or an unparsable key spec),
`code-lc4ri` prints an error and exits **before the TUI starts** — it never
launches with an ambiguous keymap.

### Notes

- **Timeout** is an *inactivity* timeout: it resets every time new output
  arrives, so long-running commands that keep printing are not killed.
- In headless `run` mode the dangerous-command confirmation is disabled (CI never
  blocks on a modal); `denyList` / `allowList` still apply. The TUI shows the
  modal.
- `cd` and `export` are tracked so the working directory and exported variables
  persist across commands. In the TUI the shell is the source of truth: the
  real `$PWD` is captured after every command, so `write:` and `{$CWD}` honour a
  `cd` done anywhere — in a list step, a compound `cd x && …`, a ` ```bash `
  block, or interactively — and the directory carries across successive `F5`
  runs.

---

## Development

```bash
make build      # go build -o code-lc4ri .
make run FILE=path/to/runbook.md
make fmt        # gofmt -w *.go
make vet        # go vet ./...
make clean
```

| File | Role |
|---|---|
| `parser.go` | Runbook grammar (lists, numbered vars, directives, fences) |
| `config.go` | `~/.go-lc4ri/config.json` loading + first-run auto-generation |
| `keybindings.go` | Parses/resolves the `keybindings` config into TUI key bindings; validates for duplicates |
| `engine.go` | Execution engine (AND-chain, parallel, retry, streaming, security) |
| `tui.go` | tview / tcell terminal UI |
| `preview.go` | Markdown → styled tview text for the `F3` preview screen |
| `headless.go` | `run` subcommand + report export |
| `main.go` | Argument parsing / entry point |
| `proc_unix.go`, `proc_windows.go` | Process-group termination per OS |

---

## License

MIT License — see [LICENSE](./LICENSE).

## Credits

- [yasutakatou](https://github.com/yasutakatou)
- TUI built with [tview](https://github.com/rivo/tview) / [tcell](https://github.com/gdamore/tcell).
