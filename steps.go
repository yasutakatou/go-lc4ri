package main

import "strings"

// =============================================================================
// Step scanning — the shared answer to "which lines of this document does the
// engine actually run?".
//
// Engine.Run decides that inline while executing; the UI needs the same answer
// *before* anything runs, to (a) tell the user what F5 would do at the cursor,
// (b) jump between steps without scrolling, and (c) refuse a run that would do
// nothing instead of silently inserting an empty ```output block. The scan below
// mirrors the decisions Run makes (engine.go) — same fence languages, same
// directive forms, same "skip a fenced block once it has been consumed" rules.
// =============================================================================

// StepKind classifies a runnable line.
type StepKind int

const (
	// StepNone is the zero value: the line runs nothing.
	StepNone StepKind = iota
	// StepList is a "- command" line (incl. write:/prompt:/include:/! forms).
	StepList
	// StepNumbered is a "N. command" line, which also binds {N}.
	StepNumbered
	// StepFence is a fenced block the engine executes (```bash) or writes out
	// (```yaml and friends).
	StepFence
)

// Step is one runnable line of a runbook.
type Step struct {
	Row  int      // 0-based line index
	Kind StepKind // what the engine will do with it
	Text string   // command body / directive, for display in the status bar
	Done bool     // carries the executed marker (see doneMarker)
}

// isExecLang reports whether a fence language token marks a block the engine
// executes as a shell script.
func isExecLang(lang string) bool {
	switch lang {
	case "bash", "sh", "zsh":
		return true
	}
	return false
}

// fenceStepText summarises a fenced block for display: its first non-blank
// content line, or the fence info string when the block is empty.
func fenceStepText(info, content string) string {
	for _, l := range strings.Split(content, "\n") {
		if s := strings.TrimSpace(l); s != "" {
			return s
		}
	}
	return strings.TrimSpace(info)
}

// RunnableSteps returns every line of the document the engine would run, in
// document order. Lines inside fenced blocks (```output blocks in particular,
// which hold captured command echoes that look exactly like list commands) are
// never reported as steps.
func RunnableSteps(lines []string, tabWidth int) []Step {
	if tabWidth <= 0 {
		tabWidth = DefaultIndentSpaces
	}
	var steps []Step
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		done := strings.HasPrefix(raw, doneMarker)

		// Fenced block: executable (```bash) or auto-written (```yaml …) blocks
		// are steps; anything else (```output, plain ```) is inert. Either way
		// its body is skipped so it is never scanned for list commands.
		if reFenceOpen.MatchString(raw) {
			fb, ok := CollectFencedBlock(lines, i)
			if !ok {
				continue
			}
			lang := fenceLang(fb.Info)
			if isExecLang(lang) || isConfigLang(lang) {
				steps = append(steps, Step{
					Row:  i,
					Kind: StepFence,
					Text: fenceStepText(fb.Info, fb.Content),
					Done: done,
				})
			}
			i += fb.Consumed - 1
			continue
		}

		norm := NormalizeIndent(raw, tabWidth)

		// write: consumes the fenced block that follows it — skip that block so
		// its contents are not mistaken for further steps.
		if wd, ok := ParseWriteDirective(norm); ok {
			steps = append(steps, Step{Row: i, Kind: StepList, Text: "write: " + wd.FilePath, Done: done})
			if fb, ok2 := CollectFencedBlock(lines, i+1); ok2 {
				i += fb.Consumed
			}
			continue
		}
		if lc, ok := DetectListCommand(norm); ok {
			steps = append(steps, Step{Row: i, Kind: StepList, Text: strings.TrimSpace(lc.Body), Done: done})
			continue
		}
		// A numbered line carries no indentation of its own, but may carry the
		// executed marker — strip it before matching.
		if n, ok := DetectNumbered(stripDoneMarker(raw)); ok {
			steps = append(steps, Step{Row: i, Kind: StepNumbered, Text: strings.TrimSpace(n.Body), Done: done})
			continue
		}
	}
	return steps
}

// FirstStepIn returns the first step whose row is in [from, to), i.e. the step
// an F5 started at `from` would run first. `to` may be past the end of the
// document; a negative `to` means "to the end".
func FirstStepIn(lines []string, from, to, tabWidth int) (Step, bool) {
	if to < 0 {
		to = len(lines)
	}
	for _, s := range RunnableSteps(lines, tabWidth) {
		if s.Row >= from && s.Row < to {
			return s, true
		}
	}
	return Step{}, false
}

// HasRunnableStep reports whether [from, to) contains anything the engine would
// run.
func HasRunnableStep(lines []string, from, to, tabWidth int) bool {
	_, ok := FirstStepIn(lines, from, to, tabWidth)
	return ok
}

// NextStepRow returns the row of the first step strictly after `from`.
func NextStepRow(lines []string, from, tabWidth int) (int, bool) {
	for _, s := range RunnableSteps(lines, tabWidth) {
		if s.Row > from {
			return s.Row, true
		}
	}
	return 0, false
}

// PrevStepRow returns the row of the last step strictly before `from`.
func PrevStepRow(lines []string, from, tabWidth int) (int, bool) {
	steps := RunnableSteps(lines, tabWidth)
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Row < from {
			return steps[i].Row, true
		}
	}
	return 0, false
}

// StepRowAtOrAfter returns the row of the first step at or after `from` — where
// the cursor should land when advancing past a block that has just run.
func StepRowAtOrAfter(lines []string, from, tabWidth int) (int, bool) {
	for _, s := range RunnableSteps(lines, tabWidth) {
		if s.Row >= from {
			return s.Row, true
		}
	}
	return 0, false
}

// truncateRunes shortens s to at most n runes, marking the cut with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
