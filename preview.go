package main

import (
	"regexp"
	"strings"

	"github.com/rivo/tview"
)

// =============================================================================
// Markdown preview rendering — a lightweight, best-effort renderer that turns
// the raw runbook text into tview colour-tagged text for the read-only
// preview screen (F3). It is not a full CommonMark implementation: headings,
// emphasis, lists, quotes and fenced code get distinct styling, everything
// else is shown as plain (but safely escaped) text.
// =============================================================================

var (
	reMdHeading    = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reMdBullet     = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reMdOrdered    = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
	reMdQuote      = regexp.MustCompile(`^(\s*)>\s?(.*)$`)
	reMdInlineCode = regexp.MustCompile("`([^`]+)`")
	reMdBold       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reMdItalic     = regexp.MustCompile(`\*([^*]+)\*`)
)

// headingStyle returns the tview colour/attribute tag for a heading level
// (1-6): more prominent colours for the top levels, bold for the rest.
func headingStyle(level int) string {
	switch level {
	case 1:
		return "yellow::b"
	case 2:
		return "aqua::b"
	case 3:
		return "green::b"
	default:
		return "white::b"
	}
}

// renderMarkdownPreview converts raw Markdown source into text ready for a
// tview TextView with dynamic colours enabled.
func renderMarkdownPreview(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	inCode := false
	for _, line := range lines {
		if reFenceOpen.MatchString(line) {
			inCode = !inCode
			out = append(out, "[gray::d]"+tview.Escape(line)+"[-:-:-]")
			continue
		}
		if inCode {
			out = append(out, "[gray]"+tview.Escape(line)+"[-:-:-]")
			continue
		}
		if HorizonCheck(line) {
			out = append(out, "[gray]"+strings.Repeat("─", 60)+"[-:-:-]")
			continue
		}
		if m := reMdHeading.FindStringSubmatch(line); m != nil {
			out = append(out, "["+headingStyle(len(m[1]))+"]"+renderInline(m[2])+"[-:-:-]")
			continue
		}
		if m := reMdQuote.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+"[gray::i]│ "+renderInline(m[2])+"[-:-:-]")
			continue
		}
		if m := reMdBullet.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+"• "+renderInline(m[2]))
			continue
		}
		if m := reMdOrdered.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+m[2]+". "+renderInline(m[3]))
			continue
		}
		out = append(out, renderInline(line))
	}
	return strings.Join(out, "\n")
}

// renderInline escapes s for safe embedding in a tview dynamic-colour view
// (so literal "[...]" — Markdown links, this tool's "{var}" bindings, etc. —
// never gets mistaken for a style tag), then layers on tag-based styling for
// inline code, bold and italic markers.
func renderInline(s string) string {
	s = tview.Escape(s)
	s = reMdInlineCode.ReplaceAllString(s, "[gray]$1[-]")
	s = reMdBold.ReplaceAllString(s, "[::b]$1[::-]")
	s = reMdItalic.ReplaceAllString(s, "[::i]$1[::-]")
	return s
}
