package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Config mirrors LC4RIConfig from extension.ts. The CLI reads the same legacy
// ~/.code-lc4ri/config.json file the VS Code extension falls back to, so a
// single configuration drives both front-ends.
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

// legacyConfigPath returns the path of ~/.code-lc4ri/config.json.
func legacyConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".code-lc4ri", "config.json")
}

// LoadConfig merges the legacy config file (if present) onto the defaults.
// A malformed file is ignored rather than fatal, matching the extension.
func LoadConfig() Config {
	cfg := DefaultConfig()
	p := legacyConfigPath()
	if p == "" {
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
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
