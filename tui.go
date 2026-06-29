package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tui is the interactive split screen: an always-editable Markdown editor on
// top and a live OS terminal on the bottom. F2 switches focus between them.
type tui struct {
	app    *tview.Application
	pages  *tview.Pages
	body   *tview.Flex
	editor *tview.TextArea
	term   *TermView
	status *tview.TextView

	file       string
	cfg        Config
	dir        string
	profile    string
	dirty      bool
	focusTerm  bool
	termWeight int
	overlay    string

	// F5 starts an Engine-driven run of the block from the cursor to the next
	// boundary. Shell commands are delegated to the visible terminal and their
	// output — plus directive markers (write:, assert:, …) — is streamed back
	// into a single ```output block placed at the boundary.
	capMu   sync.Mutex
	running bool        // an F5 run is in progress
	sess    *runSession // the active run's output-block target
	capIdle time.Duration
	cwd     string // shell cwd carried across runs (seeded from the runbook dir)

	// Per-command terminal capture (one command of the run at a time).
	capActive    bool
	capID        string
	capBegin     string
	capEnd       string
	capAcc       strings.Builder // accumulated, ANSI-stripped shell output
	capStarted   bool            // begin marker seen
	capScheduled bool            // a render is already queued (coalescing)
	capDone      chan ExecResult // signalled when the command's end marker arrives
	idleTimer    *time.Timer     // inactivity timeout for the current command
	capSeq       int             // unique id source
}

// runSession is the document state for one F5 run: the lines surrounding the
// output block, the cursor anchor row, and the finalized output accumulated so
// far (headers + completed command output + directive markers).
type runSession struct {
	pre, post []string
	row       int
	committed strings.Builder // guarded by tui.capMu
}

// reMarkerTail parses the exit status (and optional working directory) appended
// to a command's end marker line, e.g. "EC=0;PWD=/home/user".
var reMarkerTail = regexp.MustCompile(`^EC=(-?\d+)(?:;PWD=(.*))?$`)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-9A-Za-z]|\x1b[=>]`)

// stripCapture removes ANSI control sequences and normalises line endings.
func stripCapture(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

const (
	editorWeight  = 3
	minTermWeight = 1
	maxTermWeight = 20
)

// runTUI loads the document (creating an empty buffer if it does not yet exist)
// and starts the interactive application.
func runTUI(file, profile string) int {
	abs, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "code-lc4ri:", err)
		return 2
	}
	data, err := os.ReadFile(abs)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "code-lc4ri:", err)
		return 2
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	t := &tui{
		app:        tview.NewApplication(),
		file:       abs,
		cfg:        LoadConfig(),
		dir:        filepath.Dir(abs),
		profile:    profile,
		termWeight: 2,
	}
	if err := t.build(content); err != nil {
		fmt.Fprintln(os.Stderr, "code-lc4ri: terminal:", err)
		return 1
	}

	runErr := t.app.Run()
	t.term.Close()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "code-lc4ri:", runErr)
		return 1
	}
	return 0
}

// shellOverride returns the configured shell name, or "" for the OS default.
func (t *tui) shellOverride() string {
	if t.cfg.Shell != nil {
		return *t.cfg.Shell
	}
	return ""
}

// build wires up the editor, the terminal and the status bar.
func (t *tui) build(content string) error {
	if t.cwd == "" {
		t.cwd = t.dir // commands start in the runbook's directory
	}
	t.editor = tview.NewTextArea()
	t.editor.SetWrap(false)
	t.editor.SetText(content, false)
	t.editor.SetBorder(true).SetTitle(t.docTitle())
	t.editor.SetChangedFunc(func() {
		if !t.dirty {
			t.dirty = true
			t.editor.SetTitle(t.docTitle())
		}
	})
	t.editor.SetFocusFunc(func() {
		t.focusTerm = false
		t.refreshStatus()
	})

	tv, err := NewTermView(t.app, t.dir, t.shellOverride())
	if err != nil {
		return err
	}
	t.term = tv
	t.term.onExit = func() { t.app.Stop() } // closing the shell exits the app
	t.term.onData = t.onTermData
	t.term.SetFocusFunc(func() {
		t.focusTerm = true
		t.refreshStatus()
	})
	t.term.Start()

	t.status = tview.NewTextView().SetDynamicColors(true)

	t.body = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.editor, 0, editorWeight, true).
		AddItem(t.term, 0, t.termWeight, false).
		AddItem(t.status, 1, 0, false)

	t.pages = tview.NewPages().AddPage("main", t.body, true, true)
	t.app.SetRoot(t.pages, true).EnableMouse(true)
	t.app.SetInputCapture(t.onKey)
	t.app.SetFocus(t.editor)
	t.refreshStatus()
	return nil
}

func (t *tui) docTitle() string {
	mark := ""
	if t.dirty {
		mark = "*"
	}
	return " " + mark + filepath.Base(t.file) + " — Markdown (Ctrl-S save) "
}

// onKey handles the global shortcuts. Everything it does not consume falls
// through to the focused widget (the editor or the terminal).
func (t *tui) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if t.overlay != "" {
		// The help overlay is dismissed with Esc / F1. Interactive overlays
		// (prompt / confirm) own their own keys and dismiss themselves, so let
		// their events through untouched.
		if t.overlay == "help" {
			if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyF1 {
				t.closeOverlay()
			}
			return nil
		}
		return ev
	}

	switch ev.Key() {
	case tcell.KeyF10:
		t.app.Stop()
		return nil
	case tcell.KeyF1:
		t.showHelp()
		return nil
	case tcell.KeyCtrlS:
		t.save()
		return nil
	case tcell.KeyF5:
		t.runFromEditor()
		return nil
	case tcell.KeyF6:
		// shrink terminal / widen Markdown
		t.resizeTerm(-1)
		return nil
	case tcell.KeyF7:
		// grow terminal
		t.resizeTerm(1)
		return nil
	case tcell.KeyF2:
		// Focus switch. (Tab is intentionally left for the focused pane: the
		// terminal needs it for shell completion, the editor for indentation.)
		t.toggleFocus()
		return nil
	case tcell.KeyUp:
		// Fallback for terminals that deliver Ctrl+Arrow. On macOS these are
		// usually swallowed by Mission Control / App Exposé, so F6/F7 are the
		// reliable bindings.
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			t.resizeTerm(-1)
			return nil
		}
	case tcell.KeyDown:
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			t.resizeTerm(1)
			return nil
		}
	}
	return ev
}

// toggleFocus moves focus between the editor and the terminal.
func (t *tui) toggleFocus() {
	if t.focusTerm {
		t.app.SetFocus(t.editor)
	} else {
		t.app.SetFocus(t.term)
	}
}

// resizeTerm grows (delta>0) or shrinks (delta<0) the terminal pane.
func (t *tui) resizeTerm(delta int) {
	t.termWeight += delta
	if t.termWeight < minTermWeight {
		t.termWeight = minTermWeight
	}
	if t.termWeight > maxTermWeight {
		t.termWeight = maxTermWeight
	}
	t.body.ResizeItem(t.term, 0, t.termWeight)
	t.refreshStatus()
}

// save writes the editor buffer to disk.
func (t *tui) save() {
	if err := os.WriteFile(t.file, []byte(t.editor.GetText()), 0o644); err != nil {
		t.flash("[red]save failed: " + tview.Escape(err.Error()) + "[-]")
		return
	}
	t.dirty = false
	t.editor.SetTitle(t.docTitle())
	t.flash("[green]saved " + tview.Escape(t.file) + "[-]")
}

// runFromEditor runs the block of commands from the cursor down to the next
// boundary (a horizon rule, blank line or output fence), driving the same LC4RI
// engine the headless runner uses — so every directive (write:, prompt:,
// assert:, [retry], [parallel], include:, # env:, numbered variables, fenced
// bash / yaml blocks) is honoured. Shell commands run in the visible terminal
// and their output, together with directive markers, streams back into a single
// ```output block placed at the boundary. Focus stays on the cursor's line.
func (t *tui) runFromEditor() {
	t.capMu.Lock()
	busy := t.running
	t.capMu.Unlock()
	if busy {
		t.flash("[yellow]a command is still running…[-]")
		return
	}

	// cmd.exe can't be bracketed for capture — just send the line interactively.
	if t.term.Kind() == "cmd" {
		if line := strings.TrimSpace(cleanCommandLine(t.editorChunk())); line != "" {
			t.term.SendString(line + "\r")
			t.app.SetFocus(t.term)
		}
		return
	}

	lines := strings.Split(t.editor.GetText(), "\n")
	row, _, _, _ := t.editor.GetCursor() // start row (selection start, or cursor)
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}

	// The output block goes at the block boundary; drop any stale block there.
	insertAt := FindBlockBoundary(lines, row, DefaultIndentSpaces)
	lines, insertAt = removeOutputBlockAt(lines, insertAt)
	pre := append([]string{}, lines[:insertAt]...)
	post := append([]string{}, lines[insertAt:]...)

	sess := &runSession{pre: pre, post: post, row: row}
	idle := time.Duration(t.cfg.Timeout) * time.Millisecond
	if idle <= 0 {
		idle = 10 * time.Second
	}

	t.capMu.Lock()
	t.running = true
	t.sess = sess
	t.capIdle = idle
	t.capMu.Unlock()

	// Reflect the block removal immediately and keep focus on the command line.
	t.app.SetFocus(t.editor)
	t.applyDoc(pre, nil, post, row)
	t.refreshStatus()

	go t.runEngine(lines, row, sess)
}

// runEngine executes the block on a background goroutine. Output and directive
// markers are pushed into the session via the Engine's hooks; shell commands are
// delegated to the visible terminal through execInTerminal.
func (t *tui) runEngine(lines []string, startIdx int, sess *runSession) {
	t.capMu.Lock()
	seed := t.cwd
	t.capMu.Unlock()
	if seed == "" {
		seed = t.dir
	}
	eng := NewEngine(t.cfg, seed)
	opts := RunOptions{
		Profile:     t.profile,
		ExecCommand: t.execInTerminal,
		OnCommand:   func(cmd string) { t.sessHeader(sess, cmd) },
		OnOutput:    func(chunk string) { t.sessInfo(sess, strings.TrimRight(chunk, "\n")) },
		OnInfo:      func(text string) { t.sessInfo(sess, text) },
		Prompt:      t.askPrompt,
		ConfirmRun:  t.askConfirm,
	}
	eng.Run(lines, startIdx, true, opts)

	t.capMu.Lock()
	t.running = false
	t.cwd = eng.Cwd // carry the working directory into the next run
	t.capMu.Unlock()
	t.app.QueueUpdateDraw(func() {
		t.renderDoc()
		t.refreshStatus()
	})
}

// execInTerminal implements RunOptions.ExecCommand: it brackets cmd with marker
// lines, sends it to the live shell and blocks until the end marker (carrying
// the exit code) arrives, streaming the command's output back into the document
// meanwhile. It runs on the engine goroutine, not the UI thread.
func (t *tui) execInTerminal(cmd string) ExecResult {
	kind := t.term.Kind()

	t.capMu.Lock()
	id := fmt.Sprintf("%d", t.capSeq)
	t.capSeq++
	t.capMu.Unlock()

	wrapped, begin, end, ok := wrapCommand(kind, cmd, id)
	if !ok {
		t.term.SendString(cmd + "\r")
		return ExecResult{}
	}

	ch := make(chan ExecResult, 1)
	t.capMu.Lock()
	idle := t.capIdle
	t.capActive = true
	t.capID = begin
	t.capBegin = begin
	t.capEnd = end
	t.capAcc.Reset()
	t.capStarted = false
	t.capScheduled = false
	t.capDone = ch
	t.idleTimer = time.AfterFunc(idle, func() {
		t.app.QueueUpdateDraw(func() { t.finishCommand(begin, true) })
	})
	t.capMu.Unlock()

	t.term.SendString(wrapped + "\r")
	return <-ch
}

// onTermData receives every chunk of raw shell output. While a command capture
// is active it strips the begin marker, resets the inactivity timer and either
// finalizes (end marker seen) or schedules a coalesced live render.
func (t *tui) onTermData(p []byte) {
	t.capMu.Lock()
	if !t.capActive {
		t.capMu.Unlock()
		return
	}
	t.capAcc.WriteString(stripCapture(string(p)))
	if t.idleTimer != nil {
		t.idleTimer.Reset(t.capIdle)
	}

	if !t.capStarted {
		acc := t.capAcc.String()
		if i := strings.Index(acc, t.capBegin); i >= 0 {
			t.capStarted = true
			rest := strings.TrimPrefix(acc[i+len(t.capBegin):], "\n")
			t.capAcc.Reset()
			t.capAcc.WriteString(rest)
		}
	}
	// Only finalize once the whole end-marker line has arrived (it ends with a
	// newline), so the trailing EC=…;PWD=… payload is complete before we parse it.
	done := false
	if t.capStarted {
		s := t.capAcc.String()
		if i := strings.Index(s, t.capEnd); i >= 0 && strings.IndexByte(s[i:], '\n') >= 0 {
			done = true
		}
	}
	schedule := !t.capScheduled
	if schedule {
		t.capScheduled = true
	}
	id := t.capID
	t.capMu.Unlock()

	if done {
		t.app.QueueUpdateDraw(func() { t.finishCommand(id, false) })
		return
	}
	if schedule {
		t.app.QueueUpdateDraw(func() { t.renderDoc() })
	}
}

// finishCommand ends the current command's capture: it extracts the output and
// exit code, appends the output to the session, renders, and signals the
// waiting execInTerminal. Runs on the UI thread.
func (t *tui) finishCommand(id string, timedOut bool) {
	t.capMu.Lock()
	if !t.capActive || t.capID != id {
		t.capMu.Unlock()
		return
	}
	if t.idleTimer != nil {
		t.idleTimer.Stop()
		t.idleTimer = nil
	}
	acc := t.capAcc.String()
	out, code, cwd := acc, 0, ""
	if i := strings.Index(acc, t.capEnd); i >= 0 {
		out = acc[:i]
		tail := acc[i+len(t.capEnd):]
		if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
			tail = tail[:nl]
		}
		if m := reMarkerTail.FindStringSubmatch(tail); m != nil {
			code, _ = strconv.Atoi(m[1])
			cwd = m[2]
		}
	} else if !t.capStarted {
		// Begin marker may still be buffered if nothing streamed; drop it.
		if i := strings.Index(out, t.capBegin); i >= 0 {
			out = out[i+len(t.capBegin):]
		}
	}
	if timedOut {
		code = -1
	}
	out = strings.TrimRight(out, "\n")
	if t.sess != nil {
		t.sess.committed.WriteString(out)
		if out != "" {
			t.sess.committed.WriteString("\n")
		}
		if timedOut {
			t.sess.committed.WriteString("[timeout after " + fmt.Sprintf("%d", t.cfg.Timeout) + "ms]\n")
		}
	}
	ch := t.capDone
	t.capActive = false
	t.capStarted = false
	t.capDone = nil
	t.capMu.Unlock()

	t.renderDoc()
	if ch != nil {
		ch <- ExecResult{Stdout: out, Code: code, TimedOut: timedOut, Cwd: cwd}
	}
}

// sessHeader appends a "[ command ] timestamp" header to the session's output,
// preceded by a --- separator after the first unit. Called from the engine
// goroutine before each command runs.
func (t *tui) sessHeader(sess *runSession, cmd string) {
	ts := time.Now().Format("Mon Jan 02 15:04:05 2006")
	t.capMu.Lock()
	if sess.committed.Len() > 0 {
		sess.committed.WriteString("---\n")
	}
	sess.committed.WriteString("[ " + cmd + " ] " + ts + "\n")
	t.capMu.Unlock()
	t.app.QueueUpdateDraw(func() { t.renderDoc() })
}

// sessInfo appends a directive / status marker line to the session's output.
func (t *tui) sessInfo(sess *runSession, text string) {
	if text == "" {
		return
	}
	t.capMu.Lock()
	sess.committed.WriteString(text + "\n")
	t.capMu.Unlock()
	t.app.QueueUpdateDraw(func() { t.renderDoc() })
}

// renderDoc rebuilds the document with the session's output block (committed
// output plus any live, not-yet-finalized command output). Runs on the UI thread.
func (t *tui) renderDoc() {
	t.capMu.Lock()
	t.capScheduled = false
	if t.sess == nil {
		t.capMu.Unlock()
		return
	}
	full := t.sess.committed.String()
	if t.capActive && t.capStarted {
		live := t.capAcc.String()
		if i := strings.Index(live, t.capEnd); i >= 0 {
			live = live[:i] // marker (and its EC/PWD tail) seen — hide it entirely
		} else if len(live) > len(t.capEnd) {
			live = live[:len(live)-len(t.capEnd)] // withhold a partial end marker
		} else {
			live = ""
		}
		full += strings.TrimRight(live, "\n")
	}
	pre, post, row := t.sess.pre, t.sess.post, t.sess.row
	t.capMu.Unlock()

	t.applyDoc(pre, buildOutputBlock(strings.TrimRight(full, "\n")), post, row)
}

// editorChunk returns the active selection, or the line under the cursor.
func (t *tui) editorChunk() string {
	if sel, _, _ := t.editor.GetSelection(); strings.TrimSpace(sel) != "" {
		return sel
	}
	lines := strings.Split(t.editor.GetText(), "\n")
	_, _, row, _ := t.editor.GetCursor()
	if row >= 0 && row < len(lines) {
		return lines[row]
	}
	return ""
}

// askPrompt implements RunOptions.Prompt: it shows a modal input box and blocks
// the engine goroutine until the user answers (Enter) or cancels (Esc).
func (t *tui) askPrompt(msg string, secret bool) (string, bool) {
	type res struct {
		val string
		ok  bool
	}
	ch := make(chan res, 1)
	t.app.QueueUpdateDraw(func() {
		input := tview.NewInputField().SetLabel(msg + " ")
		if secret {
			input.SetMaskCharacter('*')
		}
		input.SetBorder(true).SetTitle(" prompt (Enter to submit, Esc to cancel) ")
		input.SetDoneFunc(func(key tcell.Key) {
			v := input.GetText()
			ok := key == tcell.KeyEnter
			t.closeOverlay()
			ch <- res{v, ok}
		})
		t.overlay = "prompt"
		t.pages.AddPage("prompt", t.modalWrap(input, 70, 3), true, true)
		t.app.SetFocus(input)
	})
	r := <-ch
	return r.val, r.ok
}

// askConfirm implements RunOptions.ConfirmRun: a modal yes/no gate for a command
// matching a dangerous pattern. Blocks the engine goroutine until answered.
func (t *tui) askConfirm(cmd, pattern string) bool {
	ch := make(chan bool, 1)
	t.app.QueueUpdateDraw(func() {
		modal := tview.NewModal().
			SetText("⚠ Dangerous command matched /" + pattern + "/:\n\n" + cmd + "\n\nRun it?").
			AddButtons([]string{"Cancel", "Run"}).
			SetDoneFunc(func(_ int, label string) {
				t.closeOverlay()
				ch <- label == "Run"
			})
		t.overlay = "confirm"
		t.pages.AddPage("confirm", modal, true, true)
		t.app.SetFocus(modal)
	})
	return <-ch
}

// applyDoc rebuilds the editor from pre + (optional) block + post, restores the
// cursor to the original row and keeps that line in view.
//
// SetText resets the cursor to the top and discards the line layout. We can't
// jump the cursor back with Select(byteOffset): a cold Select to a far offset
// mis-maps in tview until the layout is built. Incremental down-moves, however,
// build the layout one row at a time and land reliably — so we drive KeyDown to
// the target row, then anchor the viewport with SetOffset.
func (t *tui) applyDoc(pre, block, post []string, row int) {
	all := make([]string, 0, len(pre)+len(block)+len(post))
	all = append(all, pre...)
	all = append(all, block...)
	all = append(all, post...)
	t.editor.SetText(strings.Join(all, "\n"), false)

	ih := t.editor.InputHandler()
	noop := func(tview.Primitive) {}
	for i := 0; i < row; i++ {
		ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
	}

	// Re-anchor the view so the executed line (and the output growing beneath
	// it) stays visible instead of snapping back to the top.
	top := row - 2
	if top < 0 {
		top = 0
	}
	t.editor.SetOffset(top, 0)
}

// cleanCommandLine strips a Markdown list ("- ") or numbered ("1. ") prefix.
func cleanCommandLine(line string) string {
	norm := NormalizeIndent(line, DefaultIndentSpaces)
	if lc, ok := DetectListCommand(norm); ok {
		return lc.Body
	}
	if n, ok := DetectNumbered(line); ok {
		return n.Body
	}
	return line
}

// wrapCommand brackets cmd with printed begin/end markers so the runner can
// extract exactly this command's output from the live shell stream, and appends
// the command's exit code to the end marker (as "EC=<n>") so the engine can
// drive AND-chains and assert: status. The marker literals are split in the
// *typed* command (via adjacent quotes / concatenation) so the search tokens
// only ever appear in the shell's *output*, never in the echoed input line. cmd
// may span multiple lines (a fenced script): markers bracket the whole block and
// the exit code reflects its last command. Returns ok=false for shells we don't
// bracket (cmd.exe).
func wrapCommand(kind, cmd, id string) (wrapped, begin, end string, ok bool) {
	begin = "@@LC4RI_B_" + id + "@@"
	end = "@@LC4RI_E_" + id + "@@"
	switch kind {
	case "posix":
		// 'X''Y' are adjacent single-quoted strings → concatenated argument. The
		// end marker line carries the exit code and the shell's $PWD.
		b := "'@@LC4RI_B_''" + id + "@@'"
		e := "'@@LC4RI_E_''" + id + "@@'"
		wrapped = "printf '%s\\n' " + b + "\n" + cmd +
			"\n__lc4ri_ec=$? ; printf '%sEC=%s;PWD=%s\\n' " + e + " \"$__lc4ri_ec\" \"$PWD\""
		return wrapped, begin, end, true
	case "powershell":
		b := "('@@LC4RI_B_'+'" + id + "@@')"
		e := "('@@LC4RI_E_'+'" + id + "@@')"
		wrapped = "Write-Output " + b + "\n" + cmd +
			"\n$__lc4ri_ec = $(if ($LASTEXITCODE -ne $null) {$LASTEXITCODE} elseif ($?) {0} else {1})" +
			"; Write-Output (" + e + "+'EC='+$__lc4ri_ec+';PWD='+(Get-Location).Path)"
		return wrapped, begin, end, true
	default: // cmd.exe and anything unknown: don't attempt capture
		return "", begin, end, false
	}
}

// buildOutputBlock wraps captured text in a fenced ```output block, lengthening
// the fence if the payload contains a matching backtick run.
func buildOutputBlock(out string) []string {
	fence := "```"
	for strings.Contains(out, fence) {
		fence += "`"
	}
	block := []string{fence + "output"}
	block = append(block, strings.Split(out, "\n")...)
	block = append(block, fence)
	return block
}

// removeOutputBlockAt deletes a previously emitted ```output block that begins
// at (or just past blank lines after) index `at`, so re-running updates it in
// place. Returns the possibly-shortened lines and the index to insert at.
func removeOutputBlockAt(lines []string, at int) ([]string, int) {
	if at < 0 {
		at = 0
	}
	if at > len(lines) {
		at = len(lines)
	}
	j := at
	for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
		j++
	}
	if j >= len(lines) {
		return lines, at
	}
	m := reFenceOpen.FindStringSubmatch(lines[j])
	if m == nil {
		return lines, at
	}
	info := strings.TrimSpace(lines[j][len(m[1])+len(m[2]):])
	if fenceLang(info) != "output" {
		return lines, at
	}
	fb, found := CollectFencedBlock(lines, j)
	if !found {
		return lines, at
	}
	out := append(lines[:j:j], lines[j+fb.Consumed:]...)
	return out, j
}

func (t *tui) refreshStatus() {
	focus := "[aqua]editor[-]"
	if t.focusTerm {
		focus = "[lime]terminal[-]"
	}
	state := ""
	if t.dirty {
		state += " [yellow]*unsaved[-]"
	}
	t.capMu.Lock()
	running := t.running
	t.capMu.Unlock()
	if running {
		state += " [yellow]running…[-]"
	}
	t.status.SetText(fmt.Sprintf(
		" focus:%s%s   [grey]F2[-]:switch [grey]Ctrl-S[-]:save [grey]F5[-]:run→reflect [grey]F6/F7[-]:resize [grey]F1[-]:help [grey]F10[-]:quit",
		focus, state))
}

// flash writes a transient message to the status bar (until the next refresh).
func (t *tui) flash(msg string) {
	t.status.SetText(" " + msg)
}

// =========================================================================
// Help overlay.
// =========================================================================

func (t *tui) modalWrap(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

func (t *tui) closeOverlay() {
	if t.overlay == "" {
		return
	}
	t.pages.RemovePage(t.overlay)
	t.overlay = ""
	if t.focusTerm {
		t.app.SetFocus(t.term)
	} else {
		t.app.SetFocus(t.editor)
	}
}

func (t *tui) showHelp() {
	if t.overlay != "" {
		t.closeOverlay()
		return
	}
	help := tview.NewTextView().SetDynamicColors(true)
	help.SetBorder(true).SetTitle(" Keyboard Shortcuts (Esc / F1 to close) ")
	help.SetText(`
  [aqua::b]Layout[-:-:-]
    [yellow]F2[-]           switch focus: editor ⇄ terminal
    [yellow]F6 / F7[-]      shrink / grow the terminal pane
                  (Ctrl-↑/↓ also works where the terminal allows it)
    [yellow]mouse click[-]  focus a pane

  [aqua::b]Editor (top)[-:-:-]
    always editable — type Markdown freely
    [yellow]Ctrl-S[-]       save to file (any time)
    [yellow]F5[-]           run the block from the cursor to the next
                  boundary (blank line / *** / output block); all
                  commands and directives (write:, prompt:, assert:,
                  [retry], [parallel], include:, # env:, 1. vars,
                  ` + "```bash / ```yaml" + ` blocks) run in order, their
                  output streaming back as a ` + "```output" + ` block
    (Tab inserts a tab / triggers indentation as usual)

  [aqua::b]Terminal (bottom)[-:-:-]
    a real OS shell — works like any terminal when focused
    keys (incl. Ctrl-C) go to the shell; type 'exit' to quit

  [aqua::b]Application[-:-:-]
    [yellow]F1[-]           this help
    [yellow]F10[-]          quit
`)
	t.overlay = "help"
	t.pages.AddPage("help", t.modalWrap(help, 64, 24), true, true)
	t.app.SetFocus(help)
}
