// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

// encode is a build helper that XOR-obfuscates a plaintext value.
// Usage:
//
//	go run ./cmd/client-tui/internal/obf/encode <plaintext> <passphrase>
package main

import (
	"fmt"
	"os"

	"github.com/jredh-dev/nexus/cmd/client-tui/internal/obf"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: encode <plaintext> <passphrase>")
		os.Exit(1)
	}
	fmt.Println(obf.Encode(os.Args[1], os.Args[2]))
}
