package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/aymanbagabas/go-pty"
	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
	"github.com/rivo/tview"
)

// writerFunc adapts a function to an io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// termHideSentinel marks the helper lines injected around an F5 command (the
// echoed wrapper and the begin/end markers). Rows containing it are not painted
// in the terminal pane. It must match the token prefix built by wrapCommand.
const termHideSentinel = "@@LC4RI_"

// rowHasSentinel reports whether emulator row r contains the hide sentinel.
func rowHasSentinel(vt vt10x.Terminal, r, cols, w int) bool {
	limit := cols
	if w < limit {
		limit = w
	}
	var sb strings.Builder
	for c := 0; c < limit; c++ {
		ch := vt.Cell(c, r).Char
		if ch == 0 {
			ch = ' '
		}
		sb.WriteRune(ch)
	}
	return strings.Contains(sb.String(), termHideSentinel)
}

// Glyph attribute bits — mirrors vt10x's unexported attr* iota constants.
const (
	attrReverse   = 1 << 0
	attrUnderline = 1 << 1
	attrBold      = 1 << 2
	attrGfx       = 1 << 3
	attrItalic    = 1 << 4
	attrBlink     = 1 << 5
)

// TermView is a tview primitive embedding a real OS terminal. It launches a
// shell attached to a pseudo-terminal (cross-platform via go-pty: a Unix PTY on
// macOS/Linux, a Windows ConPTY on Windows) and renders the live screen through
// a vt10x emulator. Keystrokes are forwarded to the shell while the pane has
// focus, so it behaves like an ordinary terminal you can operate at any time.
type TermView struct {
	*tview.Box
	app    *tview.Application
	pty    pty.Pty
	cmd    *pty.Cmd
	term   vt10x.Terminal
	label  string
	kind   string // "posix" | "powershell" | "cmd"
	onExit func()
	onData func([]byte) // raw shell output tap (set before Start)

	mu         sync.Mutex
	cols, rows int
	closed     bool
}

// shellKind classifies a shell label into a command-wrapping family.
func shellKind(label string) string {
	switch label {
	case "powershell", "pwsh":
		return "powershell"
	case "cmd":
		return "cmd"
	default:
		return "posix"
	}
}

// Kind reports the shell family ("posix", "powershell" or "cmd").
func (tv *TermView) Kind() string { return tv.kind }

// shellCommand chooses the shell to launch for the current OS. A non-empty
// override (config.json "shell") takes precedence.
func shellCommand(override string) (name string, args []string, label string) {
	if override != "" {
		return resolveShell(override)
	}
	switch runtime.GOOS {
	case "windows":
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			return p, []string{"-NoLogo"}, "pwsh"
		}
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			return p, []string{"-NoLogo"}, "powershell"
		}
		c := os.Getenv("ComSpec")
		if c == "" {
			c = "cmd.exe"
		}
		return c, nil, "cmd"
	case "darwin":
		if s := os.Getenv("SHELL"); s != "" {
			return s, nil, filepath.Base(s)
		}
		return "/bin/zsh", nil, "zsh"
	default: // linux and other unix
		if s := os.Getenv("SHELL"); s != "" {
			return s, nil, filepath.Base(s)
		}
		if p, err := exec.LookPath("bash"); err == nil {
			return p, nil, "bash"
		}
		return "/bin/sh", nil, "sh"
	}
}

// resolveShell maps a configured shell name to an executable and arguments.
func resolveShell(name string) (string, []string, string) {
	switch name {
	case "powershell":
		return "powershell", []string{"-NoLogo"}, "powershell"
	case "pwsh":
		return "pwsh", []string{"-NoLogo"}, "pwsh"
	case "cmd":
		c := os.Getenv("ComSpec")
		if c == "" {
			c = "cmd.exe"
		}
		return c, nil, "cmd"
	}
	return name, nil, filepath.Base(name)
}

// termEnv returns the child environment with a sane TERM for the emulator.
func termEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	hasTerm := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") {
			kv = "TERM=xterm-256color"
			hasTerm = true
		}
		out = append(out, kv)
	}
	if !hasTerm {
		out = append(out, "TERM=xterm-256color")
	}
	return out
}

// NewTermView opens a PTY, starts the OS shell in dir and begins streaming.
func NewTermView(app *tview.Application, dir, shellOverride string) (*TermView, error) {
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	name, args, label := shellCommand(shellOverride)

	cols, rows := 80, 24
	vt := vt10x.New(vt10x.WithWriter(p), vt10x.WithSize(cols, rows))

	cmd := p.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = termEnv()

	tv := &TermView{
		app:   app,
		pty:   p,
		cmd:   cmd,
		term:  vt,
		label: label,
		kind:  shellKind(label),
		cols:  cols,
		rows:  rows,
	}
	tv.Box = tview.NewBox()
	tv.Box.SetBorder(true).SetTitle(" terminal — " + label + " ")

	_ = p.Resize(cols, rows)
	if err := cmd.Start(); err != nil {
		p.Close()
		return nil, err
	}
	return tv, nil
}

// Start begins streaming shell output. Set onData/onExit before calling it.
func (tv *TermView) Start() {
	go tv.readLoop()
	go func() {
		_ = tv.cmd.Wait()
		tv.markExit()
	}()
}

// readLoop pumps shell output into the emulator (and the onData tap, via a
// TeeReader so rune boundaries are still handled by bufio) and repaints.
func (tv *TermView) readLoop() {
	sink := writerFunc(func(p []byte) (int, error) {
		if tv.onData != nil {
			tv.onData(p)
		}
		return len(p), nil
	})
	br := bufio.NewReaderSize(io.TeeReader(tv.pty, sink), 32*1024)
	for {
		if err := tv.term.Parse(br); err != nil {
			tv.markExit()
			return
		}
		tv.app.QueueUpdateDraw(func() {})
	}
}

// markExit records that the shell has gone and notifies the owner once.
func (tv *TermView) markExit() {
	tv.mu.Lock()
	already := tv.closed
	tv.closed = true
	tv.mu.Unlock()
	if !already && tv.onExit != nil {
		tv.onExit()
	}
}

// Draw renders the emulator grid into the pane's inner rectangle.
func (tv *TermView) Draw(screen tcell.Screen) {
	tv.Box.DrawForSubclass(screen, tv)
	x, y, w, h := tv.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	tv.resize(w, h)

	vt := tv.term
	vt.Lock()
	defer vt.Unlock()
	cols, rows := vt.Size()
	for r := 0; r < rows && r < h; r++ {
		// Hide the bracketing lines injected to capture F5 output (the echoed
		// wrapper command and the begin/end markers) so they don't clutter the
		// real terminal. The emulator still processed them, so the shell's own
		// layout is unaffected — we just don't paint these rows.
		if rowHasSentinel(vt, r, cols, w) {
			for c := 0; c < w; c++ {
				screen.SetContent(x+c, y+r, ' ', nil, tcell.StyleDefault)
			}
			continue
		}
		for c := 0; c < cols && c < w; c++ {
			g := vt.Cell(c, r)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			screen.SetContent(x+c, y+r, ch, nil, glyphStyle(g))
		}
	}
	if vt.CursorVisible() && tv.HasFocus() {
		cur := vt.Cursor()
		if cur.X >= 0 && cur.X < w && cur.Y >= 0 && cur.Y < h {
			screen.ShowCursor(x+cur.X, y+cur.Y)
		}
	}
}

// resize keeps the emulator and the PTY in sync with the pane size.
func (tv *TermView) resize(w, h int) {
	tv.mu.Lock()
	changed := w != tv.cols || h != tv.rows
	closed := tv.closed
	tv.cols, tv.rows = w, h
	tv.mu.Unlock()
	if !changed || closed {
		return
	}
	tv.term.Resize(w, h)
	_ = tv.pty.Resize(w, h)
}

// InputHandler forwards keystrokes to the shell while the pane is focused.
func (tv *TermView) InputHandler() func(*tcell.EventKey, func(p tview.Primitive)) {
	return tv.WrapInputHandler(func(ev *tcell.EventKey, _ func(p tview.Primitive)) {
		tv.mu.Lock()
		closed := tv.closed
		tv.mu.Unlock()
		if closed {
			return
		}
		if b := keyToBytes(ev); len(b) > 0 {
			_, _ = tv.pty.Write(b)
		}
	})
}

// SendString writes raw text to the shell as if typed.
func (tv *TermView) SendString(s string) {
	tv.mu.Lock()
	closed := tv.closed
	tv.mu.Unlock()
	if closed {
		return
	}
	_, _ = tv.pty.Write([]byte(s))
}

// Close terminates the shell and releases the PTY. Safe to call more than once.
func (tv *TermView) Close() {
	tv.mu.Lock()
	tv.closed = true
	cmd := tv.cmd
	tv.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if tv.pty != nil {
		_ = tv.pty.Close()
	}
}

// glyphStyle converts a vt10x glyph's colours and attributes to a tcell style.
func glyphStyle(g vt10x.Glyph) tcell.Style {
	fg := vtColor(g.FG, tcell.ColorDefault)
	bg := vtColor(g.BG, tcell.ColorDefault)
	if g.Mode&attrReverse != 0 {
		fg, bg = bg, fg
	}
	st := tcell.StyleDefault.Foreground(fg).Background(bg)
	if g.Mode&attrBold != 0 {
		st = st.Bold(true)
	}
	if g.Mode&attrUnderline != 0 {
		st = st.Underline(true)
	}
	if g.Mode&attrItalic != 0 {
		st = st.Italic(true)
	}
	if g.Mode&attrBlink != 0 {
		st = st.Blink(true)
	}
	return st
}

// vtColor maps a vt10x colour to a tcell colour (ANSI/256 palette, 24-bit RGB,
// or the terminal default).
func vtColor(c vt10x.Color, def tcell.Color) tcell.Color {
	switch c {
	case vt10x.DefaultFG, vt10x.DefaultBG, vt10x.DefaultCursor:
		return def
	}
	if c < 256 {
		return tcell.PaletteColor(int(c))
	}
	if c < (1 << 24) {
		return tcell.NewHexColor(int32(c))
	}
	return def
}

// keyToBytes encodes a tcell key event as the byte sequence a terminal expects.
func keyToBytes(ev *tcell.EventKey) []byte {
	switch ev.Key() {
	case tcell.KeyRune:
		r := ev.Rune()
		if ev.Modifiers()&tcell.ModAlt != 0 {
			return append([]byte{0x1b}, []byte(string(r))...)
		}
		return []byte(string(r))
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return []byte{0x7f}
	case tcell.KeyEsc:
		return []byte{0x1b}
	case tcell.KeyUp:
		return []byte("\x1b[A")
	case tcell.KeyDown:
		return []byte("\x1b[B")
	case tcell.KeyRight:
		return []byte("\x1b[C")
	case tcell.KeyLeft:
		return []byte("\x1b[D")
	case tcell.KeyHome:
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	default:
		// Ctrl-letter and other control keys: the tcell key code equals the
		// control byte (e.g. KeyCtrlC == 3).
		if k := ev.Key(); k > 0 && k < 0x20 {
			return []byte{byte(k)}
		}
	}
	return nil
}
