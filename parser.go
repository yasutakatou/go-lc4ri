package main

import (
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// LC4RI parsing helpers — a faithful Go port of the helpers exported from
// src/extension.ts. The CLI shares the exact same runbook semantics as the
// VS Code extension so a document behaves identically in both.
// =============================================================================

// Variables holds the runtime variable state for a runbook session.
type Variables struct {
	Num    map[string]string // {1}..{9}
	Named  map[string]string // {name}
	Prev   string            // {$PREV}
	Status int               // {$STATUS}
	Cwd    string            // {$CWD}
}

// NewVariables returns an initialised, empty variable set.
func NewVariables() *Variables {
	return &Variables{Num: map[string]string{}, Named: map[string]string{}}
}

// DefaultDangerousPatterns mirrors DEFAULT_DANGEROUS_PATTERNS in extension.ts.
var DefaultDangerousPatterns = []string{
	// Unix / Linux
	`\brm\s+-rf?\s+/`,
	`\bdd\s+if=`,
	`\bmkfs\.`,
	`\bshutdown\b`,
	`\breboot\b`,
	`:\(\)\s*\{\s*:\|:&\s*\};:`,
	`curl\s+[^|]+\|\s*(?:sh|bash)`,
	`wget\s+[^|]+\|\s*(?:sh|bash)`,
	`>\s*/dev/sd[a-z]`,
	// Windows
	`\brd\s+/s\s+/q\b`,
	`\bformat\s+[A-Za-z]:`,
	`\bdel\s+/[fFsS].*\s+/[fFsS]`,
	`Remove-Item\b.*-Recurse\b.*-Force\b`,
	`Remove-Item\b.*-Force\b.*-Recurse\b`,
}

// DefaultIndentSpaces — 2-space indentation maps to one AND-chain level.
const DefaultIndentSpaces = 2

var (
	reHorizonStar  = regexp.MustCompile(`^(?:\*\s?){3,}\s*$`)
	reHorizonDash  = regexp.MustCompile(`^(?:-\s?){3,}\s*$`)
	reListCommand  = regexp.MustCompile(`^(\t*)- (.*)$`)
	reNumbered     = regexp.MustCompile(`^([1-9])\.\s+(.*)$`)
	reBinding      = regexp.MustCompile(`\s*(?:→|->)\s*\{([A-Za-z_][A-Za-z0-9_]*)\}\s*$`)
	reEnvDirective = regexp.MustCompile(`^#\s*env:\s*(.+)$`)
	reInclude      = regexp.MustCompile(`(?i)^include:\s+`)
	reOpen         = regexp.MustCompile(`(?i)^open:\s+`)
	reBang         = regexp.MustCompile(`^!\s+`)
	reParallel     = regexp.MustCompile(`(?i)^\[parallel\]\s*`)
	reRetry        = regexp.MustCompile(`(?i)^\[retry:\s*(\d+)(?:\s*,\s*(?:interval:)?\s*(\d+)(s|ms)?)?\]\s*`)
	reWrite        = regexp.MustCompile(`(?i)^(\t*)- write:\s+(.+)$`)
	rePrompt       = regexp.MustCompile(`(?i)^(\t*)- prompt:\s+(secret\s+)?\{([A-Za-z_][A-Za-z0-9_]*)\}\s+(.+)$`)
	reVar          = regexp.MustCompile(`\{([^{}\s]+)\}`)
	reFenceOpen    = regexp.MustCompile("^(\\s*)(`{3,}|~{3,})[^\\n]*$")
	reCdCommand    = regexp.MustCompile(`^cd\s+(.+)$`)
	reExportCmd    = regexp.MustCompile(`^export\s+([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
)

// RegTab returns the regexp that matches a list command at AND-chain depth cnt.
func RegTab(cnt int) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.Repeat("\t", cnt) + "- ")
}

// NormalizeIndent converts a line's leading whitespace into tab-based depth so
// that 2-space Markdown indentation maps onto AND-chain levels.
func NormalizeIndent(line string, tabWidth int) string {
	if tabWidth <= 0 {
		tabWidth = DefaultIndentSpaces
	}
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	ws, rest := line[:i], line[i:]
	if len(ws) == 0 {
		return line
	}
	col := 0
	for _, c := range ws {
		if c == '\t' {
			col += tabWidth - (col % tabWidth)
		} else {
			col++
		}
	}
	if col == 0 {
		return rest
	}
	tabs := (col + tabWidth - 1) / tabWidth
	return strings.Repeat("\t", tabs) + rest
}

// HorizonCheck reports whether a line is a horizon separator (*** or ---).
func HorizonCheck(line string) bool {
	return reHorizonStar.MatchString(line) || reHorizonDash.MatchString(line)
}

// ListCommand is the parsed result of a "- command" line.
type ListCommand struct {
	Depth int
	Body  string
}

// DetectListCommand parses a tab-indented list line.
func DetectListCommand(line string) (ListCommand, bool) {
	m := reListCommand.FindStringSubmatch(line)
	if m == nil {
		return ListCommand{}, false
	}
	return ListCommand{Depth: len(m[1]), Body: m[2]}, true
}

// Numbered is the parsed result of a "N. command" line.
type Numbered struct {
	Idx  string
	Body string
}

// DetectNumbered parses a numbered command line that creates {N}.
func DetectNumbered(line string) (Numbered, bool) {
	m := reNumbered.FindStringSubmatch(line)
	if m == nil {
		return Numbered{}, false
	}
	return Numbered{Idx: m[1], Body: m[2]}, true
}

// ExtractBinding splits a trailing "→ {name}" binding off the command body.
func ExtractBinding(body string) (string, string) {
	loc := reBinding.FindStringSubmatchIndex(body)
	if loc == nil {
		return body, ""
	}
	name := body[loc[2]:loc[3]]
	return body[:loc[0]], name
}

// AssertKind enumerates the supported assertion forms.
type AssertKind int

const (
	AssertNone AssertKind = iota
	AssertContains
	AssertEquals
	AssertStatus
	AssertRegex
)

// Assert is a parsed "assert:" directive.
type Assert struct {
	Kind   AssertKind
	Arg    string
	Status int
	Re     *regexp.Regexp
}

var (
	reAssert         = regexp.MustCompile(`(?i)^assert\s*:\s*(.+)$`)
	reAssertContains = regexp.MustCompile(`(?i)^contains\s+(?:"([^"]*)"|'([^']*)'|(\S.*))$`)
	reAssertEquals   = regexp.MustCompile(`(?i)^equals\s+(?:"([^"]*)"|'([^']*)'|(\S.*))$`)
	reAssertStatus   = regexp.MustCompile(`(?i)^status\s*==\s*(-?\d+)$`)
	reAssertRegex    = regexp.MustCompile(`(?i)^regex\s+/(.+)/$`)
)

// ParseAssert parses an "assert: ..." command body.
func ParseAssert(body string) (Assert, bool) {
	m := reAssert.FindStringSubmatch(body)
	if m == nil {
		return Assert{}, false
	}
	rest := strings.TrimSpace(m[1])
	if r := reAssertContains.FindStringSubmatch(rest); r != nil {
		return Assert{Kind: AssertContains, Arg: firstNonEmpty(r[1], r[2], r[3])}, true
	}
	if r := reAssertEquals.FindStringSubmatch(rest); r != nil {
		return Assert{Kind: AssertEquals, Arg: firstNonEmpty(r[1], r[2], r[3])}, true
	}
	if r := reAssertStatus.FindStringSubmatch(rest); r != nil {
		n, _ := strconv.Atoi(r[1])
		return Assert{Kind: AssertStatus, Status: n}, true
	}
	if r := reAssertRegex.FindStringSubmatch(rest); r != nil {
		re, err := regexp.Compile(r[1])
		if err != nil {
			return Assert{}, false
		}
		return Assert{Kind: AssertRegex, Re: re}, true
	}
	return Assert{}, false
}

// ParseEnvFile parses KEY=VALUE pairs from a .env file body.
func ParseEnvFile(content string) map[string]string {
	result := map[string]string{}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			result[key] = val
		}
	}
	return result
}

// WriteDirective is a parsed "- write: path" directive.
type WriteDirective struct {
	Depth    int
	FilePath string
}

// ParseWriteDirective parses a write directive line.
func ParseWriteDirective(line string) (WriteDirective, bool) {
	m := reWrite.FindStringSubmatch(line)
	if m == nil {
		return WriteDirective{}, false
	}
	return WriteDirective{Depth: len(m[1]), FilePath: strings.TrimSpace(m[2])}, true
}

// PromptDirective is a parsed "- prompt: {VAR} message" directive.
type PromptDirective struct {
	Depth    int
	BindName string
	Message  string
	Secret   bool
}

// ParsePromptDirective parses a prompt directive line.
func ParsePromptDirective(line string) (PromptDirective, bool) {
	m := rePrompt.FindStringSubmatch(line)
	if m == nil {
		return PromptDirective{}, false
	}
	return PromptDirective{
		Depth:    len(m[1]),
		Secret:   m[2] != "",
		BindName: m[3],
		Message:  strings.TrimSpace(m[4]),
	}, true
}

// FencedBlock is the parsed content of a fenced code block.
type FencedBlock struct {
	Content  string
	Consumed int
	Info     string // text after the opening fence (e.g. "yaml config.yml")
}

// CollectFencedBlock collects a fenced code block beginning at or after startIdx.
func CollectFencedBlock(lines []string, startIdx int) (FencedBlock, bool) {
	idx := startIdx
	for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
		idx++
	}
	if idx >= len(lines) {
		return FencedBlock{}, false
	}
	open := reFenceOpen.FindStringSubmatch(lines[idx])
	if open == nil {
		return FencedBlock{}, false
	}
	fenceIndent := len(open[1])
	fenceChar := open[2][0]
	fenceLen := len(open[2])
	info := strings.TrimSpace(lines[idx][len(open[1])+fenceLen:])
	closingRe := regexp.MustCompile("^\\s{0," + strconv.Itoa(fenceIndent) + "}[" + regexp.QuoteMeta(string(fenceChar)) + "]{" + strconv.Itoa(fenceLen) + ",}\\s*$")

	idx++
	var content []string
	for idx < len(lines) {
		l := lines[idx]
		if closingRe.MatchString(l) {
			return FencedBlock{Content: strings.Join(content, "\n"), Consumed: idx - startIdx + 1, Info: info}, true
		}
		lead := 0
		for lead < len(l) && l[lead] == ' ' {
			lead++
		}
		if lead >= fenceIndent {
			content = append(content, l[fenceIndent:])
		} else {
			content = append(content, l)
		}
		idx++
	}
	return FencedBlock{}, false
}

// DetectParallelFlag strips a leading "[parallel]" prefix.
func DetectParallelFlag(body string) (string, bool) {
	loc := reParallel.FindStringIndex(body)
	if loc == nil {
		return body, false
	}
	return body[loc[1]:], true
}

// RetryFlag holds the parsed "[retry: N, interval]" parameters.
type RetryFlag struct {
	Count    int
	Interval time.Duration
}

// DetectRetryFlag strips and parses a leading "[retry: ...]" prefix.
func DetectRetryFlag(body string) (string, RetryFlag) {
	m := reRetry.FindStringSubmatch(body)
	if m == nil {
		return body, RetryFlag{}
	}
	count, _ := strconv.Atoi(m[1])
	var interval time.Duration
	if m[2] != "" {
		n, _ := strconv.Atoi(m[2])
		if m[3] == "s" {
			interval = time.Duration(n) * time.Second
		} else {
			interval = time.Duration(n) * time.Millisecond
		}
	}
	loc := reRetry.FindStringIndex(body)
	return body[loc[1]:], RetryFlag{Count: count, Interval: interval}
}

// SubstituteVars expands {name}, {N} and {$BUILTIN} placeholders in a line.
func SubstituteVars(line string, vars *Variables) string {
	return reVar.ReplaceAllStringFunc(line, func(whole string) string {
		key := whole[1 : len(whole)-1]
		if strings.HasPrefix(key, "$") {
			switch key {
			case "$PREV":
				return strings.TrimRight(vars.Prev, "\r\n")
			case "$STATUS":
				return strconv.Itoa(vars.Status)
			case "$DATE":
				return time.Now().Format(time.RFC3339)
			case "$CWD":
				if vars.Cwd != "" {
					return vars.Cwd
				}
				wd, _ := os.Getwd()
				return wd
			case "$USER":
				if u, err := user.Current(); err == nil {
					return u.Username
				}
				return ""
			case "$HOST":
				h, _ := os.Hostname()
				return h
			default:
				return whole
			}
		}
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			if v, ok := vars.Num[key]; ok {
				return v
			}
		}
		if v, ok := vars.Named[key]; ok {
			return v
		}
		return whole
	})
}

// ApplyChangeWord applies the pre→post substitution map to a line.
func ApplyChangeWord(line string, m map[string]string) string {
	for pre, post := range m {
		line = strings.ReplaceAll(line, pre, post)
	}
	return line
}

// ApplyTemplate wraps a command with the active profile or OS template.
// Priority: profile → OS template → passthrough.
func ApplyTemplate(cmd string, cfg Config, profile string) string {
	if profile != "" {
		if tpl, ok := cfg.Profiles[profile]; ok && strings.Contains(tpl, "{COMMAND}") {
			return strings.ReplaceAll(tpl, "{COMMAND}", cmd)
		}
	}
	if tpl, ok := cfg.Template[goPlatform()]; ok && strings.Contains(tpl, "{COMMAND}") {
		return strings.ReplaceAll(tpl, "{COMMAND}", cmd)
	}
	return cmd
}

// SecurityVerdict is the result of a security policy check.
type SecurityVerdict struct {
	OK        bool
	Reason    string
	Dangerous string
}

// MatchesAny returns the first pattern that matches s, or "".
func MatchesAny(s string, patterns []string) string {
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		if re.MatchString(s) {
			return p
		}
	}
	return ""
}

// CheckSecurity evaluates deny/allow/dangerous lists for a command.
func CheckSecurity(cmd string, cfg Config) SecurityVerdict {
	if deny := MatchesAny(cmd, cfg.DenyList); deny != "" {
		return SecurityVerdict{OK: false, Reason: "denyList match: /" + deny + "/"}
	}
	if len(cfg.AllowList) > 0 {
		if MatchesAny(cmd, cfg.AllowList) == "" {
			return SecurityVerdict{OK: false, Reason: "not in allowList"}
		}
	}
	if d := MatchesAny(cmd, cfg.DangerousPatterns); d != "" {
		return SecurityVerdict{OK: true, Dangerous: d}
	}
	return SecurityVerdict{OK: true}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
