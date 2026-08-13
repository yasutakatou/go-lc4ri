package main

import (
	"strings"
	"testing"
)

// sampleDoc is a runbook exercising every step form plus the shapes that must
// NOT be reported as steps: prose, an ```output block whose captured text looks
// exactly like a list command, and the body of a write: directive's block.
func sampleDoc() []string {
	return strings.Split(`# Title

Some prose describing what happens next.

- echo one
  - echo two
1. hostname @ {host}

`+"```bash"+`
echo scripted
`+"```"+`

- write: conf.yaml
  `+"```yaml"+`
  - not: a step
  key: value
  `+"```"+`

`+"```output"+`
[ echo one ] now
- echo this is captured output, not a step
`+"```"+`

- echo last
`, "\n")
}

func TestRunnableStepsFindsEveryForm(t *testing.T) {
	lines := sampleDoc()
	steps := RunnableSteps(lines, DefaultIndentSpaces)

	var got []string
	for _, s := range steps {
		got = append(got, lines[s.Row])
	}
	want := []string{
		"- echo one",
		"  - echo two",
		"1. hostname @ {host}",
		"```bash",
		"- write: conf.yaml",
		"- echo last",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("RunnableSteps found\n  %q\nwant\n  %q", got, want)
	}

	// The kinds and display text feed the status bar.
	if steps[2].Kind != StepNumbered || steps[2].Text != "hostname @ {host}" {
		t.Errorf("numbered step = %+v", steps[2])
	}
	if steps[3].Kind != StepFence || steps[3].Text != "echo scripted" {
		t.Errorf("fenced step = %+v, want the block's first line as text", steps[3])
	}
	if steps[4].Text != "write: conf.yaml" {
		t.Errorf("write step text = %q", steps[4].Text)
	}
}

// TestRunnableStepsIgnoresOutputBlocks is the regression guard for the failure
// the users hit: a captured "- echo …" line inside an ```output block must not
// look like a step.
func TestRunnableStepsIgnoresOutputBlocks(t *testing.T) {
	lines := []string{
		"```output",
		"- echo captured",
		"1. also captured",
		"```",
	}
	if steps := RunnableSteps(lines, DefaultIndentSpaces); len(steps) != 0 {
		t.Fatalf("output block reported %d steps: %+v", len(steps), steps)
	}
}

// TestRunnableStepsFenceLanguages pins which fenced blocks count as steps —
// exactly the set the engine executes or writes out.
func TestRunnableStepsFenceLanguages(t *testing.T) {
	cases := []struct {
		lang string
		want bool
	}{
		{"bash", true}, {"sh", true}, {"zsh", true},
		{"yaml", true}, {"json", true},
		{"output", false}, {"", false}, {"shell", false}, {"text", false},
	}
	for _, c := range cases {
		lines := []string{"```" + c.lang, "echo hi", "```"}
		got := len(RunnableSteps(lines, DefaultIndentSpaces)) == 1
		if got != c.want {
			t.Errorf("fence ```%s runnable = %v, want %v", c.lang, got, c.want)
		}
	}
}

func TestRunnableStepsMarksExecutedLines(t *testing.T) {
	lines := []string{addDoneMarker("- echo one"), "- echo two", addDoneMarker("1. echo three")}
	steps := RunnableSteps(lines, DefaultIndentSpaces)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3: %+v", len(steps), steps)
	}
	if !steps[0].Done || steps[1].Done || !steps[2].Done {
		t.Errorf("done flags = %v/%v/%v, want true/false/true", steps[0].Done, steps[1].Done, steps[2].Done)
	}
	// A marked numbered line must still parse as a step (the marker is two
	// leading spaces, which "N." matching would otherwise reject).
	if steps[2].Kind != StepNumbered {
		t.Errorf("marked numbered line kind = %v, want StepNumbered", steps[2].Kind)
	}
}

func TestNextPrevStepRow(t *testing.T) {
	lines := sampleDoc() // steps on rows 4, 5, 6, 8, 12, 22 (see sampleDoc)
	steps := RunnableSteps(lines, DefaultIndentSpaces)
	first, last := steps[0].Row, steps[len(steps)-1].Row

	if row, ok := NextStepRow(lines, 0, DefaultIndentSpaces); !ok || row != first {
		t.Errorf("NextStepRow from top = (%d, %v), want (%d, true)", row, ok, first)
	}
	if row, ok := PrevStepRow(lines, last, DefaultIndentSpaces); !ok || row != steps[len(steps)-2].Row {
		t.Errorf("PrevStepRow from last = (%d, %v), want (%d, true)", row, ok, steps[len(steps)-2].Row)
	}
	if _, ok := NextStepRow(lines, last, DefaultIndentSpaces); ok {
		t.Errorf("NextStepRow past the last step should report none")
	}
	if _, ok := PrevStepRow(lines, first, DefaultIndentSpaces); ok {
		t.Errorf("PrevStepRow before the first step should report none")
	}
	// Both are strict: standing on a step jumps off it, never back to itself.
	if row, ok := NextStepRow(lines, first, DefaultIndentSpaces); !ok || row <= first {
		t.Errorf("NextStepRow on a step = %d, want a row past %d", row, first)
	}
}

func TestStepRowAtOrAfter(t *testing.T) {
	lines := sampleDoc()
	first := RunnableSteps(lines, DefaultIndentSpaces)[0].Row
	if row, ok := StepRowAtOrAfter(lines, first, DefaultIndentSpaces); !ok || row != first {
		t.Errorf("StepRowAtOrAfter(%d) = (%d, %v), want (%d, true)", first, row, ok, first)
	}
}

// TestHasRunnableStepMatchesTheEnginesReach checks the pre-flight used by F5:
// a cursor in the prose above a block sees that block, a cursor in the trailing
// documentation sees nothing.
func TestHasRunnableStepInRange(t *testing.T) {
	lines := []string{
		"# Title",       // 0
		"prose",         // 1
		"- echo one",    // 2
		"",              // 3
		"more prose",    // 4
		"and even more", // 5
	}
	boundary := FindBlockBoundary(lines, 1, DefaultIndentSpaces)
	if !HasRunnableStep(lines, 1, boundary, DefaultIndentSpaces) {
		t.Errorf("prose above a command should still reach the command (boundary %d)", boundary)
	}
	if HasRunnableStep(lines, 4, len(lines), DefaultIndentSpaces) {
		t.Errorf("trailing prose reported a runnable step")
	}
	// Inside a fenced block that the engine does not execute: nothing to run.
	fenced := []string{"```text", "echo nope", "```"}
	if HasRunnableStep(fenced, 1, len(fenced), DefaultIndentSpaces) {
		t.Errorf("a non-executable fenced block reported a runnable step")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("echo hello", 20); got != "echo hello" {
		t.Errorf("short string was altered: %q", got)
	}
	if got := truncateRunes("kubectl get nodes", 8); got != "kubectl…" {
		t.Errorf("truncateRunes = %q, want %q", got, "kubectl…")
	}
	// Multi-byte input must be cut on rune boundaries.
	if got := truncateRunes("あいうえお", 3); got != "あい…" {
		t.Errorf("truncateRunes(multibyte) = %q, want %q", got, "あい…")
	}
}

// TestRunWithReportsRanAny is the engine half of "no silent no-op": a run that
// matches no command must say so instead of reporting a clean zero-failure run.
func TestRunWithReportsRanAny(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		ranAny bool
	}{
		{"prose only", []string{"# Title", "just documentation"}, false},
		{"non-executable fence", []string{"```text", "echo nope", "```"}, false},
		{"inside a bash block", []string{"echo orphaned", "```"}, false},
		{"list command", []string{"- echo hi"}, true},
		{"numbered command", []string{"1. echo hi"}, true},
		{"bash block", []string{"```bash", "echo hi", "```"}, true},
	}
	for _, c := range cases {
		eng := NewEngine(DefaultConfig(), ".")
		res := eng.RunWith(c.lines, 0, false, RunOptions{DryRun: true})
		if res.RanAny != c.ranAny {
			t.Errorf("%s: RanAny = %v, want %v", c.name, res.RanAny, c.ranAny)
		}
	}
}

// TestRunKeepsReturningFailures pins the compatibility wrapper.
func TestRunKeepsReturningFailures(t *testing.T) {
	eng := NewEngine(DefaultConfig(), ".")
	if got := eng.Run([]string{"- echo hi"}, 0, false, RunOptions{DryRun: true}); got != 0 {
		t.Errorf("Run = %d failures, want 0", got)
	}
}
