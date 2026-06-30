package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Config mirrors LC4RIConfig from extension.ts. This CLI keeps its own
// configuration under ~/.go-lc4ri/config.json — deliberately separate from the
// VS Code extension's ~/.code-lc4ri so the two front-ends never share state.
type Config struct {
	Timeout           int               `json:"timeout"`
	Profiles          map[string]string `json:"profiles"`
	Template          map[string]string `json:"template"`
	ChangeWord        map[string]string `json:"changeWord"`
	OutputFormat      string            `json:"outputFormat"`
	DangerousPatterns []string          `json:"dangerousPatterns"`
	AllowList         []string          `json:"allowList"`
	DenyList          []string          `json:"denyList"`
	ConfirmDangerous  bool              `json:"confirmDangerous"`
	Shell             *string           `json:"shell"`
}

// DefaultConfig returns the built-in defaults (matches DEFAULT_CONFIG).
func DefaultConfig() Config {
	return Config{
		Timeout:           10000,
		Profiles:          map[string]string{},
		Template:          map[string]string{},
		ChangeWord:        map[string]string{},
		OutputFormat:      "codeblock",
		DangerousPatterns: append([]string(nil), DefaultDangerousPatterns...),
		AllowList:         []string{},
		DenyList:          []string{},
		ConfirmDangerous:  true,
		Shell:             nil,
	}
}

// configDir returns ~/.go-lc4ri, this CLI's own configuration directory.
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".go-lc4ri")
}

// configPath returns the path of ~/.go-lc4ri/config.json.
func configPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// writeDefaultConfig materialises ~/.go-lc4ri/config.json with the built-in
// defaults so a fresh install ships a complete, self-documenting file to edit.
// Best effort: any error leaves the in-memory defaults in force.
func writeDefaultConfig(path string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep regex metacharacters (& < >) readable
	enc.SetIndent("", "  ")
	if err := enc.Encode(DefaultConfig()); err != nil {
		return
	}
	_ = os.WriteFile(path, buf.Bytes(), 0o644)
}

// LoadConfig merges ~/.go-lc4ri/config.json (if present) onto the defaults,
// auto-generating it on first run when it is missing. A malformed file is
// ignored rather than fatal.
func LoadConfig() Config {
	cfg := DefaultConfig()
	p := configPath()
	if p == "" {
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			writeDefaultConfig(p) // first run: leave a template under ~/.go-lc4ri/
		}
		return cfg
	}
	var raw struct {
		Timeout           *int              `json:"timeout"`
		Profiles          map[string]string `json:"profiles"`
		Template          map[string]string `json:"template"`
		ChangeWord        map[string]string `json:"changeWord"`
		OutputFormat      *string           `json:"outputFormat"`
		DangerousPatterns []string          `json:"dangerousPatterns"`
		AllowList         []string          `json:"allowList"`
		DenyList          []string          `json:"denyList"`
		ConfirmDangerous  *bool             `json:"confirmDangerous"`
		Shell             *string           `json:"shell"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return cfg
	}
	if raw.Timeout != nil {
		cfg.Timeout = *raw.Timeout
	}
	if raw.Profiles != nil {
		cfg.Profiles = raw.Profiles
	}
	if raw.Template != nil {
		cfg.Template = raw.Template
	}
	if raw.ChangeWord != nil {
		cfg.ChangeWord = raw.ChangeWord
	}
	if raw.OutputFormat != nil {
		cfg.OutputFormat = *raw.OutputFormat
	}
	if raw.DangerousPatterns != nil {
		cfg.DangerousPatterns = raw.DangerousPatterns
	}
	if raw.AllowList != nil {
		cfg.AllowList = raw.AllowList
	}
	if raw.DenyList != nil {
		cfg.DenyList = raw.DenyList
	}
	if raw.ConfirmDangerous != nil {
		cfg.ConfirmDangerous = *raw.ConfirmDangerous
	}
	if raw.Shell != nil {
		cfg.Shell = raw.Shell
	}
	return cfg
}

// goPlatform returns the Node-style platform key used by the template map.
func goPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS // "linux", etc.
	}
}
