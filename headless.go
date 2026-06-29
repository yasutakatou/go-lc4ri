package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// runHeadless executes a runbook without a UI (CI / scripting use) and,
// optionally, writes an HTML or Markdown report. It returns a process exit code.
func runHeadless(file string, dryRun bool, profile, report string) int {
	abs, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "code-lc4ri:", err)
		return 2
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "code-lc4ri:", err)
		return 2
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	cfg := LoadConfig()
	cfg.ConfirmDangerous = false // non-interactive: never block on a modal
	eng := NewEngine(cfg, filepath.Dir(abs))

	opts := RunOptions{
		DryRun:  dryRun,
		Profile: profile,
		OnCommand: func(cmd string) {
			fmt.Println("▶ " + cmd)
		},
		OnOutput: func(chunk string) {
			fmt.Print(chunk)
		},
		OnInfo: func(text string) {
			fmt.Println(text)
		},
	}

	failures := eng.Run(lines, 0, false, opts)

	if report != "" {
		if err := writeReport(report, eng.Entries); err != nil {
			fmt.Fprintln(os.Stderr, "code-lc4ri: report:", err)
		} else {
			fmt.Println("report written to", report)
		}
	}
	if failures == 0 {
		return 0
	}
	return 1
}

func writeReport(path string, entries []ReportEntry) error {
	if strings.HasSuffix(path, ".html") {
		var b strings.Builder
		b.WriteString("<!doctype html><meta charset=utf-8><style>section{border-left:4px solid #aaa;padding:.5em 1em;margin:1em 0}.ok{border-color:#3a3}.ng{border-color:#c33}pre{background:#111;color:#eee;padding:1em;overflow:auto}</style><h1>lc4ri report</h1>")
		for _, e := range entries {
			cls := "ng"
			if e.OK {
				cls = "ok"
			}
			b.WriteString(fmt.Sprintf("<section class=%q><h3>%s</h3><pre>%s</pre></section>\n",
				cls, html.EscapeString(e.Command), html.EscapeString(e.Output)))
		}
		return os.WriteFile(path, []byte(b.String()), 0o644)
	}
	var b strings.Builder
	b.WriteString("# lc4ri report\n\n")
	for _, e := range entries {
		mark := "❌"
		if e.OK {
			mark = "✅"
		}
		b.WriteString(fmt.Sprintf("## %s %s\n\n```\n%s\n```\n\n", mark, e.Command, e.Output))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
