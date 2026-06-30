package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestTermViewRunsShell drives the embedded terminal end-to-end: it launches
// the OS shell on a PTY, sends a command, and checks the output is reflected in
// the emulator's screen buffer.
func TestTermViewRunsShell(t *testing.T) {
	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(80, 24)
	app.SetScreen(sc)

	tv, err := NewTermView(app, ".", "")
	if err != nil {
		t.Fatalf("NewTermView: %v", err)
	}
	tv.Start()
	app.SetRoot(tv, true)
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tv.Close()
	}()

	time.Sleep(400 * time.Millisecond) // let the shell start

	marker := "lc4ri_marker_42"
	cmd := "echo " + marker
	if runtime.GOOS == "windows" {
		cmd = "echo " + marker
	}
	tv.SendString(cmd + "\r")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(tv.term.String(), marker) {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("shell output never showed %q in:\n%s", marker, tv.term.String())
}

func TestShellCommandPerOS(t *testing.T) {
	name, _, label := shellCommand("")
	if name == "" || label == "" {
		t.Fatalf("empty shell for %s: name=%q label=%q", runtime.GOOS, name, label)
	}
}

// TestRunFromEditorStreamsOutput runs a Markdown command line with F5 and
// checks the shell output is reflected back into the editor as an output block
// while focus returns to the original line.
func TestRunFromEditorStreamsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "rb.md")
	// Put the command well down the document so that "return to the executed
	// line" is meaningfully different from scrolling to the top.
	header := "# Title\n" + strings.Repeat("filler\n", 20) + "\n"
	cmdRow := strings.Count(header, "\n") // command sits on the next line
	if err := os.WriteFile(f, []byte(header+"- echo lc4ri_stream_77\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(100, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, file: f, cfg: LoadConfig(), dir: dir, termWeight: 8}
	data, _ := os.ReadFile(f)
	if err := tu.build(string(data)); err != nil {
		t.Fatalf("build: %v", err)
	}
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tu.term.Close()
	}()
	time.Sleep(500 * time.Millisecond) // let the shell start

	// Put the cursor on the command line, then fire F5 on the main thread.
	onMain(app, func() {
		// Navigate to the command line the way a user would — with arrow keys,
		// which build the line layout reliably as the cursor descends.
		ih := tu.editor.InputHandler()
		noop := func(tview.Primitive) {}
		for i := 0; i < cmdRow; i++ {
			ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
		}
		tu.runFromEditor()
	})

	deadline := time.Now().Add(6 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var txt string
		var focusEditor bool
		var row, viewTop int
		onMain(app, func() {
			txt = tu.editor.GetText()
			focusEditor = app.GetFocus() == tu.editor
			_, _, row, _ = tu.editor.GetCursor()
			viewTop, _ = tu.editor.GetOffset()
		})
		last = txt
		if strings.Contains(txt, "```output") && strings.Contains(txt, "lc4ri_stream_77") {
			if !focusEditor {
				t.Errorf("focus did not return to the editor")
			}
			if row != cmdRow {
				t.Errorf("cursor on row %d, want original line %d", row, cmdRow)
			}
			// The view must be anchored near the executed line, not at the top.
			if viewTop == 0 || viewTop > cmdRow {
				t.Errorf("view top row = %d, want it anchored near line %d (not the top)", viewTop, cmdRow)
			}
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("output block never appeared; editor:\n%s", last)
}

// TestRunFromEditorBatchDirectives drives F5 over a multi-command block that
// also contains a write: directive, and checks (a) every command in the block
// ran as one batch into a single output block, and (b) the directive wrote its
// file. This is the v1.x "run from cursor to the boundary" behaviour.
func TestRunFromEditorBatchDirectives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "rb.md")
	target := filepath.Join(dir, "out", "conf.yaml")
	doc := "# Title\n" +
		"- echo aaa_111\n" +
		"- echo bbb_222\n" +
		"- write: " + target + "\n" +
		"  ```yaml\n" +
		"  key: value_333\n" +
		"  ```\n" +
		"\n" // trailing blank line == boundary
	if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdRow := 1 // first "- echo" line

	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(100, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, file: f, cfg: LoadConfig(), dir: dir, termWeight: 8}
	data, _ := os.ReadFile(f)
	if err := tu.build(string(data)); err != nil {
		t.Fatalf("build: %v", err)
	}
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tu.term.Close()
	}()
	time.Sleep(500 * time.Millisecond)

	onMain(app, func() {
		ih := tu.editor.InputHandler()
		noop := func(tview.Primitive) {}
		for i := 0; i < cmdRow; i++ {
			ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
		}
		tu.runFromEditor()
	})

	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var txt string
		onMain(app, func() { txt = tu.editor.GetText() })
		last = txt
		fileOK := false
		if b, err := os.ReadFile(target); err == nil && strings.Contains(string(b), "value_333") {
			fileOK = true
		}
		if strings.Contains(txt, "aaa_111") && strings.Contains(txt, "bbb_222") &&
			strings.Contains(txt, "```output") && fileOK {
			// Exactly one output block for the whole batch.
			if n := strings.Count(txt, "```output"); n != 1 {
				t.Errorf("want 1 output block, got %d:\n%s", n, txt)
			}
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("batch run incomplete; editor:\n%s\nfile %s exists: %v", last, target, fileExists(target))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// TestWriteDirectiveFollowsCd checks that a write: directive lands in the
// directory reached by a preceding cd — even a compound "cd … && …" the engine
// can't statically track — because the shell's real $PWD is captured per command.
func TestWriteDirectiveFollowsCd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "rb.md")
	doc := "# Title\n" +
		"- cd subdir && echo moved\n" + // compound: only the live $PWD reveals the cd
		"- write: out.txt\n" +
		"  ```\n" +
		"  hello_cd_444\n" +
		"  ```\n" +
		"\n"
	if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdRow := 1

	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(100, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, file: f, cfg: LoadConfig(), dir: dir, termWeight: 8}
	data, _ := os.ReadFile(f)
	if err := tu.build(string(data)); err != nil {
		t.Fatalf("build: %v", err)
	}
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tu.term.Close()
	}()
	time.Sleep(500 * time.Millisecond)

	onMain(app, func() {
		ih := tu.editor.InputHandler()
		noop := func(tview.Primitive) {}
		for i := 0; i < cmdRow; i++ {
			ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
		}
		tu.runFromEditor()
	})

	target := filepath.Join(sub, "out.txt")
	wrong := filepath.Join(dir, "out.txt")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(target); err == nil && strings.Contains(string(b), "hello_cd_444") {
			if fileExists(wrong) {
				t.Errorf("file also written to runbook dir %s", wrong)
			}
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("write: did not follow cd into %s (wrong-dir file exists: %v)", sub, fileExists(wrong))
}

// TestTabReachesShell verifies Tab is forwarded to the focused terminal (for
// shell completion) instead of being swallowed for focus switching.
func TestTabReachesShell(t *testing.T) {
	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(80, 24)
	app.SetScreen(sc)

	tv, err := NewTermView(app, ".", "")
	if err != nil {
		t.Fatalf("NewTermView: %v", err)
	}
	tv.Start()
	app.SetRoot(tv, true)
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tv.Close()
	}()
	time.Sleep(600 * time.Millisecond)

	ih := tv.InputHandler()
	noop := func(tview.Primitive) {}
	for _, r := range "ls /et" {
		ih(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), noop)
	}
	ih(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noop)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(tv.term.String(), "/etc") {
			return // completion fired → Tab reached the shell
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Errorf("Tab did not reach the shell (no completion of 'ls /et'):\n%s", tv.term.String())
}

// TestRunFromEditorFencedBash drives F5 over a fenced ```bash block: the whole
// multi-line script is one ExecCommand call, so this exercises the multi-line
// terminal wrapping (begin marker / script / exit-code+end marker).
func TestRunFromEditorFencedBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "rb.md")
	doc := "# Title\n" +
		"```bash\n" +
		"echo line_aaa\n" +
		"echo line_bbb\n" +
		"```\n" +
		"\n"
	if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	fenceRow := 1 // the ```bash line

	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(100, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, file: f, cfg: LoadConfig(), dir: dir, termWeight: 8}
	data, _ := os.ReadFile(f)
	if err := tu.build(string(data)); err != nil {
		t.Fatalf("build: %v", err)
	}
	go func() { _ = app.Run() }()
	defer func() {
		app.Stop()
		tu.term.Close()
	}()
	time.Sleep(500 * time.Millisecond)

	onMain(app, func() {
		ih := tu.editor.InputHandler()
		noop := func(tview.Primitive) {}
		for i := 0; i < fenceRow; i++ {
			ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
		}
		tu.runFromEditor()
	})

	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var txt string
		onMain(app, func() { txt = tu.editor.GetText() })
		last = txt
		if strings.Contains(txt, "line_aaa") && strings.Contains(txt, "line_bbb") &&
			strings.Contains(txt, "```output") {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("fenced bash output incomplete; editor:\n%s", last)
}

// TestCaptureIsClean drives a command through execInTerminal and verifies the
// captured output contains only the command's real stdout — none of the shell's
// prompt/input echo, and none of the begin/end markers or the `__lc4ri_ec=…;
// printf …` closing wrapper that used to leak onto the last line of the output
// block. Bracketed paste keeps that noise out of the stream.
func TestCaptureIsClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(100, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, cfg: LoadConfig(), dir: ".", termWeight: 8, cwd: "."}
	tv, err := NewTermView(app, ".", "")
	if err != nil {
		t.Fatal(err)
	}
	tu.term = tv
	tu.term.onData = tu.onTermData
	tu.term.Start()
	app.SetRoot(tv, true)
	go func() { _ = app.Run() }()
	defer func() { app.Stop(); tv.Close() }()
	time.Sleep(600 * time.Millisecond)

	tu.capIdle = 2 * time.Second
	done := make(chan ExecResult, 1)
	go func() { done <- tu.execInTerminal("echo clean_aaa\necho clean_bbb") }()
	var res ExecResult
	select {
	case res = <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("execInTerminal timed out")
	}

	got := strings.TrimRight(res.Stdout, "\n")
	if want := "clean_aaa\nclean_bbb"; got != want {
		t.Errorf("captured output not clean:\n got %q\nwant %q", got, want)
	}
	for _, noise := range []string{termHideSentinel, "__lc4ri_ec", "printf", "echo clean_aaa"} {
		if strings.Contains(res.Stdout, noise) {
			t.Errorf("captured output leaked %q:\n%q", noise, res.Stdout)
		}
	}
	if res.Code != 0 {
		t.Errorf("exit code = %d, want 0", res.Code)
	}
}

// sync runs fn on the application's main loop and waits for it.
func onMain(app *tview.Application, fn func()) {
	done := make(chan struct{})
	app.QueueUpdateDraw(func() {
		fn()
		close(done)
	})
	<-done
}

// TestWrapCommandHidden ensures the markers wrapCommand emits carry the hide
// sentinel, so the terminal pane suppresses those lines.
func TestWrapCommandHidden(t *testing.T) {
	wrapped, begin, end, ok := wrapCommand("posix", "echo hi", "0")
	if !ok {
		t.Fatal("posix should be wrappable")
	}
	for _, s := range []string{begin, end} {
		if !strings.Contains(s, termHideSentinel) {
			t.Errorf("marker %q lacks hide sentinel %q", s, termHideSentinel)
		}
	}
	if !strings.Contains(wrapped, termHideSentinel) {
		t.Errorf("wrapped command %q lacks hide sentinel", wrapped)
	}
	// The contiguous begin token must NOT appear in the typed wrapper (only in
	// the shell's output), otherwise capture would self-trigger on the echo.
	if strings.Contains(wrapped, begin) {
		t.Errorf("wrapper %q contains contiguous begin token %q (echo would false-match)", wrapped, begin)
	}
}

// TestDoneMarkerIsSpaces pins the executed-block flag to two leading spaces (so
// it doesn't alter Markdown rendering the way a bullet or a tab would) and
// checks the add/strip round-trip, idempotency, and that highlighting keys off
// the two-space prefix.
func TestDoneMarkerIsSpaces(t *testing.T) {
	if doneMarker != "  " {
		t.Fatalf("doneMarker = %q, want two spaces", doneMarker)
	}
	const cmd = "1. hostname → {host}"
	marked := addDoneMarker(cmd)
	if !strings.HasPrefix(marked, "  ") {
		t.Errorf("addDoneMarker(%q) = %q, want two leading spaces", cmd, marked)
	}
	if got := addDoneMarker(marked); got != marked {
		t.Errorf("addDoneMarker not idempotent: %q -> %q", marked, got)
	}
	if got := stripDoneMarker(marked); got != cmd {
		t.Errorf("stripDoneMarker(%q) = %q, want %q", marked, got, cmd)
	}
	// The highlight in markedEditor.Draw keys off this same prefix.
	if !strings.HasPrefix(marked, doneMarker) {
		t.Errorf("marked line would not be highlighted: %q", marked)
	}
}

func TestCleanCommandLine(t *testing.T) {
	cases := map[string]string{
		"- echo hi":  "echo hi",
		"  - ls -la": "ls -la",
		"1. whoami":  "whoami",
		"plain echo": "plain echo",
	}
	for in, want := range cases {
		if got := cleanCommandLine(in); got != want {
			t.Errorf("cleanCommandLine(%q) = %q, want %q", in, got, want)
		}
	}
}
