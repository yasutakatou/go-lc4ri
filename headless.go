package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// reportStats summarizes a run for the report header/footer.
type reportStats struct {
	Total    int
	Passed   int
	Failed   int
	Duration time.Duration
}

func summarize(entries []ReportEntry) reportStats {
	var s reportStats
	for _, e := range entries {
		s.Total++
		if e.OK {
			s.Passed++
		} else {
			s.Failed++
		}
		s.Duration += e.Duration
	}
	return s
}

// writeReport writes an execution report in the format implied by the file
// extension: .html, .xml (JUnit), .json, or Markdown otherwise. Every format
// carries the per-command exit code and duration plus a run summary.
func writeReport(path string, entries []ReportEntry) error {
	switch {
	case strings.HasSuffix(path, ".html"):
		return os.WriteFile(path, []byte(reportHTML(entries)), 0o644)
	case strings.HasSuffix(path, ".xml"):
		return os.WriteFile(path, reportJUnit(entries), 0o644)
	case strings.HasSuffix(path, ".json"):
		return os.WriteFile(path, reportJSON(entries), 0o644)
	default:
		return os.WriteFile(path, []byte(reportMarkdown(entries)), 0o644)
	}
}

func reportHTML(entries []ReportEntry) string {
	s := summarize(entries)
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=utf-8><style>body{font-family:system-ui,sans-serif}section{border-left:4px solid #aaa;padding:.5em 1em;margin:1em 0}.ok{border-color:#3a3}.ng{border-color:#c33}pre{background:#111;color:#eee;padding:1em;overflow:auto}.meta{color:#888;font-size:.85em}</style><h1>lc4ri report</h1>")
	b.WriteString(fmt.Sprintf("<p class=meta>%d commands — <b>%d passed</b>, <b>%d failed</b> in %s</p>\n",
		s.Total, s.Passed, s.Failed, s.Duration.Round(time.Millisecond)))
	for _, e := range entries {
		cls := "ng"
		if e.OK {
			cls = "ok"
		}
		b.WriteString(fmt.Sprintf("<section class=%q><h3>%s</h3><p class=meta>exit %d · %s</p><pre>%s</pre></section>\n",
			cls, html.EscapeString(e.Command), e.Code, e.Duration.Round(time.Millisecond), html.EscapeString(e.Output)))
	}
	return b.String()
}

func reportMarkdown(entries []ReportEntry) string {
	s := summarize(entries)
	var b strings.Builder
	b.WriteString("# lc4ri report\n\n")
	b.WriteString(fmt.Sprintf("%d commands — **%d passed**, **%d failed** in %s\n\n",
		s.Total, s.Passed, s.Failed, s.Duration.Round(time.Millisecond)))
	for _, e := range entries {
		mark := "❌"
		if e.OK {
			mark = "✅"
		}
		b.WriteString(fmt.Sprintf("## %s %s\n\n_exit %d · %s_\n\n```\n%s\n```\n\n",
			mark, e.Command, e.Code, e.Duration.Round(time.Millisecond), e.Output))
	}
	return b.String()
}

// JUnit XML structures. A single <testsuite> whose <testcase>s are the run's
// commands, so CI systems (GitHub Actions, GitLab, Jenkins) render the runbook
// as a test report directly.
type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     float64      `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}
type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Time     float64     `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}
type junitCase struct {
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}
type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func reportJUnit(entries []ReportEntry) []byte {
	s := summarize(entries)
	suite := junitSuite{
		Name:     "lc4ri",
		Tests:    s.Total,
		Failures: s.Failed,
		Time:     s.Duration.Seconds(),
	}
	for _, e := range entries {
		c := junitCase{Name: e.Command, Time: e.Duration.Seconds(), SystemOut: e.Output}
		if !e.OK {
			c.Failure = &junitFailure{
				Message: fmt.Sprintf("exit %d", e.Code),
				Body:    e.Output,
			}
		}
		suite.Cases = append(suite.Cases, c)
	}
	doc := junitSuites{Tests: s.Total, Failures: s.Failed, Time: s.Duration.Seconds(), Suites: []junitSuite{suite}}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return []byte(xml.Header)
	}
	return append([]byte(xml.Header), out...)
}

// jsonEntry is the machine-readable form of a ReportEntry.
type jsonEntry struct {
	Command    string `json:"command"`
	Output     string `json:"output"`
	Code       int    `json:"code"`
	OK         bool   `json:"ok"`
	Timestamp  string `json:"timestamp"`
	DurationMs int64  `json:"durationMs"`
}

func reportJSON(entries []ReportEntry) []byte {
	s := summarize(entries)
	items := make([]jsonEntry, 0, len(entries))
	for _, e := range entries {
		items = append(items, jsonEntry{
			Command:    e.Command,
			Output:     e.Output,
			Code:       e.Code,
			OK:         e.OK,
			Timestamp:  e.Ts.Format(time.RFC3339),
			DurationMs: e.Duration.Milliseconds(),
		})
	}
	doc := struct {
		Total      int         `json:"total"`
		Passed     int         `json:"passed"`
		Failed     int         `json:"failed"`
		DurationMs int64       `json:"durationMs"`
		Commands   []jsonEntry `json:"commands"`
	}{s.Total, s.Passed, s.Failed, s.Duration.Milliseconds(), items}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(out, '\n')
}
