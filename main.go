// Command code-lc4ri is the standalone CLI for code-lc4ri runbooks.
//
// It re-implements the LC4RI runbook semantics of the VS Code extension in Go
// and adds an interactive terminal UI: a split screen with the Markdown
// document on top and a live terminal on the bottom, plus popup panels for the
// variable inspector and keyboard help.
//
//	code-lc4ri <file.md>                 # launch the interactive TUI
//	code-lc4ri tui <file.md> [--profile NAME]
//	code-lc4ri run <file.md> [--dry-run] [--profile NAME] [--report FILE]
package main

import (
	"fmt"
	"os"
)

const version = "2.0.0"

func usage() {
	fmt.Print(`code-lc4ri ` + version + ` — Markdown + LC4RI runner

Usage:
  code-lc4ri <file.md>                              Launch the interactive TUI
  code-lc4ri tui <file.md> [--profile NAME]         Launch the interactive TUI
  code-lc4ri run <file.md> [options]                Run headlessly (CI / scripts)
  code-lc4ri --help | --version

Run options:
  --dry-run            Show resolved commands without executing them
  --profile NAME       Wrap commands with the named profile from config.json
  --report FILE        Write an execution report (.html or .md)

The TUI is a split screen: an always-editable Markdown editor on top and a
live OS terminal (your shell) on the bottom.

TUI shortcuts (also shown in-app with F1):
  F2           switch focus editor ⇄ terminal
  Ctrl-S       save the document (any time)
  F5           run the current editor line/selection in the terminal;
               output streams back into the doc as an output block
  F6 / F7      shrink / grow the terminal pane
  F1           help          F10   quit (or type 'exit' in the shell)
`)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	switch args[0] {
	case "--help", "-h":
		usage()
		return
	case "--version", "-v":
		fmt.Println("code-lc4ri", version)
		return
	case "run":
		os.Exit(runCommand(args[1:]))
	case "tui":
		os.Exit(tuiCommand(args[1:]))
	default:
		// Bare file argument → TUI.
		os.Exit(tuiCommand(args))
	}
}

func runCommand(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}
	file := ""
	dryRun := false
	profile := ""
	report := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--profile":
			if i+1 < len(args) {
				i++
				profile = args[i]
			}
		case "--report":
			if i+1 < len(args) {
				i++
				report = args[i]
			}
		default:
			if file == "" {
				file = args[i]
			}
		}
	}
	if file == "" {
		usage()
		return 1
	}
	return runHeadless(file, dryRun, profile, report)
}

func tuiCommand(args []string) int {
	file := ""
	profile := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--profile":
			if i+1 < len(args) {
				i++
				profile = args[i]
			}
		case "--help", "-h":
			usage()
			return 0
		default:
			if file == "" {
				file = args[i]
			}
		}
	}
	if file == "" {
		usage()
		return 1
	}
	return runTUI(file, profile)
}
