// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/jredh-dev/nexus/cmd/tui/internal/app"
	"github.com/jredh-dev/nexus/cmd/tui/internal/obf"
)

// Build-time embedded values. Set by the Makefile via:
//
//	-ldflags "-X main.obfAddr=<hex> -X main.obfSecret=<hex> -X main.obfKey=<hex>"
//
// Encoding scheme:
//   - obfKey    = Encode(passphrase,   binaryName="tui")
//   - obfAddr   = Encode(serverAddr,   passphrase)
//   - obfSecret = Encode(sharedSecret, passphrase)
//
// The passphrase exists only in CI secrets at build time; it is never stored.
// obfKey is re-encoded with the binary name so a renamed binary decodes to
// garbage (mild tamper signal).
//
// Dev mode: leave all three empty; falls back to localhost:9090, no secret.
//
// Secrets service URL is always baked via SECRETS_URL ldflag in production.
var (
	obfAddr       string
	obfSecret     string
	obfKey        string
	obfSecretsURL string
)

func resolveConfig() (addr, secret, secretsURL string, devMode bool) {
	if obfAddr == "" || obfSecret == "" || obfKey == "" {
		return "localhost:9090", "", "http://localhost:8080", true
	}

	// The binary name is the seed for decoding obfKey.
	// Renaming the binary intentionally breaks decoding.
	binaryName := "tui"
	if len(os.Args) > 0 {
		binaryName = os.Args[0]
	}

	passphrase, err := obf.Decode(obfKey, binaryName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: key decode failed: %v\n", err)
		os.Exit(1)
	}

	resolvedAddr, err := obf.Decode(obfAddr, passphrase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: addr decode failed: %v\n", err)
		os.Exit(1)
	}

	resolvedSecret, err := obf.Decode(obfSecret, passphrase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: secret decode failed: %v\n", err)
		os.Exit(1)
	}

	resolvedSecretsURL := "http://localhost:8080"
	if obfSecretsURL != "" {
		u, err := obf.Decode(obfSecretsURL, passphrase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tui: secrets URL decode failed: %v\n", err)
			os.Exit(1)
		}
		resolvedSecretsURL = u
	}

	return resolvedAddr, resolvedSecret, resolvedSecretsURL, false
}

func main() {
	addr, secret, secretsURL, devMode := resolveConfig()

	if devMode {
		fmt.Fprintln(os.Stderr, "tui: dev mode (no build-time config baked in)")
	}

	hermitClient, err := app.NewHermitClient(addr, secret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: hermit dial: %v\n", err)
		os.Exit(1)
	}

	secretsClient := app.NewSecretsClient(secretsURL)

	m := app.New(addr, secret, hermitClient, secretsClient)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
