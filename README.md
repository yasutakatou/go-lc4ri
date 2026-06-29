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
┌─ runbook.md — Markdown (Ctrl-S save) ─────────────────────┐
│   # Deploy                                                 │
│ ▸ - kubectl get nodes          ← cursor; press F5 to run   │
│   ```output                       output streamed back in  │
│   NAME     STATUS  ROLES  AGE     as an editable block     │
│   node-1   Ready   <none> 12d                              │
│   ```                                                      │
├─ terminal — zsh ──────────────────────────────────────────┤
│ $ kubectl get nodes                                        │
│ NAME       STATUS   ROLES    AGE        ← a real OS shell  │
│ node-1     Ready    <none>   12d          (live)           │
├───────────────────────────────────────────────────────────┤
│ focus:editor   F2:switch Ctrl-S:save F5:run→reflect …      │
└───────────────────────────────────────────────────────────┘
```

---

## Features

- **Interactive TUI** — an always-editable Markdown editor on top and a live OS
  shell on the bottom. Press `F5` to run the whole block from the cursor to the
  next boundary through the **full LC4RI engine** (AND-chain, variables,
  assertions, parallel, retry, `write:`, `include:`, …); commands run in the
  visible shell and their output streams **back into the document** as an
  editable ` ```output ` block; `Ctrl-S` saves.
- **Headless runner** — `code-lc4ri run` for CI / scripting, sharing the exact
  same engine, with a non-zero exit code on any failure and optional HTML /
  Markdown report export.
- **AND-chain execution** — indentation expresses dependencies; a step only runs
  if its parent succeeded.
- **Variables** — numbered (`{1}`–`{9}`), named (`→ {name}`), built-ins
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
git clone https://github.com/yasutakatou/code-lc4ri-cli
cd code-lc4ri-cli
go build -o code-lc4ri .      # produces ./code-lc4ri
# or
make build
```

Requires **Go 1.24+**. Dependencies (`tview` / `tcell`) are fetched automatically.

### go install

```bash
go install github.com/yasutakatou/code-lc4ri/cli@latest
# or, from a clone:
make install                  # installs to $GOBIN
```

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
| `--report FILE` | Write an execution report (`.html` or `.md`) |

```bash
code-lc4ri run runbook.md
code-lc4ri run runbook.md --dry-run
code-lc4ri run runbook.md --profile prod-ssh --report report.html
```

The exit code is non-zero if any command failed or any `assert:` failed, so it
slots into CI directly.

---

## The interactive TUI

The TUI is a split screen with two panes plus a status bar:

- **Top pane — Markdown editor.** A normal, always-editable text editor holding
  your `.md` document. Type and edit Markdown freely; `Ctrl-S` saves to disk.
- **Bottom pane — live OS shell.** A real terminal attached to your shell
  (zsh / bash / PowerShell …). When focused it behaves like any terminal —
  keystrokes (incl. `Ctrl-C`) go straight to the shell.
- **Status bar** — current focus, an unsaved/running indicator and a one-line
  shortcut reminder.

`F2` (or a mouse click) moves focus between the two panes. `Tab` is left for the
focused pane — shell completion in the terminal, indentation in the editor.

### Running a block with F5

Put the cursor anywhere in a block of steps (or at its first line) and press
**`F5`**. Execution runs **from the cursor down to the next boundary** (a blank
line, `***` / `---` horizon, or an output fence) as one batch, driving the
**same LC4RI engine** as headless `run`: the AND-chain, numbered/named variables,
`assert:`, `[parallel]`, `[retry:]`, `prompt:`, `write:`, `include:`, `# env:`
and fenced ` ```bash ` / ` ```yaml ` blocks all apply.

Shell commands run in the visible bottom terminal (a leading `- ` / `1. ` prefix
is stripped), and their output — together with per-command headers, `---`
separators and directive markers — streams **back into the document** in
real-time as a single editable ` ```output ` block placed at the boundary:

````markdown
1. hostname → {host}
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

Re-running the same block replaces its previous ` ```output ` block in place, and
focus stays on the cursor line.

Because output is written into the buffer, the document **is** modified in the
TUI — use `Ctrl-S` to persist it (or just don't save to discard the captured
output). Headless `run` never touches the source file.

> A `prompt:` directive opens a modal input box, and a command matching a
> dangerous pattern opens a confirm modal — both block until you answer.
> `[parallel]` groups run sequentially in the TUI (a single visible shell can't
> interleave captures); headless `run` runs them concurrently.

### Keyboard shortcuts

| Key | Action |
|---|---|
| `F2` | Switch focus: editor ⇄ terminal |
| `Tab` | Passes through to the focused pane (shell completion / editor indent) |
| `Ctrl-S` | Save the document to disk (any time) |
| `F5` | Run the block from the cursor to the next boundary; stream output back into the doc |
| `F6` / `F7` | Shrink / grow the terminal pane (widen / narrow the editor) |
| mouse click | Focus a pane |
| `F1` | Help overlay (dismiss with `Esc` or `F1`) |
| `F10` | Quit (or type `exit` in the shell) |

> On macOS, `Ctrl-↑` / `Ctrl-↓` are reserved by the system (Mission Control /
> App Exposé), so resize is bound to **`F6` / `F7`**. `Ctrl-↑/↓` still works as a
> fallback on terminals that deliver those keys to the application.

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
1. hostname → {host}      ← stores stdout in {1} and {host}
- echo working on {host}
- ls → {files}            ← bind a list command's output to {files}
```

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

A ` ```bash ` / ` ```sh ` / ` ```zsh ` block is executed; a ` ```yaml `,
`json`, `conf`, `ini` or `toml` block is written to the filename in the fence
info string (or an auto-generated name if omitted).

### Terminal passthrough & file open

```markdown
- ! command          ← run in the active terminal (output captured like any step)
- open: notes.md     ← editor-only; reported as skipped in the CLI
```

---

## Configuration

`code-lc4ri` reads `~/.code-lc4ri/config.json` (the same file the VS Code
extension uses). All keys are optional.

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
  "confirmDangerous": true                // show the confirm modal in the TUI
}
```

A sensible default `dangerousPatterns` list ships built-in
(`rm -rf /`, `dd if=`, `mkfs.`, fork bombs, `curl | sh`, Windows `format`/`del`,
`Remove-Item -Recurse -Force`, …).

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
| `config.go` | `~/.code-lc4ri/config.json` loading |
| `engine.go` | Execution engine (AND-chain, parallel, retry, streaming, security) |
| `tui.go` | tview / tcell terminal UI |
| `headless.go` | `run` subcommand + report export |
| `main.go` | Argument parsing / entry point |
| `proc_unix.go`, `proc_windows.go` | Process-group termination per OS |

---

## License

MIT License — see [LICENSE](./LICENSE).

## Credits

- [yasutakatou](https://github.com/yasutakatou)
- TUI built with [tview](https://github.com/rivo/tview) / [tcell](https://github.com/gdamore/tcell).
