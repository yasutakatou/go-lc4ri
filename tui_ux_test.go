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

// newTestTUI builds a TUI over doc in a temp directory and starts the
// application, returning it with a cleanup that stops the app and the shell.
func newTestTUI(t *testing.T, doc string) *tui {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "rb.md")
	if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	app := tview.NewApplication()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(120, 40)
	app.SetScreen(sc)

	tu := &tui{app: app, file: f, cfg: LoadConfig(), dir: dir, termWeight: 8}
	if err := tu.build(doc); err != nil {
		t.Fatalf("build: %v", err)
	}
	go func() { _ = app.Run() }()
	t.Cleanup(func() {
		app.Stop()
		tu.term.Close()
	})
	time.Sleep(400 * time.Millisecond) // let the shell come up
	return tu
}

// cursorAt drives the editor's cursor to row the way a user would, with arrow
// keys (a cold Select to a far offset mis-maps until the layout is built).
func cursorAt(tu *tui, row int) {
	ih := tu.editor.InputHandler()
	noop := func(tview.Primitive) {}
	for i := 0; i < row; i++ {
		ih(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noop)
	}
}

func rowOf(text, want string) int {
	for i, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == want {
			return i
		}
	}
	return -1
}

// TestRunRefusesWhenNothingWouldRun covers the failure the users actually hit:
// pressing the run key where there is no command must say so, and must leave
// the document completely alone — no green "executed" marker, and above all no
// empty ```output block, which used to be the only visible outcome.
func TestRunRefusesWhenNothingWouldRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	doc := "# Title\n- echo aaa\n\njust prose\nmore prose\n"
	tu := newTestTUI(t, doc)

	var before, after, status string
	onMain(tu.app, func() {
		before = tu.editor.GetText()
		cursorAt(tu, 3) // "just prose": nothing runnable from here to the end
		tu.runFromEditor()
		after = tu.editor.GetText()
		status = tu.status.GetText(true)
	})

	if after != before {
		t.Errorf("document was modified by a run that had nothing to run:\n%q", after)
	}
	if strings.Contains(after, "```output") {
		t.Errorf("an empty output block was inserted:\n%q", after)
	}
	if !strings.Contains(status, "nothing to run") {
		t.Errorf("status = %q, want it to explain that there is nothing to run", status)
	}
	// The message must name the runnable forms and the way to reach one.
	for _, want := range []string{"- command", "1. command", "```bash", tu.keyLabel(actNextStep)} {
		if !strings.Contains(status, want) {
			t.Errorf("status = %q, want it to mention %q", status, want)
		}
	}
	tu.capMu.Lock()
	running := tu.running
	tu.capMu.Unlock()
	if running {
		t.Errorf("a refused run left the session marked as running")
	}
}

// TestRunFromEditorAdvancesToNextStep checks that finishing a block parks the
// cursor on the next step, past the output block just written — so working
// through a runbook never requires scrolling between steps.
func TestRunFromEditorAdvancesToNextStep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-shell capture test")
	}
	doc := "# Title\n- echo aaa_111\n\nsome prose in between\n\n- echo bbb_222\n"
	tu := newTestTUI(t, doc)

	onMain(tu.app, func() {
		cursorAt(tu, 1) // the first command
		tu.runFromEditor()
	})

	deadline := time.Now().Add(8 * time.Second)
	var text string
	var row int
	for time.Now().Before(deadline) {
		onMain(tu.app, func() {
			text = tu.editor.GetText()
			_, _, row, _ = tu.editor.GetCursor()
		})
		if strings.Contains(text, "aaa_111") && strings.Contains(text, "```output") {
			want := rowOf(text, "- echo bbb_222")
			if row == want {
				return // cursor advanced onto the next step
			}
			// give the post-run advance a moment to land
			if time.Now().After(deadline.Add(-2 * time.Second)) {
				t.Fatalf("cursor on row %d, want next step row %d\n%s", row, want, text)
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("run never completed; editor:\n%s", text)
}

// TestStepJumpKeys checks the next/prev-step keys move the cursor between
// runnable lines and are left to the shell when the terminal has focus.
func TestStepJumpKeys(t *testing.T) {
	doc := "# Title\nprose\nprose\n- echo one\nprose\nprose\n- echo two\nprose\n"
	tu := newTestTUI(t, doc)

	first := rowOf(doc, "- echo one")
	second := rowOf(doc, "- echo two")

	var row int
	var consumed bool
	onMain(tu.app, func() {
		consumed = tu.onKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)) == nil
		_, _, row, _ = tu.editor.GetCursor()
	})
	if !consumed {
		t.Fatalf("next-step key was not consumed by the editor")
	}
	if row != first {
		t.Fatalf("next-step jumped to row %d, want %d", row, first)
	}

	onMain(tu.app, func() {
		tu.onKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone))
		_, _, row, _ = tu.editor.GetCursor()
	})
	if row != second {
		t.Fatalf("second next-step jumped to row %d, want %d", row, second)
	}

	onMain(tu.app, func() {
		tu.onKey(tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModNone))
		_, _, row, _ = tu.editor.GetCursor()
	})
	if row != first {
		t.Fatalf("prev-step jumped to row %d, want %d", row, first)
	}

	// At the last step there is nowhere to go: the cursor stays put and the
	// status bar says so instead of moving somewhere arbitrary.
	var status string
	onMain(tu.app, func() {
		tu.onKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone))
		tu.onKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone))
		_, _, row, _ = tu.editor.GetCursor()
		status = tu.status.GetText(true)
	})
	if row != second {
		t.Errorf("cursor moved off the last step to row %d", row)
	}
	if !strings.Contains(status, "no runnable step after") {
		t.Errorf("status = %q, want a 'no step after this line' notice", status)
	}

	// With the terminal focused the same keys belong to the shell (history).
	var passed bool
	onMain(tu.app, func() {
		tu.focusTerm = true
		passed = tu.onKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone)) != nil
		tu.focusTerm = false
	})
	if !passed {
		t.Errorf("next-step key was swallowed while the terminal had focus")
	}
}

// TestStatusExplainsWhatRunWillDo covers the context line of the status bar:
// it must name the command under the cursor, and warn when there is none.
func TestStatusExplainsWhatRunWillDo(t *testing.T) {
	doc := "# Title\n- kubectl get nodes\n\ntrailing prose\n"
	tu := newTestTUI(t, doc)

	var onCmd, onProse string
	onMain(tu.app, func() {
		cursorAt(tu, 1)
		tu.refreshStatus()
		onCmd = tu.status.GetText(true)
		cursorAt(tu, 2) // now on "trailing prose"
		tu.refreshStatus()
		onProse = tu.status.GetText(true)
	})

	if !strings.Contains(onCmd, "kubectl get nodes") {
		t.Errorf("status on a command = %q, want the command echoed", onCmd)
	}
	if !strings.Contains(onCmd, tu.keyLabel(actRun)) {
		t.Errorf("status on a command = %q, want the run key named", onCmd)
	}
	if !strings.Contains(onProse, "nothing to run at the cursor") {
		t.Errorf("status on prose = %q, want a 'nothing to run' notice", onProse)
	}
	// The shortcut reminder stays on the second row in both cases.
	stepKeys := tu.keyLabel(actNextStep) + "/" + tu.keyLabel(actPrevStep) + ":step"
	for _, s := range []string{onCmd, onProse} {
		if !strings.Contains(s, stepKeys) {
			t.Errorf("status = %q, want the shortcut row (%q) to survive", s, stepKeys)
		}
	}
}

// TestPaneTitlesStateTheirRole checks the split screen labels itself: which
// pane is the runbook, which is the shell, and the key that runs a step.
func TestPaneTitlesStateTheirRole(t *testing.T) {
	tu := newTestTUI(t, "# Title\n- echo hi\n")

	var top, bottom string
	onMain(tu.app, func() {
		top = tu.editor.GetTitle()
		bottom = tu.term.GetTitle()
	})
	for _, want := range []string{"① Runbook", "rb.md", tu.keyLabel(actRun), tu.keyLabel(actSave)} {
		if !strings.Contains(top, want) {
			t.Errorf("editor title = %q, want it to contain %q", top, want)
		}
	}
	for _, want := range []string{"② Terminal", "commands run here", tu.keyLabel(actFocus)} {
		if !strings.Contains(bottom, want) {
			t.Errorf("terminal title = %q, want it to contain %q", bottom, want)
		}
	}
}

// TestHelpExplainsTheFlow checks the help overlay opens with the three-step
// model of the tool rather than a bare key list.
func TestHelpExplainsTheFlow(t *testing.T) {
	tu := newTestTUI(t, "# Title\n- echo hi\n")

	var text string
	onMain(tu.app, func() {
		tu.showHelp()
		if _, prim := tu.pages.GetFrontPage(); prim != nil {
			text = findHelpText(prim)
		}
	})
	if text == "" {
		t.Fatal("help overlay produced no text")
	}
	for _, want := range []string{"How it works", "runbook", "terminal", "```output", "- command"} {
		if !strings.Contains(text, want) {
			t.Errorf("help text is missing %q", want)
		}
	}
}

// findHelpText digs the TextView out of the modal's nested flexes.
func findHelpText(p tview.Primitive) string {
	switch v := p.(type) {
	case *tview.TextView:
		return v.GetText(true)
	case *tview.Flex:
		for i := 0; i < v.GetItemCount(); i++ {
			if s := findHelpText(v.GetItem(i)); s != "" {
				return s
			}
		}
	}
	return ""
}
