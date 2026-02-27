// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	pb "github.com/jredh-dev/nexus/cmd/fool/proto"
)

type appState int

const (
	stateLogin appState = iota
	stateConnecting
	stateDashboard
	stateBenchmark
	stateDB
	stateSecrets
	stateError
)

type dbHistoryEntry struct {
	ts     string
	cmd    string
	output string
	isErr  bool
}

// Model is the root bubbletea model for fool.
// Exported so tests can construct and drive it directly.
type Model struct {
	state  appState
	addr   string
	secret string

	width  int
	height int

	hermit  HermitClient
	secrets SecretsClient

	err error

	// Login
	username string

	// Dashboard
	serverInfo  *pb.ServerInfoResponse
	viewHistory []string

	// Benchmark
	grpcBench    *pb.BenchmarkResponse
	tcpPlain     *tcpBenchResultMsg
	tcpTLS       *tcpBenchResultMsg
	benchRunning bool

	// DB Console
	dbStats   *pb.DbStatsResponse
	dbInput   string
	dbHistory []dbHistoryEntry

	// Secrets panel
	secretsList  []Secret
	secretsStats SecretsStats
	secretsInput string // value being typed for submission
	secretsLog   []secretsLogEntry

	// Menu
	menuItems []string
	menuIdx   int
}

type secretsLogEntry struct {
	ts    string
	text  string
	isErr bool
}

// New creates a fresh Model. hermit and secrets clients may be nil for testing
// individual panels without a live server.
func New(addr, secret string, h HermitClient, s SecretsClient) Model {
	return Model{
		state:     stateLogin,
		addr:      addr,
		secret:    secret,
		hermit:    h,
		secrets:   s,
		username:  "",
		menuItems: []string{"Hermit DB", "Benchmark", "Secrets", "Quit"},
		menuIdx:   0,
	}
}
