package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Bindable action names for the "keybindings" section of config.json. Ctrl-R
// and Ctrl-Enter remain fixed convenience aliases for actRun regardless of
// how actRun itself is bound (see onKey) — they are not configurable, so they
// are intentionally left out of this set.
const (
	actQuit         = "quit"
	actHelp         = "help"
	actPreview      = "preview"
	actSave         = "save"
	actRun          = "run"
	actRunAll       = "runAll"
	actCancel       = "cancel"
	actFocus        = "focus"
	actResizeShrink = "resizeShrink"
	actResizeGrow   = "resizeGrow"
	actNextStep     = "nextStep"
	actPrevStep     = "prevStep"
)

// actionOrder lists the bindable actions in the order they are documented.
var actionOrder = []string{actQuit, actHelp, actPreview, actSave, actRun, actRunAll, actCancel, actFocus, actResizeShrink, actResizeGrow, actNextStep, actPrevStep}

// defaultKeybindings mirrors the shortcuts this CLI shipped with before
// keybindings became configurable.
func defaultKeybindings() map[string]string {
	return map[string]string{
		actQuit:         "F10",
		actHelp:         "F1",
		actPreview:      "F3",
		actSave:         "Ctrl-S",
		actRun:          "F5",
		actRunAll:       "F9",
		actCancel:       "F8",
		actFocus:        "F2",
		actResizeShrink: "F6",
		actResizeGrow:   "F7",
		// Step navigation is only consumed while the editor has focus, so the
		// shell keeps Ctrl-N / Ctrl-P (history) for itself — see onKey.
		actNextStep: "Ctrl-N",
		actPrevStep: "Ctrl-P",
	}
}

// keySpec is a parsed key binding: either a named/function key (key set) or a
// printable rune (key == tcell.KeyRune).
type keySpec struct {
	key   tcell.Key
	rune  rune
	label string // the original config string, for display
}

func (k keySpec) matches(ev *tcell.EventKey) bool {
	if k.key == tcell.KeyRune {
		return ev.Key() == tcell.KeyRune && ev.Rune() == k.rune
	}
	return ev.Key() == k.key
}

// keySignature identifies a keySpec for duplicate-assignment detection.
func keySignature(k keySpec) string {
	if k.key == tcell.KeyRune {
		return "rune:" + string(k.rune)
	}
	return fmt.Sprintf("key:%d", k.key)
}

var namedKeys = map[string]tcell.Key{
	"f1": tcell.KeyF1, "f2": tcell.KeyF2, "f3": tcell.KeyF3, "f4": tcell.KeyF4,
	"f5": tcell.KeyF5, "f6": tcell.KeyF6, "f7": tcell.KeyF7, "f8": tcell.KeyF8,
	"f9": tcell.KeyF9, "f10": tcell.KeyF10, "f11": tcell.KeyF11, "f12": tcell.KeyF12,
	"esc": tcell.KeyEsc, "escape": tcell.KeyEsc,
	"enter": tcell.KeyEnter, "return": tcell.KeyEnter,
	"tab":  tcell.KeyTab,
	"up":   tcell.KeyUp,
	"down": tcell.KeyDown, "left": tcell.KeyLeft, "right": tcell.KeyRight,
	"home": tcell.KeyHome, "end": tcell.KeyEnd,
	"pgup": tcell.KeyPgUp, "pgdn": tcell.KeyPgDn,
	"backspace": tcell.KeyBackspace2,
	"delete":    tcell.KeyDelete, "del": tcell.KeyDelete,
}

var ctrlLetterKeys = map[byte]tcell.Key{
	'a': tcell.KeyCtrlA, 'b': tcell.KeyCtrlB, 'c': tcell.KeyCtrlC, 'd': tcell.KeyCtrlD,
	'e': tcell.KeyCtrlE, 'f': tcell.KeyCtrlF, 'g': tcell.KeyCtrlG, 'h': tcell.KeyCtrlH,
	'i': tcell.KeyCtrlI, 'j': tcell.KeyCtrlJ, 'k': tcell.KeyCtrlK, 'l': tcell.KeyCtrlL,
	'm': tcell.KeyCtrlM, 'n': tcell.KeyCtrlN, 'o': tcell.KeyCtrlO, 'p': tcell.KeyCtrlP,
	'q': tcell.KeyCtrlQ, 'r': tcell.KeyCtrlR, 's': tcell.KeyCtrlS, 't': tcell.KeyCtrlT,
	'u': tcell.KeyCtrlU, 'v': tcell.KeyCtrlV, 'w': tcell.KeyCtrlW, 'x': tcell.KeyCtrlX,
	'y': tcell.KeyCtrlY, 'z': tcell.KeyCtrlZ,
}

// parseKeySpec parses a config key string such as "F5", "Ctrl-S", "Esc" or a
// single printable character such as "q" into a keySpec.
func parseKeySpec(spec string) (keySpec, error) {
	raw := spec
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return keySpec{}, fmt.Errorf("empty key")
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "ctrl-") || strings.HasPrefix(strings.ToLower(trimmed), "ctrl+") {
		rest := trimmed[5:]
		if len(rest) == 1 {
			c := rest[0] | 0x20 // fold to lowercase ascii
			if k, ok := ctrlLetterKeys[c]; ok {
				return keySpec{key: k, label: raw}, nil
			}
		}
		return keySpec{}, fmt.Errorf("unrecognized key %q (Ctrl- combinations support letters A-Z only)", raw)
	}
	if k, ok := namedKeys[strings.ToLower(trimmed)]; ok {
		return keySpec{key: k, label: raw}, nil
	}
	if runes := []rune(trimmed); len(runes) == 1 {
		return keySpec{key: tcell.KeyRune, rune: runes[0], label: raw}, nil
	}
	return keySpec{}, fmt.Errorf("unrecognized key %q", raw)
}

// resolveKeybindings merges the config's "keybindings" overrides onto the
// defaults, parses every binding and rejects unknown action names or two
// actions bound to the same key — the caller must treat that as fatal and
// stop before the TUI starts.
func resolveKeybindings(overrides map[string]string) (map[string]keySpec, error) {
	merged := defaultKeybindings()
	for action, spec := range overrides {
		if _, known := merged[action]; !known {
			return nil, fmt.Errorf("keybindings: unknown action %q (valid actions: %s)", action, strings.Join(actionOrder, ", "))
		}
		merged[action] = spec
	}

	resolved := make(map[string]keySpec, len(merged))
	claimedBy := make(map[string]string, len(merged))
	for _, action := range actionOrder {
		spec := merged[action]
		ks, err := parseKeySpec(spec)
		if err != nil {
			return nil, fmt.Errorf("keybindings.%s: %v", action, err)
		}
		sig := keySignature(ks)
		if other, dup := claimedBy[sig]; dup {
			return nil, fmt.Errorf("keybindings: %q and %q are both bound to %q", other, action, spec)
		}
		claimedBy[sig] = action
		resolved[action] = ks
	}
	return resolved, nil
}
