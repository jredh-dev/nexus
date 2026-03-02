// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Init satisfies tea.Model. Returns nil (no initial commands).
func (m Model) Init() tea.Cmd {
	return nil
}

// Update is the bubbletea update function.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case loginResultMsg:
		return m.handleLoginResult(msg)

	case serverInfoMsg:
		return m.handleServerInfo(msg)

	case benchmarkResultMsg:
		return m.handleBenchmarkResult(msg)

	case tcpBenchResultMsg:
		return m.handleTCPBenchResult(msg)

	case dbStatsMsg:
		return m.handleDbStats(msg)

	case dbCmdResultMsg:
		return m.handleDbCmdResult(msg)

	case secretsListMsg:
		return m.handleSecretsList(msg)

	case secretsStatsMsg:
		return m.handleSecretsStats(msg)

	case secretSubmitMsg:
		return m.handleSecretSubmit(msg)
	}

	return m, nil
}

// --- Key Handling ---

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()

	if k.Code == 'c' && k.Mod == tea.ModCtrl {
		if m.hermit != nil {
			m.hermit.Close()
		}
		return m, tea.Quit
	}

	switch m.state {
	case stateLogin:
		return m.handleLoginKey(k)
	case stateDashboard, stateBenchmark:
		return m.handleDashboardKey(k)
	case stateDB:
		return m.handleDBKey(k)
	case stateSecrets:
		return m.handleSecretsKey(k)
	case stateError:
		if k.Code == 'q' || k.Code == tea.KeyEscape {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m Model) handleLoginKey(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEnter:
		if m.username == "" {
			m.username = "operator"
		}
		m.state = stateConnecting
		return m, m.doLogin()
	case tea.KeyBackspace:
		if len(m.username) > 0 {
			m.username = m.username[:len(m.username)-1]
		}
	case tea.KeyEscape:
		return m, tea.Quit
	default:
		if k.Text != "" {
			m.username += k.Text
		}
	}
	return m, nil
}

func (m Model) handleDashboardKey(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyUp, 'k':
		if m.menuIdx > 0 {
			m.menuIdx--
		}
	case tea.KeyDown, 'j':
		if m.menuIdx < len(m.menuItems)-1 {
			m.menuIdx++
		}
	case tea.KeyEnter:
		return m.executeMenuItem()
	case 'q', tea.KeyEscape:
		if m.hermit != nil {
			m.hermit.Close()
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleDBKey(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEscape:
		m.state = stateDashboard
		m.dbInput = ""
		return m, nil
	case tea.KeyEnter:
		if m.dbInput != "" {
			cmd := strings.TrimSpace(m.dbInput)
			m.dbInput = ""
			return m, m.executeDBCommand(cmd)
		}
	case tea.KeyBackspace:
		if len(m.dbInput) > 0 {
			m.dbInput = m.dbInput[:len(m.dbInput)-1]
		}
	default:
		if k.Text != "" {
			m.dbInput += k.Text
		}
	}
	return m, nil
}

func (m Model) handleSecretsKey(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEscape:
		m.state = stateDashboard
		m.secretsInput = ""
		return m, nil
	case tea.KeyEnter:
		if m.secretsInput != "" {
			val := strings.TrimSpace(m.secretsInput)
			m.secretsInput = ""
			return m, m.doSubmitSecret(val)
		}
		// Empty enter refreshes
		return m, tea.Batch(m.doSecretsList(), m.doSecretsStats())
	case tea.KeyBackspace:
		if len(m.secretsInput) > 0 {
			m.secretsInput = m.secretsInput[:len(m.secretsInput)-1]
		}
	default:
		if k.Text != "" {
			m.secretsInput += k.Text
		}
	}
	return m, nil
}

func (m Model) executeMenuItem() (tea.Model, tea.Cmd) {
	switch m.menuItems[m.menuIdx] {
	case "Hermit DB":
		m.state = stateDB
		m.dbInput = ""
		return m, m.doDbStats()
	case "Benchmark":
		m.state = stateBenchmark
		m.benchRunning = true
		m.grpcBench = nil
		m.tcpPlain = nil
		m.tcpTLS = nil
		return m, tea.Batch(m.doBenchmark(), m.doTCPBench(false), m.doTCPBench(true))
	case "Secrets":
		m.state = stateSecrets
		m.secretsInput = ""
		return m, tea.Batch(m.doSecretsList(), m.doSecretsStats())
	case "Quit":
		if m.hermit != nil {
			m.hermit.Close()
		}
		return m, tea.Quit
	}
	return m, nil
}

// --- DB Command Dispatch ---
//
// Supported commands:
//
//	kv:set <key> <value>     — document store write
//	kv:get <key>             — document store read
//	kv:list                  — list all keys
//	sql:insert <key> <value> — relational store write (enqueued)
//	sql:query [key]          — relational store read (eventual)
//	stats                    — refresh DB stats
//	help                     — show command list

func (m Model) executeDBCommand(raw string) tea.Cmd {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil
	}

	verb := strings.ToLower(parts[0])

	switch verb {
	case "kv:set":
		if len(parts) < 3 {
			return m.dbResult(raw, "", fmt.Errorf("usage: kv:set <key> <value>"))
		}
		key := parts[1]
		val := strings.Join(parts[2:], " ")
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.KvSet(key, []byte(val))
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			if !resp.Ok {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("%s", resp.Error)}
			}
			return dbCmdResultMsg{cmd: raw, output: fmt.Sprintf("OK  key=%q", key)}
		}

	case "kv:get":
		if len(parts) < 2 {
			return m.dbResult(raw, "", fmt.Errorf("usage: kv:get <key>"))
		}
		key := parts[1]
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.KvGet(key)
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			if !resp.Found {
				return dbCmdResultMsg{cmd: raw, output: fmt.Sprintf("NOT FOUND  key=%q", key)}
			}
			return dbCmdResultMsg{cmd: raw, output: fmt.Sprintf("value=%q", string(resp.Value))}
		}

	case "kv:list":
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.KvList()
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			if len(resp.Keys) == 0 {
				return dbCmdResultMsg{cmd: raw, output: "(empty)"}
			}
			return dbCmdResultMsg{cmd: raw, output: strings.Join(resp.Keys, "  ")}
		}

	case "sql:insert":
		if len(parts) < 3 {
			return m.dbResult(raw, "", fmt.Errorf("usage: sql:insert <key> <value>"))
		}
		key := parts[1]
		val := strings.Join(parts[2:], " ")
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.SqlInsert(key, val)
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			if !resp.Queued {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("%s", resp.Error)}
			}
			return dbCmdResultMsg{cmd: raw, output: fmt.Sprintf("QUEUED  key=%q val=%q", key, val)}
		}

	case "sql:query":
		keyFilter := ""
		if len(parts) >= 2 {
			keyFilter = parts[1]
		}
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.SqlQuery(keyFilter, 20)
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			if len(resp.Rows) == 0 {
				return dbCmdResultMsg{cmd: raw, output: fmt.Sprintf("(no rows) committed=%d pending=%d",
					resp.TotalCommitted, resp.PendingWrites)}
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("committed=%d pending=%d\n", resp.TotalCommitted, resp.PendingWrites))
			for _, r := range resp.Rows {
				sb.WriteString(fmt.Sprintf("  [%s] key=%q val=%q\n", r.Id[:8], r.Key, r.Value))
			}
			return dbCmdResultMsg{cmd: raw, output: strings.TrimRight(sb.String(), "\n")}
		}

	case "stats":
		return func() tea.Msg {
			if m.hermit == nil {
				return dbCmdResultMsg{cmd: raw, err: fmt.Errorf("not connected")}
			}
			resp, err := m.hermit.DbStats()
			if err != nil {
				return dbCmdResultMsg{cmd: raw, err: err}
			}
			out := fmt.Sprintf("docs: keys=%d bytes=%d  rels: rows=%d pending=%d",
				resp.DocKeyCount, resp.DocCompressedBytes,
				resp.RelRowCount, resp.RelPendingWrites)
			return dbCmdResultMsg{cmd: raw, output: out}
		}

	case "help":
		help := "kv:set <k> <v>  kv:get <k>  kv:list  sql:insert <k> <v>  sql:query [k]  stats"
		return m.dbResult(raw, help, nil)

	default:
		return m.dbResult(raw, "", fmt.Errorf("unknown command %q — type 'help'", verb))
	}
}

func (m Model) dbResult(cmd, output string, err error) tea.Cmd {
	return func() tea.Msg {
		return dbCmdResultMsg{cmd: cmd, output: output, err: err}
	}
}

// --- Async Commands ---

func (m Model) doLogin() tea.Cmd {
	return func() tea.Msg {
		if m.hermit == nil {
			return loginResultMsg{err: fmt.Errorf("hermit client not configured")}
		}
		err := m.hermit.Login(m.username, "hardcoded-token")
		return loginResultMsg{err: err}
	}
}

func (m Model) doServerInfo() tea.Cmd {
	return func() tea.Msg {
		if m.hermit == nil {
			return serverInfoMsg{err: fmt.Errorf("not connected")}
		}
		resp, err := m.hermit.ServerInfo()
		return serverInfoMsg{resp: resp, err: err}
	}
}

func (m Model) doBenchmark() tea.Cmd {
	return func() tea.Msg {
		if m.hermit == nil {
			return benchmarkResultMsg{err: fmt.Errorf("not connected")}
		}
		resp, err := m.hermit.Benchmark(100, 0)
		return benchmarkResultMsg{resp: resp, err: err}
	}
}

func (m Model) doTCPBench(encrypted bool) tea.Cmd {
	return func() tea.Msg {
		port := 9091
		if encrypted {
			port = 9093
		}
		host := m.addr
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}
		addr := fmt.Sprintf("%s:%d", host, port)
		if m.hermit == nil {
			return tcpBenchResultMsg{encrypted: encrypted, err: fmt.Errorf("not connected")}
		}
		rtt, err := m.hermit.TCPPing(addr, encrypted)
		return tcpBenchResultMsg{encrypted: encrypted, rttNs: rtt, err: err}
	}
}

func (m Model) doDbStats() tea.Cmd {
	return func() tea.Msg {
		if m.hermit == nil {
			return dbStatsMsg{err: fmt.Errorf("not connected")}
		}
		resp, err := m.hermit.DbStats()
		return dbStatsMsg{resp: resp, err: err}
	}
}

func (m Model) doSecretsList() tea.Cmd {
	return func() tea.Msg {
		if m.secrets == nil {
			return secretsListMsg{err: fmt.Errorf("secrets client not configured")}
		}
		list, err := m.secrets.List()
		return secretsListMsg{secrets: list, err: err}
	}
}

func (m Model) doSecretsStats() tea.Cmd {
	return func() tea.Msg {
		if m.secrets == nil {
			return secretsStatsMsg{err: fmt.Errorf("secrets client not configured")}
		}
		stats, err := m.secrets.Stats()
		return secretsStatsMsg{stats: stats, err: err}
	}
}

func (m Model) doSubmitSecret(value string) tea.Cmd {
	username := m.username
	if username == "" {
		username = "fool"
	}
	return func() tea.Msg {
		if m.secrets == nil {
			return secretSubmitMsg{err: fmt.Errorf("secrets client not configured")}
		}
		result, err := m.secrets.Submit(value, username)
		return secretSubmitMsg{result: result, err: err}
	}
}

// --- Message Handlers ---

func (m Model) handleLoginResult(msg loginResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.state = stateError
		m.err = msg.err
		return m, nil
	}
	m.state = stateDashboard
	return m, m.doServerInfo()
}

func (m Model) handleServerInfo(msg serverInfoMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.serverInfo = msg.resp
	m.viewHistory = append(m.viewHistory, fmt.Sprintf("[%s] server info fetched", time.Now().Format("15:04:05")))
	return m, nil
}

func (m Model) handleBenchmarkResult(msg benchmarkResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
	} else {
		m.grpcBench = msg.resp
	}
	m.checkBenchDone()
	return m, nil
}

func (m Model) handleTCPBenchResult(msg tcpBenchResultMsg) (tea.Model, tea.Cmd) {
	cp := msg
	if msg.encrypted {
		m.tcpTLS = &cp
	} else {
		m.tcpPlain = &cp
	}
	m.checkBenchDone()
	return m, nil
}

func (m Model) handleDbStats(msg dbStatsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.dbStats = msg.resp
	return m, nil
}

func (m Model) handleDbCmdResult(msg dbCmdResultMsg) (tea.Model, tea.Cmd) {
	ts := time.Now().Format("15:04:05")
	entry := dbHistoryEntry{
		ts:    ts,
		cmd:   msg.cmd,
		isErr: msg.err != nil,
	}
	if msg.err != nil {
		entry.output = msg.err.Error()
	} else {
		entry.output = msg.output
	}
	m.dbHistory = append(m.dbHistory, entry)
	if len(m.dbHistory) > 50 {
		m.dbHistory = m.dbHistory[len(m.dbHistory)-50:]
	}
	verb := strings.ToLower(strings.Fields(msg.cmd)[0])
	if verb == "kv:set" || verb == "sql:insert" || verb == "stats" {
		return m, m.doDbStats()
	}
	return m, nil
}

func (m Model) handleSecretsList(msg secretsListMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		ts := time.Now().Format("15:04:05")
		m.secretsLog = append(m.secretsLog, secretsLogEntry{ts: ts, text: msg.err.Error(), isErr: true})
		return m, nil
	}
	m.secretsList = msg.secrets
	return m, nil
}

func (m Model) handleSecretsStats(msg secretsStatsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		ts := time.Now().Format("15:04:05")
		m.secretsLog = append(m.secretsLog, secretsLogEntry{ts: ts, text: msg.err.Error(), isErr: true})
		return m, nil
	}
	m.secretsStats = msg.stats
	return m, nil
}

func (m Model) handleSecretSubmit(msg secretSubmitMsg) (tea.Model, tea.Cmd) {
	ts := time.Now().Format("15:04:05")
	if msg.err != nil {
		m.secretsLog = append(m.secretsLog, secretsLogEntry{ts: ts, text: msg.err.Error(), isErr: true})
		return m, nil
	}
	r := msg.result
	var text string
	switch {
	case r.WasNew:
		text = fmt.Sprintf("SECRET  %q admitted (count=%d)", r.Secret.Value, r.Secret.Count)
	default:
		text = fmt.Sprintf("EXPOSED  %q admitted again (count=%d)", r.Secret.Value, r.Secret.Count)
	}
	if r.Message != "" {
		text += "  " + r.Message
	}
	m.secretsLog = append(m.secretsLog, secretsLogEntry{ts: ts, text: text})
	if len(m.secretsLog) > 50 {
		m.secretsLog = m.secretsLog[len(m.secretsLog)-50:]
	}
	return m, tea.Batch(m.doSecretsList(), m.doSecretsStats())
}

func (m *Model) checkBenchDone() {
	if m.grpcBench != nil && m.tcpPlain != nil && m.tcpTLS != nil {
		m.benchRunning = false
	}
}
