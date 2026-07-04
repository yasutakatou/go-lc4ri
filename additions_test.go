package main

import (
	"strings"
	"testing"
	"time"
)

// TestExtractBinding pins the result-binding operators: the new single-char "@"
// (which must be space-separated so an "ssh user@{host}" target is left alone),
// plus the legacy "→" / "->" for backward compatibility.
func TestExtractBinding(t *testing.T) {
	cases := []struct {
		in       string
		wantCmd  string
		wantName string
	}{
		{"hostname @ {host}", "hostname", "host"},
		{"ls -la @ {files}", "ls -la", "files"},
		{"hostname → {host}", "hostname", "host"},
		{"hostname -> {host}", "hostname", "host"},
		{"echo hi", "echo hi", ""},
		// "@" without a leading space is part of the command, not a binding.
		{"ssh user@{host}", "ssh user@{host}", ""},
	}
	for _, c := range cases {
		gotCmd, gotName := ExtractBinding(c.in)
		if gotCmd != c.wantCmd || gotName != c.wantName {
			t.Errorf("ExtractBinding(%q) = (%q, %q), want (%q, %q)",
				c.in, gotCmd, gotName, c.wantCmd, c.wantName)
		}
	}
}

// TestListCommandBinding checks that a plain "- cmd @ {name}" list command binds
// its output to a named variable (previously only numbered "N." lines did).
func TestListCommandBinding(t *testing.T) {
	cfg := DefaultConfig()
	eng := NewEngine(cfg, ".")
	var out strings.Builder
	opts := RunOptions{OnOutput: func(s string) { out.WriteString(s) }}
	// echo runs in the real shell; the AND-chain then reads {greeting}.
	eng.Run([]string{"- echo hello @ {greeting}", "- echo got {greeting}"}, 0, false, opts)
	if got := eng.Vars.Named["greeting"]; got != "hello" {
		t.Fatalf("binding {greeting} = %q, want %q", got, "hello")
	}
	if !strings.Contains(out.String(), "got hello") {
		t.Errorf("expanded command output = %q, want it to contain %q", out.String(), "got hello")
	}
}

// TestReportFormats checks the JUnit and JSON report writers carry the exit
// code, duration and pass/fail summary.
func TestReportFormats(t *testing.T) {
	entries := []ReportEntry{
		{Command: "echo ok", Output: "ok", Code: 0, OK: true, Ts: time.Unix(0, 0), Duration: 50 * time.Millisecond},
		{Command: "false", Output: "", Code: 1, OK: false, Ts: time.Unix(0, 0), Duration: 20 * time.Millisecond},
	}

	junit := string(reportJUnit(entries))
	for _, want := range []string{`tests="2"`, `failures="1"`, `<testcase name="echo ok"`, `<failure message="exit 1"`} {
		if !strings.Contains(junit, want) {
			t.Errorf("JUnit report missing %q\n%s", want, junit)
		}
	}

	js := string(reportJSON(entries))
	for _, want := range []string{`"total": 2`, `"passed": 1`, `"failed": 1`, `"durationMs": 50`, `"code": 1`} {
		if !strings.Contains(js, want) {
			t.Errorf("JSON report missing %q\n%s", want, js)
		}
	}
}

// TestStripRunArtifacts checks that Run-All's cleanup drops ```output blocks and
// executed-line markers, leaving the underlying runbook intact.
func TestStripRunArtifacts(t *testing.T) {
	in := []string{
		addDoneMarker("- echo one"),
		"- echo two",
		"```output",
		"[ echo one ] now",
		"one",
		"```",
		"- echo three",
	}
	got := stripRunArtifacts(in)
	want := []string{"- echo one", "- echo two", "- echo three"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stripRunArtifacts = %q, want %q", got, want)
	}
}
