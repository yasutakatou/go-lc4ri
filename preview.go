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
	reMdHeading      = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reMdBullet       = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reMdOrdered      = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
	reMdQuote        = regexp.MustCompile(`^(\s*)>\s?(.*)$`)
	reMdInlineCode   = regexp.MustCompile("`([^`]+)`")
	reMdBold         = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reMdItalic       = regexp.MustCompile(`\*([^*]+)\*`)
	reMdTableSepCell = regexp.MustCompile(`^:?-{1,}:?$`)
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
	for i := 0; i < len(lines); i++ {
		line := lines[i]
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
		if strings.Contains(line, "|") && i+1 < len(lines) && isTableSeparatorLine(lines[i+1]) {
			header := splitTableCells(line)
			aligns := parseTableAlign(lines[i+1])
			j := i + 2
			var rows [][]string
			for j < len(lines) {
				rowLine := lines[j]
				if strings.TrimSpace(rowLine) == "" || !strings.Contains(rowLine, "|") ||
					reFenceOpen.MatchString(rowLine) || HorizonCheck(rowLine) {
					break
				}
				rows = append(rows, splitTableCells(rowLine))
				j++
			}
			out = append(out, renderMarkdownTable(header, aligns, rows)...)
			i = j - 1
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

// splitTableCells splits a "| a | b |" (or "a | b", without outer pipes)
// table row into its raw, unrendered cell contents.
func splitTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// isTableSeparatorLine reports whether line is a GFM table header separator,
// e.g. "|---|:---|---:|" or "--- | :-: | ---".
func isTableSeparatorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "-") {
		return false
	}
	cells := splitTableCells(trimmed)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !reMdTableSepCell.MatchString(strings.TrimSpace(c)) {
			return false
		}
	}
	return true
}

// parseTableAlign derives a per-column alignment ("left"/"right"/"center")
// from a table separator row's ":---"/"---:"/":---:" cells.
func parseTableAlign(sepLine string) []string {
	cells := splitTableCells(sepLine)
	aligns := make([]string, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(c)
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		switch {
		case left && right:
			aligns[i] = "center"
		case right:
			aligns[i] = "right"
		default:
			aligns[i] = "left"
		}
	}
	return aligns
}

// tableVisibleWidth approximates a cell's rendered width by stripping the
// inline markdown markers (which become tview tags, not visible characters)
// before counting runes.
func tableVisibleWidth(s string) int {
	s = reMdInlineCode.ReplaceAllString(s, "$1")
	s = reMdBold.ReplaceAllString(s, "$1")
	s = reMdItalic.ReplaceAllString(s, "$1")
	return len([]rune(strings.TrimSpace(s)))
}

// padTableCells extends cells with empty strings so every row in a table has
// the same column count.
func padTableCells(cells []string, n int) []string {
	for len(cells) < n {
		cells = append(cells, "")
	}
	return cells[:n]
}

// padTableCell renders and pads a single cell's text to width columns,
// honouring the column's alignment.
func padTableCell(raw string, width int, align string) string {
	trimmed := strings.TrimSpace(raw)
	pad := width - tableVisibleWidth(trimmed)
	if pad < 0 {
		pad = 0
	}
	rendered := renderInline(trimmed)
	switch align {
	case "right":
		return strings.Repeat(" ", pad) + rendered
	case "center":
		l := pad / 2
		return strings.Repeat(" ", l) + rendered + strings.Repeat(" ", pad-l)
	default:
		return rendered + strings.Repeat(" ", pad)
	}
}

// tableBorder draws a horizontal border line for the given column widths,
// using left/mid/right box-drawing corner characters.
func tableBorder(widths []int, left, mid, right string) string {
	parts := make([]string, len(widths))
	for i, w := range widths {
		parts[i] = strings.Repeat("─", w+2)
	}
	return "[gray]" + left + strings.Join(parts, mid) + right + "[-:-:-]"
}

// tableRow renders one data (or header) row as a "│ cell │ cell │" line.
func tableRow(cells []string, aligns []string, widths []int, header bool) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		cell := padTableCell(c, widths[i], aligns[i])
		if header {
			cell = "[::b]" + cell + "[::-]"
		}
		parts[i] = " " + cell + " "
	}
	return "[gray]│[-:-:-]" + strings.Join(parts, "[gray]│[-:-:-]") + "[gray]│[-:-:-]"
}

// renderMarkdownTable renders a full GFM table (header, alignment row and
// body rows) as box-drawn, colour-tagged lines for the tview preview.
func renderMarkdownTable(header []string, aligns []string, rows [][]string) []string {
	numCols := len(header)
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	for len(aligns) < numCols {
		aligns = append(aligns, "left")
	}
	header = padTableCells(header, numCols)
	for i := range rows {
		rows[i] = padTableCells(rows[i], numCols)
	}

	widths := make([]int, numCols)
	for c := 0; c < numCols; c++ {
		widths[c] = tableVisibleWidth(header[c])
	}
	for _, r := range rows {
		for c := 0; c < numCols; c++ {
			if w := tableVisibleWidth(r[c]); w > widths[c] {
				widths[c] = w
			}
		}
	}
	for c := range widths {
		if widths[c] < 3 {
			widths[c] = 3
		}
	}

	out := make([]string, 0, len(rows)+4)
	out = append(out, tableBorder(widths, "┌", "┬", "┐"))
	out = append(out, tableRow(header, aligns, widths, true))
	out = append(out, tableBorder(widths, "├", "┼", "┤"))
	for _, r := range rows {
		out = append(out, tableRow(r, aligns, widths, false))
	}
	out = append(out, tableBorder(widths, "└", "┴", "┘"))
	return out
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
