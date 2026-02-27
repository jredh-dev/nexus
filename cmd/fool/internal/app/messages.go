// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	pb "github.com/jredh-dev/nexus/cmd/fool/proto"
)

// --- Tea messages ---

type loginResultMsg struct {
	err error
}

type serverInfoMsg struct {
	resp *pb.ServerInfoResponse
	err  error
}

type benchmarkResultMsg struct {
	resp *pb.BenchmarkResponse
	err  error
}

type tcpBenchResultMsg struct {
	encrypted bool
	rttNs     int64
	err       error
}

type dbStatsMsg struct {
	resp *pb.DbStatsResponse
	err  error
}

type dbCmdResultMsg struct {
	cmd    string
	output string
	err    error
}

type secretsListMsg struct {
	secrets []Secret
	err     error
}

type secretsStatsMsg struct {
	stats SecretsStats
	err   error
}

type secretSubmitMsg struct {
	result *SubmitResult
	err    error
}
