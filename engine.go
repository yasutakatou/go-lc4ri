package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ReportEntry records a single executed command for report export / timeline.
type ReportEntry struct {
	Command string
	Output  string
	Code    int
	Ts      time.Time
	OK      bool
}

// ExecResult is the outcome of running one shell command.
type ExecResult struct {
	Stdout   string
	Stderr   string
	Code     int
	TimedOut bool
	Cwd      string // shell's working directory after the command (live-terminal mode)
}

// RunOptions carries per-run callbacks and flags. The TUI and the headless
// runner provide different implementations of these hooks.
type RunOptions struct {
	DryRun     bool
	Profile    string
	OnCommand  func(cmd string)                             // before each command
	OnOutput   func(chunk string)                           // streaming stdout/stderr
	OnInfo     func(text string)                            // markers (assert, retry…)
	Prompt     func(msg string, secret bool) (string, bool) // interactive input
	ConfirmRun func(cmd, pattern string) bool               // dangerous-command gate

	// ExecCommand, when non-nil, runs a fully-resolved shell command somewhere
	// other than a private subprocess and returns its captured result. The TUI
	// sets this so commands run in the visible terminal (with output streamed
	// straight back into the document) instead of an invisible child process.
	ExecCommand func(cmd string) ExecResult
}

func (o RunOptions) command(s string) {
	if o.OnCommand != nil {
		o.OnCommand(s)
	}
}
func (o RunOptions) output(s string) {
	if o.OnOutput != nil {
		o.OnOutput(s)
	}
}
func (o RunOptions) info(s string) {
	if o.OnInfo != nil {
		o.OnInfo(s)
	}
}

// Engine runs a parsed LC4RI runbook, tracking variables, working directory
// and exported environment across commands.
type Engine struct {
	Cfg      Config
	Vars     *Variables
	Entries  []ReportEntry
	Cwd      string
	Env      map[string]string
	TabWidth int

	mu        sync.Mutex
	current   *exec.Cmd
	cancelled bool
}

// NewEngine creates an engine whose initial working directory is dir.
func NewEngine(cfg Config, dir string) *Engine {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	return &Engine{
		Cfg:      cfg,
		Vars:     NewVariables(),
		Cwd:      dir,
		Env:      env,
		TabWidth: DefaultIndentSpaces,
	}
}

// Cancel terminates the currently running command, if any.
func (e *Engine) Cancel() {
	e.mu.Lock()
	e.cancelled = true
	cmd := e.current
	e.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		killProcessGroup(cmd)
	}
}

// ResetCancel clears the cancelled flag before a fresh run.
func (e *Engine) ResetCancel() {
	e.mu.Lock()
	e.cancelled = false
	e.mu.Unlock()
}

func (e *Engine) isCancelled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancelled
}

// envSlice renders the tracked environment as a KEY=VALUE slice.
func (e *Engine) envSlice() []string {
	out := make([]string, 0, len(e.Env))
	for k, v := range e.Env {
		out = append(out, k+"="+v)
	}
	return out
}

// shellInvocation chooses the shell and arguments for the current platform.
func (e *Engine) shellInvocation(cmd string) (string, []string) {
	shell := ""
	if e.Cfg.Shell != nil {
		shell = *e.Cfg.Shell
	}
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "powershell"
		} else {
			shell = "bash"
		}
	}
	switch shell {
	case "powershell":
		return "powershell", []string{"-NoProfile", "-Command", cmd}
	case "cmd":
		return "cmd", []string{"/C", cmd}
	case "bash":
		if runtime.GOOS == "windows" {
			return "bash", []string{"-c", cmd}
		}
		return "/bin/bash", []string{"-c", cmd}
	default:
		return "/bin/sh", []string{"-c", cmd}
	}
}

// execShell runs a single command, streaming output and honouring the
// inactivity timeout and cancellation.
func (e *Engine) execShell(cmd string, opts RunOptions) ExecResult {
	// Live-terminal mode: hand the command to the visible shell and let the
	// caller capture (and stream) its output. No private subprocess is spawned.
	if opts.ExecCommand != nil {
		return opts.ExecCommand(cmd)
	}
	name, args := e.shellInvocation(cmd)
	c := exec.Command(name, args...)
	c.Dir = e.Cwd
	c.Env = e.envSlice()
	setProcAttr(c)

	stdout, _ := c.StdoutPipe()
	stderr, _ := c.StderrPipe()

	if err := c.Start(); err != nil {
		out := fmt.Sprintf("[error] %v", err)
		opts.output(out + "\n")
		return ExecResult{Stderr: out, Code: -1}
	}

	e.mu.Lock()
	e.current = c
	e.mu.Unlock()

	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	timedOut := false

	// Inactivity timeout: reset on every chunk of output.
	idle := time.Duration(e.Cfg.Timeout) * time.Millisecond
	var timer *time.Timer
	if idle > 0 {
		timer = time.AfterFunc(idle, func() {
			timedOut = true
			killProcessGroup(c)
		})
	}
	reset := func() {
		if timer != nil {
			timer.Reset(idle)
		}
	}

	pump := func(r io.Reader, buf *strings.Builder) {
		defer wg.Done()
		br := bufio.NewReader(r)
		chunk := make([]byte, 4096)
		for {
			n, err := br.Read(chunk)
			if n > 0 {
				reset()
				s := string(chunk[:n])
				buf.WriteString(s)
				opts.output(s)
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout, &outBuf)
	go pump(stderr, &errBuf)
	wg.Wait()
	err := c.Wait()
	if timer != nil {
		timer.Stop()
	}

	e.mu.Lock()
	e.current = nil
	e.mu.Unlock()

	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	if timedOut {
		opts.output(fmt.Sprintf("\n[timeout after %dms]\n", e.Cfg.Timeout))
	}
	return ExecResult{Stdout: outBuf.String(), Stderr: errBuf.String(), Code: code, TimedOut: timedOut}
}

// resolvePath resolves p relative to the engine's tracked working directory.
func (e *Engine) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(e.Cwd, p)
}

// trackCdExport intercepts pure `cd` / `export` commands so directory and
// environment state persist across commands without a shared shell session.
// Returns true if the command was handled internally.
func (e *Engine) trackCdExport(cmd string) (bool, string) {
	trimmed := strings.TrimSpace(cmd)
	// Reject compound statements — let the shell handle them.
	if strings.ContainsAny(trimmed, "&|;") {
		return false, ""
	}
	if m := reCdCommand.FindStringSubmatch(trimmed); m != nil {
		target := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		dest := e.resolvePath(target)
		if st, err := os.Stat(dest); err == nil && st.IsDir() {
			e.Cwd = filepath.Clean(dest)
			e.Vars.Cwd = e.Cwd
			return true, e.Cwd
		}
		return true, "[cd: no such directory: " + target + "]"
	}
	if m := reExportCmd.FindStringSubmatch(trimmed); m != nil {
		val := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		e.Env[m[1]] = val
		return true, ""
	}
	return false, ""
}

// mirrorCdExport updates the engine's tracked cwd / env from a pure cd or export
// command without intercepting it, so the command still runs in the live
// terminal while {$CWD} / {var} / write: paths remain accurate. Compound
// statements are ignored (the shell handles them; we don't try to track them).
func (e *Engine) mirrorCdExport(cmd string) {
	trimmed := strings.TrimSpace(cmd)
	if strings.ContainsAny(trimmed, "&|;") {
		return
	}
	if m := reCdCommand.FindStringSubmatch(trimmed); m != nil {
		target := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		dest := e.resolvePath(target)
		if st, err := os.Stat(dest); err == nil && st.IsDir() {
			e.Cwd = filepath.Clean(dest)
			e.Vars.Cwd = e.Cwd
		}
		return
	}
	if m := reExportCmd.FindStringSubmatch(trimmed); m != nil {
		e.Env[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `"'`)
	}
}

// runOneCommand resolves, secures and executes a single command body, records
// the result and updates {$PREV}/{$STATUS}. It returns the exit code.
func (e *Engine) runOneCommand(body string, opts RunOptions) int {
	sub := ApplyChangeWord(SubstituteVars(body, e.Vars), e.Cfg.ChangeWord)
	final := ApplyTemplate(sub, e.Cfg, opts.Profile)
	opts.command(final)

	if opts.DryRun {
		opts.output("[dry-run] " + final + "\n")
		e.Entries = append(e.Entries, ReportEntry{Command: final, Output: "[dry-run]", Code: 0, Ts: time.Now(), OK: true})
		return 0
	}

	verdict := CheckSecurity(final, e.Cfg)
	if !verdict.OK {
		opts.output("[blocked] " + verdict.Reason + "\n")
		e.Entries = append(e.Entries, ReportEntry{Command: final, Output: "[blocked] " + verdict.Reason, Code: 1, Ts: time.Now(), OK: false})
		e.Vars.Status = 1
		return 1
	}
	if verdict.Dangerous != "" && e.Cfg.ConfirmDangerous {
		allow := true
		if opts.ConfirmRun != nil {
			allow = opts.ConfirmRun(final, verdict.Dangerous)
		}
		if !allow {
			opts.output("[cancelled: dangerous pattern /" + verdict.Dangerous + "/]\n")
			e.Vars.Status = 1
			return 1
		}
	}

	// Live-terminal mode: run everything (including cd / export) in the visible
	// shell so the user sees it, but mirror cd / export into the engine's tracked
	// state so {$CWD} / {var} / write: paths stay correct.
	if opts.ExecCommand != nil {
		e.mirrorCdExport(final)
		r := e.execShell(final, opts)
		// The shell is the source of truth for the working directory: sync to the
		// $PWD it reported so write: / {$CWD} honour cd done in scripts, bash
		// blocks, compound commands or interactively.
		if r.Cwd != "" {
			e.Cwd = r.Cwd
			e.Vars.Cwd = r.Cwd
		}
		out := r.Stdout
		if r.Stderr != "" {
			out += "\n[stderr]\n" + r.Stderr
		}
		e.Vars.Prev = r.Stdout
		e.Vars.Status = r.Code
		e.Entries = append(e.Entries, ReportEntry{Command: final, Output: out, Code: r.Code, Ts: time.Now(), OK: r.Code == 0})
		return r.Code
	}

	// Intercept cd / export so state persists.
	if handled, msg := e.trackCdExport(final); handled {
		if msg != "" {
			opts.output(msg + "\n")
		}
		code := 0
		if strings.HasPrefix(msg, "[cd:") {
			code = 1
		}
		e.Vars.Prev = ""
		e.Vars.Status = code
		e.Entries = append(e.Entries, ReportEntry{Command: final, Output: msg, Code: code, Ts: time.Now(), OK: code == 0})
		return code
	}

	r := e.execShell(final, opts)
	out := r.Stdout
	if r.Stderr != "" {
		out += "\n[stderr]\n" + r.Stderr
	}
	e.Vars.Prev = r.Stdout
	e.Vars.Status = r.Code
	e.Entries = append(e.Entries, ReportEntry{Command: final, Output: out, Code: r.Code, Ts: time.Now(), OK: r.Code == 0})
	return r.Code
}

// runWithRetry runs a command honouring an optional [retry: N, interval] flag.
func (e *Engine) runWithRetry(body string, rf RetryFlag, opts RunOptions) int {
	code := e.runOneCommand(body, opts)
	for attempt := 1; code != 0 && attempt <= rf.Count && !e.isCancelled(); attempt++ {
		if rf.Interval > 0 {
			opts.info(fmt.Sprintf("[retry %d/%d wait %dms...]", attempt, rf.Count, rf.Interval.Milliseconds()))
			time.Sleep(rf.Interval)
		} else {
			opts.info(fmt.Sprintf("[retry %d/%d...]", attempt, rf.Count))
		}
		code = e.runOneCommand(body, opts)
	}
	return code
}

// boundary classifies a line as a chain-stopping separator for run-from-cursor.
func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// Run executes the runbook lines starting at startIdx. When stopAtBoundary is
// true (interactive run-from-cursor) execution halts at the first horizon,
// blank line or output fence once at least one command has run. It returns the
// number of failed commands/assertions.
func (e *Engine) Run(lines []string, startIdx int, stopAtBoundary bool, opts RunOptions) int {
	e.ResetCancel()
	failures := 0
	execCount := 0
	ranAny := false

	for i := startIdx; i < len(lines); i++ {
		if e.isCancelled() {
			opts.info("[cancelled]")
			break
		}
		raw := lines[i]

		// Horizon separator.
		if HorizonCheck(raw) {
			if ranAny && stopAtBoundary {
				break
			}
			execCount = 0
			continue
		}
		// Blank line boundary (v1.5.3).
		if isBlank(raw) {
			if ranAny && stopAtBoundary {
				break
			}
			continue
		}

		// Env directive: # env: <path>
		if m := reEnvDirective.FindStringSubmatch(raw); m != nil {
			e.loadEnvFile(strings.TrimSpace(m[1]), opts)
			continue
		}

		// Numbered assignment: N. cmd  → binds {N}
		if num, ok := DetectNumbered(raw); ok {
			body, bind := ExtractBinding(num.Body)
			code := e.runOneCommand(body, opts)
			val := strings.TrimSpace(e.Vars.Prev)
			e.Vars.Num[num.Idx] = val
			if bind != "" {
				e.Vars.Named[bind] = val
			}
			ranAny = true
			if code != 0 {
				failures++
			}
			continue
		}

		// Fenced code block (```bash exec / ```yaml auto-write / output block).
		if reFenceOpen.MatchString(raw) {
			fb, ok := CollectFencedBlock(lines, i)
			if !ok {
				continue
			}
			lang := fenceLang(fb.Info)
			switch {
			case lang == "bash" || lang == "sh" || lang == "zsh":
				if e.runFencedScript(fb, opts) != 0 {
					failures++
				}
				ranAny = true
				i += fb.Consumed - 1
				continue
			case isConfigLang(lang):
				e.autoWriteBlock(fb, lang, opts)
				ranAny = true
				i += fb.Consumed - 1
				continue
			default:
				// A plain output block acts as a boundary after commands ran.
				if ranAny && stopAtBoundary {
					return failures
				}
				i += fb.Consumed - 1
				continue
			}
		}

		// AND-chain list-command handling (normalise spaces → tabs first).
		norm := NormalizeIndent(raw, e.TabWidth)
		if !RegTab(execCount).MatchString(norm) {
			execCount = 0
		}
		depthRe := RegTab(execCount)
		if !depthRe.MatchString(norm) {
			continue
		}

		// write: directive (consumes the following fenced block).
		if wd, ok := ParseWriteDirective(norm); ok && wd.Depth == execCount {
			consumed := e.handleWrite(wd, lines, i, opts)
			ranAny = true
			execCount++
			i += consumed
			continue
		}

		// prompt: directive.
		if pd, ok := ParsePromptDirective(norm); ok && pd.Depth == execCount {
			ok2 := e.handlePrompt(pd, opts)
			ranAny = true
			if ok2 {
				execCount++
			} else {
				execCount = 0
				failures++
			}
			continue
		}

		body := depthRe.ReplaceAllString(norm, "")
		noPar, parallel := DetectParallelFlag(body)

		// include: another runbook.
		if reInclude.MatchString(noPar) {
			inc := strings.TrimSpace(reInclude.ReplaceAllString(noPar, ""))
			failures += e.handleInclude(inc, opts)
			ranAny = true
			execCount++
			continue
		}
		// open: VS Code only — informational in the CLI.
		if reOpen.MatchString(noPar) {
			opts.info("[open: " + strings.TrimSpace(reOpen.ReplaceAllString(noPar, "")) + " — skipped in CLI]")
			execCount++
			continue
		}
		// terminal passthrough: - ! command
		if reBang.MatchString(noPar) {
			cmd := strings.TrimSpace(reBang.ReplaceAllString(noPar, ""))
			code := e.runOneCommand(cmd, opts)
			ranAny = true
			if code != 0 {
				failures++
			}
			execCount = okStep(execCount, code)
			continue
		}
		// assert: directive.
		if a, ok := ParseAssert(noPar); ok {
			pass := e.evalAssert(a)
			if pass {
				opts.info("✓ assert: " + noPar)
			} else {
				opts.info("✗ ASSERT FAILED: " + noPar)
				failures++
				execCount = 0
			}
			ranAny = true
			continue
		}

		// Parallel group: collect consecutive [parallel] lines at this depth.
		if parallel {
			items := []string{noPar}
			j := i + 1
			for j < len(lines) {
				njorm := NormalizeIndent(lines[j], e.TabWidth)
				if !depthRe.MatchString(njorm) {
					break
				}
				nbody := depthRe.ReplaceAllString(njorm, "")
				nb, np := DetectParallelFlag(nbody)
				if !np {
					break
				}
				items = append(items, nb)
				j++
			}
			i = j - 1
			allOK := e.runParallel(items, opts)
			ranAny = true
			if !allOK {
				failures++
				execCount = 0
			} else {
				execCount++
			}
			continue
		}

		// Regular command (with optional [retry: ...]).
		cmd, rf := DetectRetryFlag(noPar)
		code := e.runWithRetry(cmd, rf, opts)
		ranAny = true
		if code != 0 {
			failures++
		}
		execCount = okStep(execCount, code)
	}
	return failures
}

// FindBlockBoundary returns the line index at which an interactive run started
// at startIdx should place its output block: the first horizon rule, blank line
// or plain (output) code fence reached after at least one command/directive
// line. Fenced exec / config blocks and write: directive blocks inside the
// command region are skipped, mirroring the boundary decisions made by Run.
func FindBlockBoundary(lines []string, startIdx, tabWidth int) int {
	if tabWidth <= 0 {
		tabWidth = DefaultIndentSpaces
	}
	seen := false
	for i := startIdx; i < len(lines); i++ {
		raw := lines[i]
		if HorizonCheck(raw) {
			if seen {
				return i
			}
			continue
		}
		if isBlank(raw) {
			if seen {
				return i
			}
			continue
		}
		if reEnvDirective.MatchString(raw) {
			seen = true
			continue
		}
		if _, ok := DetectNumbered(raw); ok {
			seen = true
			continue
		}
		if reFenceOpen.MatchString(raw) {
			fb, ok := CollectFencedBlock(lines, i)
			if !ok {
				continue
			}
			lang := fenceLang(fb.Info)
			if lang == "bash" || lang == "sh" || lang == "zsh" || isConfigLang(lang) {
				seen = true
				i += fb.Consumed - 1
				continue
			}
			// A plain / output block is where the output goes — stop here.
			if seen {
				return i
			}
			i += fb.Consumed - 1
			continue
		}
		norm := NormalizeIndent(raw, tabWidth)
		if _, ok := ParseWriteDirective(norm); ok {
			seen = true
			if fb, ok2 := CollectFencedBlock(lines, i+1); ok2 {
				i += fb.Consumed
			}
			continue
		}
		if _, ok := DetectListCommand(norm); ok {
			seen = true
			continue
		}
		// Prose / other text: not a boundary — keep scanning.
	}
	return len(lines)
}

func okStep(execCount, code int) int {
	if code == 0 {
		return execCount + 1
	}
	return 0
}

func (e *Engine) runParallel(items []string, opts RunOptions) bool {
	// The visible terminal is a single shell: capturing several commands at once
	// would interleave their output. Run them sequentially in live-terminal mode.
	if opts.ExecCommand != nil {
		allOK := true
		for _, it := range items {
			if e.runOneCommand(it, opts) != 0 {
				allOK = false
			}
		}
		return allOK
	}
	results := make([]int, len(items))
	var wg sync.WaitGroup
	for idx, it := range items {
		wg.Add(1)
		go func(idx int, body string) {
			defer wg.Done()
			results[idx] = e.runOneCommand(body, opts)
		}(idx, it)
	}
	wg.Wait()
	for _, c := range results {
		if c != 0 {
			return false
		}
	}
	return true
}

func (e *Engine) evalAssert(a Assert) bool {
	switch a.Kind {
	case AssertContains:
		return strings.Contains(e.Vars.Prev, a.Arg)
	case AssertEquals:
		return strings.TrimSpace(e.Vars.Prev) == a.Arg
	case AssertStatus:
		return e.Vars.Status == a.Status
	case AssertRegex:
		return a.Re.MatchString(e.Vars.Prev)
	}
	return false
}

func (e *Engine) loadEnvFile(p string, opts RunOptions) {
	resolved := e.resolvePath(p)
	data, err := os.ReadFile(resolved)
	if err != nil {
		opts.info("[env file not found: " + resolved + "]")
		return
	}
	for k, v := range ParseEnvFile(string(data)) {
		e.Vars.Named[k] = v
		e.Env[k] = v
	}
	opts.info("[loaded env: " + resolved + "]")
}

func (e *Engine) handlePrompt(pd PromptDirective, opts RunOptions) bool {
	if opts.DryRun {
		opts.info("[dry-run] would prompt: " + pd.Message)
		return true
	}
	if opts.Prompt == nil {
		opts.info("[prompt unsupported in this mode]")
		return false
	}
	val, ok := opts.Prompt(pd.Message, pd.Secret)
	if !ok {
		opts.info("(cancelled by user)")
		return false
	}
	e.Vars.Named[pd.BindName] = val
	return true
}

func (e *Engine) handleWrite(wd WriteDirective, lines []string, i int, opts RunOptions) int {
	target := e.resolvePath(SubstituteVars(wd.FilePath, e.Vars))
	fb, ok := CollectFencedBlock(lines, i+1)
	if !ok {
		opts.info("[write: no fenced block after " + wd.FilePath + "]")
		return 0
	}
	content := SubstituteVars(fb.Content, e.Vars)
	if opts.DryRun {
		opts.info("[dry-run] would write " + target + " (" + fmt.Sprintf("%d", len(content)) + " bytes)")
		return fb.Consumed
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		err = os.WriteFile(target, []byte(content), 0o644)
		if err != nil {
			opts.info("[write failed: " + err.Error() + "]")
		} else {
			opts.info("[wrote " + target + "]")
		}
	}
	return fb.Consumed
}

func (e *Engine) handleInclude(p string, opts RunOptions) int {
	resolved := e.resolvePath(p)
	data, err := os.ReadFile(resolved)
	if err != nil {
		opts.info("[include not found: " + resolved + "]")
		return 1
	}
	opts.info("[include: " + resolved + "]")
	saved := e.Cwd
	e.Cwd = filepath.Dir(resolved)
	f := e.Run(strings.Split(string(data), "\n"), 0, false, opts)
	e.Cwd = saved
	return f
}

func (e *Engine) runFencedScript(fb FencedBlock, opts RunOptions) int {
	// Join line continuations and resolve variables, then run as one script.
	script := SubstituteVars(fb.Content, e.Vars)
	return e.runOneCommand(script, opts)
}

func (e *Engine) autoWriteBlock(fb FencedBlock, lang string, opts RunOptions) {
	// Filename may follow the language token in the fence info string.
	fields := strings.Fields(fb.Info)
	var name string
	if len(fields) >= 2 {
		name = fields[1]
	} else {
		name = randomName() + "." + lang
	}
	target := e.resolvePath(SubstituteVars(name, e.Vars))
	content := SubstituteVars(fb.Content, e.Vars)
	if opts.DryRun {
		opts.info("[dry-run] would write " + target)
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			opts.info("[write failed: " + err.Error() + "]")
			return
		}
	}
	opts.info("[wrote " + target + "]")
}

func fenceLang(info string) string {
	f := strings.Fields(info)
	if len(f) == 0 {
		return ""
	}
	return strings.ToLower(f[0])
}

func isConfigLang(lang string) bool {
	switch lang {
	case "yaml", "yml", "json", "conf", "ini", "toml":
		return true
	}
	return false
}

var nameCounter int

func randomName() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	nameCounter++
	n := nameCounter*2654435761 + 12345
	b := make([]byte, 8)
	for i := range b {
		b[i] = letters[(n>>(i*3))%26]
		if n < 0 {
			n = -n
		}
	}
	return string(b)
}
