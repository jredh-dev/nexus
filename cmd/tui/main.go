// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package main

import (
	"flag"
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
// Dev mode: leave all three empty; falls back to defaults (overridable via
// env vars or CLI flags).
//
// Resolution priority: CLI flag > env var > obf build-time > hardcoded default.
var (
	obfAddr       string
	obfSecret     string
	obfKey        string
	obfSecretsURL string
)

// config holds the resolved TUI configuration after merging all sources.
type config struct {
	HermitAddr string // gRPC address for hermit server
	Secret     string // x-hermit-secret value
	SecretsURL string // HTTP base URL for secrets service
	Insecure   bool   // true = plaintext gRPC (no TLS)
	DevMode    bool   // true = no build-time config baked in
}

// resolveConfig merges build-time, env var, and CLI flag sources.
// Priority: CLI flag > env var > obf build-time > hardcoded default.
func resolveConfig() config {
	// --- CLI flags ---
	flagAddr := flag.String("hermit-addr", "", "hermit gRPC address (host:port)")
	flagSecret := flag.String("hermit-secret", "", "x-hermit-secret shared secret")
	flagSecretsURL := flag.String("secrets-url", "", "secrets HTTP base URL")
	flagInsecure := flag.Bool("insecure", false, "use plaintext gRPC (no TLS)")
	flag.Parse()

	// --- Start with hardcoded defaults ---
	cfg := config{
		HermitAddr: "localhost:9090",
		Secret:     "",
		SecretsURL: "http://localhost:8081",
		Insecure:   true, // dev default: local Docker runs plaintext h2c
		DevMode:    true,
	}

	// --- Layer: obf build-time values (if baked in) ---
	if obfAddr != "" && obfSecret != "" && obfKey != "" {
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

		cfg.HermitAddr = resolvedAddr
		cfg.Secret = resolvedSecret
		cfg.Insecure = false // production builds use TLS
		cfg.DevMode = false

		if obfSecretsURL != "" {
			u, err := obf.Decode(obfSecretsURL, passphrase)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tui: secrets URL decode failed: %v\n", err)
				os.Exit(1)
			}
			cfg.SecretsURL = u
		}
	}

	// --- Layer: env vars override build-time ---
	if v := os.Getenv("HERMIT_ADDR"); v != "" {
		cfg.HermitAddr = v
	}
	if v := os.Getenv("HERMIT_SECRET"); v != "" {
		cfg.Secret = v
	}
	if v := os.Getenv("SECRETS_URL"); v != "" {
		cfg.SecretsURL = v
	}
	if v := os.Getenv("HERMIT_INSECURE"); v == "1" || v == "true" {
		cfg.Insecure = true
	} else if v == "0" || v == "false" {
		cfg.Insecure = false
	}

	// --- Layer: CLI flags override everything ---
	if *flagAddr != "" {
		cfg.HermitAddr = *flagAddr
	}
	if *flagSecret != "" {
		cfg.Secret = *flagSecret
	}
	if *flagSecretsURL != "" {
		cfg.SecretsURL = *flagSecretsURL
	}
	// flag.Bool has no "was set" check, so we only override if the flag was
	// explicitly passed. We use flag.Visit to detect this.
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "insecure" {
			cfg.Insecure = *flagInsecure
		}
	})

	return cfg
}

func main() {
	cfg := resolveConfig()

	if cfg.DevMode {
		fmt.Fprintln(os.Stderr, "tui: dev mode (no build-time config baked in)")
	}

	hermitClient, err := app.NewHermitClient(cfg.HermitAddr, cfg.Secret, cfg.Insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: hermit dial: %v\n", err)
		os.Exit(1)
	}

	secretsClient := app.NewSecretsClient(cfg.SecretsURL)

	m := app.New(cfg.HermitAddr, cfg.Secret, hermitClient, secretsClient)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
