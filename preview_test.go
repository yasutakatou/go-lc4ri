package main

import (
	"strings"
	"testing"
)

// TestRenderMarkdownPreviewStyling checks that the F3 preview renderer applies
// the expected tview colour tags for headings, emphasis and lists, and that it
// preserves literal square brackets (Markdown links, this tool's {var}
// bindings) instead of misparsing them as style tags.
func TestRenderMarkdownPreviewStyling(t *testing.T) {
	src := "# Title\n" +
		"Some **bold**, *italic* and `code`.\n" +
		"- item one\n" +
		"1. step one\n" +
		"> a quote\n" +
		"See [the docs](https://example.com) and {host}.\n"
	got := renderMarkdownPreview(src)

	cases := []string{
		"[yellow::b]Title[-:-:-]",
		"[::b]bold[::-]",
		"[::i]italic[::-]",
		"[gray]code[-]",
		"• item one",
		"1. step one",
		"[gray::i]│ a quote[-:-:-]",
		"[the docs[](https://example.com) and {host}.",
	}
	for _, want := range cases {
		if !strings.Contains(got, want) {
			t.Fatalf("renderMarkdownPreview output missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestRenderMarkdownPreviewCodeFence checks that fenced code content is styled
// as a block and is not run through inline emphasis parsing.
func TestRenderMarkdownPreviewCodeFence(t *testing.T) {
	src := "```bash\n*not italic* here\n```\n"
	got := renderMarkdownPreview(src)
	if !strings.Contains(got, "*not italic* here") {
		t.Fatalf("fenced code line should stay verbatim, got:\n%s", got)
	}
	if strings.Contains(got, "[::i]") {
		t.Fatalf("fenced code line should not receive inline emphasis styling, got:\n%s", got)
	}
}
