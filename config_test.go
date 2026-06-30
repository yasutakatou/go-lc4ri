package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestConfigPathSeparated checks the config lives under ~/.go-lc4ri (this CLI's
// own dir) and never under the VS Code extension's ~/.code-lc4ri.
func TestConfigPathSeparated(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	p := configPath()
	if want := filepath.Join(home, ".go-lc4ri", "config.json"); p != want {
		t.Fatalf("configPath() = %q, want %q", p, want)
	}
	if strings.Contains(p, ".code-lc4ri") {
		t.Fatalf("config path still points at the extension dir: %q", p)
	}
}

// TestLoadConfigAutoGenerates checks that a missing config is materialised under
// ~/.go-lc4ri on first load, holds the defaults, and is then read back.
func TestLoadConfigAutoGenerates(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}

	cfg := LoadConfig()
	if cfg.Timeout != DefaultConfig().Timeout {
		t.Errorf("Timeout = %d, want default %d", cfg.Timeout, DefaultConfig().Timeout)
	}

	p := filepath.Join(home, ".go-lc4ri", "config.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("config was not auto-generated at %s: %v", p, err)
	}
	if !strings.Contains(string(data), `"timeout"`) {
		t.Errorf("generated config lacks the timeout key:\n%s", data)
	}

	// A user edit must be honoured on reload (and the file not overwritten).
	edited := strings.Replace(string(data), `"timeout": 10000`, `"timeout": 42000`, 1)
	if edited == string(data) {
		t.Fatalf("could not find default timeout to edit in:\n%s", data)
	}
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig().Timeout; got != 42000 {
		t.Errorf("reloaded Timeout = %d, want 42000 (edit clobbered?)", got)
	}
}
