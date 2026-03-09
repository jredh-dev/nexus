// Command wstr manages git workstream worktrees.
// Three subcommands: start, commit, end.
//
// Usage:
//
//	wstr start <task> --repo <owner/repo> [--base <branch>]
//	wstr commit [--message <msg>]
//	wstr end [--push]
//	wstr --help
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jredh-dev/nexus/cmd/wstr/internal/workstream"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "-?" || args[0] == "help" {
		printUsage()
		return 0
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "start":
		return cmdStart(rest)
	case "commit":
		return cmdCommit(rest)
	case "end":
		return cmdEnd(rest)
	default:
		fmt.Fprintf(os.Stderr, "wstr: unknown subcommand %q\n\nRun 'wstr --help' for usage.\n", sub)
		return 1
	}
}

// cmdStart creates a new workstream worktree + branch.
func cmdStart(args []string) int {
	fs := flag.NewFlagSet("wstr start", flag.ContinueOnError)
	repo := fs.String("repo", "", "Repository in owner/repo format (required)")
	base := fs.String("base", "main", "Base branch to branch from")

	// Support -h/-?/help as first arg before flag parsing.
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Println(`wstr start — create a new workstream worktree

Usage:
  wstr start <task description> --repo <owner/repo> [--base <branch>]

Examples:
  wstr start "add dark mode to settings" --repo jredh-dev/nexus
  wstr start "fix login bug" --repo jredh-dev/nexus --base develop`)
		return 0
	}

	// First positional arg is the task description; rest are flags.
	var task string
	var flagArgs []string
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = args[i:]
			break
		}
		if task == "" {
			task = a
		} else {
			task += " " + a
		}
	}
	if task == "" && len(args) > 0 {
		// All args are flags; task comes from leftover after parsing.
		flagArgs = args
	}

	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintf(os.Stderr, "wstr start: %v\n", err)
		return 1
	}

	// If task still empty, try positional args after flag parsing.
	if task == "" {
		task = strings.Join(fs.Args(), " ")
	}

	if task == "" {
		fmt.Fprintln(os.Stderr, "wstr start: task description is required")
		return 1
	}
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "wstr start: --repo is required")
		return 1
	}

	ws, err := workstream.Create(task, *repo, *base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wstr start: %v\n", err)
		return 1
	}

	fmt.Printf("✓ Workstream created: %s\n", ws.ID)
	fmt.Printf("  Task:    %s\n", ws.Task)
	fmt.Printf("  Branch:  %s\n", ws.Branch)
	fmt.Printf("  Worktree: %s\n", ws.WorktreePath)
	fmt.Printf("\nNext:\n  cd %s\n  # ... make changes ...\n  wstr commit -m \"feat: ...\"\n  wstr end --push\n", ws.WorktreePath)
	return 0
}

// cmdCommit stages all and commits in the current directory.
func cmdCommit(args []string) int {
	fs := flag.NewFlagSet("wstr commit", flag.ContinueOnError)
	msg := fs.String("message", "", "Commit message (optional — defaults to wip: <task>)")
	fs.StringVar(msg, "m", "", "Shorthand for --message")

	if len(args) > 0 && isHelp(args[0]) {
		fmt.Println(`wstr commit — stage all changes and commit

Usage:
  wstr commit [--message <msg>]
  wstr commit [-m <msg>]

Must be run from inside a workstream worktree (where .wstr.json exists).`)
		return 0
	}

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "wstr commit: %v\n", err)
		return 1
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wstr commit: %v\n", err)
		return 1
	}

	if err := workstream.Commit(dir, *msg); err != nil {
		fmt.Fprintf(os.Stderr, "wstr commit: %v\n", err)
		return 1
	}
	fmt.Println("✓ Committed")
	return 0
}

// cmdEnd cleans up the worktree.
func cmdEnd(args []string) int {
	fs := flag.NewFlagSet("wstr end", flag.ContinueOnError)
	push := fs.Bool("push", false, "Push branch to origin before removing")

	if len(args) > 0 && isHelp(args[0]) {
		fmt.Println(`wstr end — clean up a workstream worktree

Usage:
  wstr end [--push]

Flags:
  --push  Push the branch to origin before removing the worktree

Must be run from inside a workstream worktree (where .wstr.json exists).`)
		return 0
	}

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "wstr end: %v\n", err)
		return 1
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wstr end: %v\n", err)
		return 1
	}

	ws, err := workstream.End(dir, *push)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wstr end: %v\n", err)
		return 1
	}

	fmt.Printf("✓ Workstream ended: %s\n", ws.ID)
	if *push {
		fmt.Printf("  Branch pushed: %s\n", ws.Branch)
	}
	return 0
}

func isHelp(s string) bool {
	return s == "--help" || s == "-h" || s == "-?" || s == "help"
}

func printUsage() {
	fmt.Println(`wstr — workstream lifecycle manager

Usage:
  wstr start <task> --repo <owner/repo> [--base <branch>]
  wstr commit [--message <msg>]
  wstr end [--push]

Subcommands:
  start   Create a new git worktree and branch for a task
  commit  Stage all changes and commit in the current worktree
  end     Remove the worktree (optionally pushing the branch first)

Run 'wstr <subcommand> --help' for subcommand details.`)
}
