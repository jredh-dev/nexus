// ref — CLI for the ref async prompt execution queue.
//
// Usage:
//
//	ref add <title> <prompt>                   add a prompt (mode=batch)
//	ref add <title> <prompt> --mode <mode>     add with explicit mode
//	ref list                                   list all prompts
//	ref get <id>                               show one prompt
//	ref update <id> [--title T] [--prompt P] [--mode M]  partial update
//	ref delete <id>                            delete a prompt
//	ref reflect                                trigger immediate drain (POST /reflect)
//
// Environment variables:
//
//	REF_URL   base URL of the ref service (default: http://localhost:8086)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || isHelp(args[0]) {
		printUsage()
		os.Exit(0)
	}

	baseURL := strings.TrimRight(env("REF_URL", "http://localhost:8086"), "/")

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "add":
		cmdAdd(baseURL, rest)
	case "list":
		cmdList(baseURL)
	case "get":
		cmdGet(baseURL, rest)
	case "update":
		cmdUpdate(baseURL, rest)
	case "delete":
		cmdDelete(baseURL, rest)
	case "reflect":
		cmdReflect(baseURL)
	default:
		fatalf("unknown command: %s\nRun ref --help for usage.", cmd)
	}
}

// -----------------------------------------------------------------------
// Commands
// -----------------------------------------------------------------------

func cmdAdd(baseURL string, args []string) {
	// ref add <title> <prompt> [--mode <mode>]
	if len(args) < 2 || isHelp(args[0]) {
		fatalf("usage: ref add <title> <prompt> [--mode batch|review|inactive]")
	}
	title := args[0]
	prompt := args[1]
	mode := "batch"

	for i := 2; i < len(args); i++ {
		if args[i] == "--mode" && i+1 < len(args) {
			mode = args[i+1]
			i++
		}
	}

	body, _ := json.Marshal(map[string]string{
		"title":  title,
		"prompt": prompt,
		"mode":   mode,
	})

	resp, err := do(http.MethodPost, baseURL+"/prompts", body)
	if err != nil {
		fatalf("add: %v", err)
	}
	var p promptRecord
	mustDecode(resp, &p)
	fmt.Printf("Created prompt id=%d title=%q mode=%s\n", p.ID, p.Title, p.Mode)
}

func cmdList(baseURL string) {
	resp, err := do(http.MethodGet, baseURL+"/prompts", nil)
	if err != nil {
		fatalf("list: %v", err)
	}
	var prompts []promptRecord
	mustDecode(resp, &prompts)

	if len(prompts) == 0 {
		fmt.Println("No prompts.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tMODE\tRUNS\tNO_CHANGE\tLAST_RUN")
	fmt.Fprintln(w, "--\t-----\t----\t----\t---------\t--------")
	for _, p := range prompts {
		lastRun := "-"
		if p.LastRunAt != nil {
			lastRun = p.LastRunAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%s\n",
			p.ID, p.Title, p.Mode, p.RunCount, p.NoChangeCount, lastRun)
	}
	w.Flush()
}

func cmdGet(baseURL string, args []string) {
	if len(args) < 1 || isHelp(args[0]) {
		fatalf("usage: ref get <id>")
	}
	id := mustParseID(args[0])

	resp, err := do(http.MethodGet, fmt.Sprintf("%s/prompts/%d", baseURL, id), nil)
	if err != nil {
		fatalf("get: %v", err)
	}
	var p promptRecord
	mustDecode(resp, &p)
	printPrompt(p)
}

func cmdUpdate(baseURL string, args []string) {
	// ref update <id> [--title T] [--prompt P] [--mode M]
	if len(args) < 1 || isHelp(args[0]) {
		fatalf("usage: ref update <id> [--title T] [--prompt P] [--mode batch|review|inactive]")
	}
	id := mustParseID(args[0])

	patch := map[string]string{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--title":
			if i+1 < len(args) {
				patch["title"] = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				patch["prompt"] = args[i+1]
				i++
			}
		case "--mode":
			if i+1 < len(args) {
				patch["mode"] = args[i+1]
				i++
			}
		}
	}
	if len(patch) == 0 {
		fatalf("update: no fields specified (--title, --prompt, --mode)")
	}

	body, _ := json.Marshal(patch)
	resp, err := do(http.MethodPut, fmt.Sprintf("%s/prompts/%d", baseURL, id), body)
	if err != nil {
		fatalf("update: %v", err)
	}
	var p promptRecord
	mustDecode(resp, &p)
	printPrompt(p)
}

func cmdDelete(baseURL string, args []string) {
	if len(args) < 1 || isHelp(args[0]) {
		fatalf("usage: ref delete <id>")
	}
	id := mustParseID(args[0])

	_, err := do(http.MethodDelete, fmt.Sprintf("%s/prompts/%d", baseURL, id), nil)
	if err != nil {
		fatalf("delete: %v", err)
	}
	fmt.Printf("Deleted prompt %d\n", id)
}

func cmdReflect(baseURL string) {
	resp, err := do(http.MethodPost, baseURL+"/reflect", nil)
	if err != nil {
		fatalf("reflect: %v", err)
	}
	var result struct {
		Processed int      `json:"processed"`
		Succeeded int      `json:"succeeded"`
		Skipped   int      `json:"skipped"`
		Errors    []string `json:"errors"`
	}
	mustDecode(resp, &result)

	fmt.Printf("Reflect complete: processed=%d succeeded=%d skipped=%d errors=%d\n",
		result.Processed, result.Succeeded, result.Skipped, len(result.Errors))
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  error: %s\n", e)
	}
}

// -----------------------------------------------------------------------
// HTTP helpers
// -----------------------------------------------------------------------

// do makes an HTTP request and returns the response body bytes on 2xx,
// or an error on non-2xx (with the response body in the error message).
func do(method, url string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 204 No Content — success, no body.
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from JSON.
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

func mustDecode(data []byte, v any) {
	if err := json.Unmarshal(data, v); err != nil {
		fatalf("decode response: %v\nraw: %s", err, data)
	}
}

// -----------------------------------------------------------------------
// Types
// -----------------------------------------------------------------------

type promptRecord struct {
	ID            int        `json:"id"`
	Title         string     `json:"title"`
	Prompt        string     `json:"prompt"`
	Mode          string     `json:"mode"`
	Response      *string    `json:"response"`
	RunCount      int        `json:"run_count"`
	NoChangeCount int        `json:"no_change_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastRunAt     *time.Time `json:"last_run_at"`
}

// -----------------------------------------------------------------------
// Utilities
// -----------------------------------------------------------------------

func printPrompt(p promptRecord) {
	fmt.Printf("ID:         %d\n", p.ID)
	fmt.Printf("Title:      %s\n", p.Title)
	fmt.Printf("Mode:       %s\n", p.Mode)
	fmt.Printf("Run count:  %d\n", p.RunCount)
	fmt.Printf("No-change:  %d\n", p.NoChangeCount)
	fmt.Printf("Created:    %s\n", p.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:    %s\n", p.UpdatedAt.Format(time.RFC3339))
	if p.LastRunAt != nil {
		fmt.Printf("Last run:   %s\n", p.LastRunAt.Format(time.RFC3339))
	}
	fmt.Printf("Prompt:\n%s\n", indent(p.Prompt))
	if p.Response != nil {
		fmt.Printf("Response:\n%s\n", indent(*p.Response))
	}
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

func mustParseID(s string) int {
	id, err := strconv.Atoi(s)
	if err != nil || id < 1 {
		fatalf("id must be a positive integer, got: %q", s)
	}
	return id
}

func isHelp(s string) bool {
	return s == "--help" || s == "-h" || s == "-?" || s == "help"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Print(`ref — async prompt execution queue CLI

Commands:
  add <title> <prompt> [--mode batch|review|inactive]
      Add a prompt to the queue (default mode: batch)

  list
      List all prompts with status

  get <id>
      Show a single prompt and its last response

  update <id> [--title T] [--prompt P] [--mode batch|review|inactive]
      Partially update a prompt

  delete <id>
      Remove a prompt

  reflect
      Trigger an immediate drain: run all non-inactive prompts through OpenCode

Environment:
  REF_URL   ref service base URL (default: http://localhost:8086)

Modes:
  batch     Run on every cron tick
  review    Run on every cron tick (reserved for refinement workflows)
  inactive  Skip — set automatically after 3 consecutive no-change runs
`)
}
