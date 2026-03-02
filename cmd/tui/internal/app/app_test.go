// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jredh-dev/nexus/cmd/tui/internal/app"
	pb "github.com/jredh-dev/nexus/cmd/tui/proto"
)

// --- Mock HermitClient ---

type mockHermit struct {
	loginErr   error
	serverInfo *pb.ServerInfoResponse
	serverErr  error
	benchResp  *pb.BenchmarkResponse
	benchErr   error
	dbStats    *pb.DbStatsResponse
	dbStatsErr error
	kvSetOK    bool
	kvGetFound bool
	kvGetValue []byte
	kvListKeys []string
}

func (m *mockHermit) Login(_, _ string) error { return m.loginErr }
func (m *mockHermit) ServerInfo() (*pb.ServerInfoResponse, error) {
	return m.serverInfo, m.serverErr
}
func (m *mockHermit) Benchmark(_, _ uint32) (*pb.BenchmarkResponse, error) {
	return m.benchResp, m.benchErr
}
func (m *mockHermit) KvSet(_ string, _ []byte) (*pb.KvSetResponse, error) {
	return &pb.KvSetResponse{Ok: m.kvSetOK}, nil
}
func (m *mockHermit) KvGet(_ string) (*pb.KvGetResponse, error) {
	return &pb.KvGetResponse{Found: m.kvGetFound, Value: m.kvGetValue}, nil
}
func (m *mockHermit) KvList() (*pb.KvListResponse, error) {
	return &pb.KvListResponse{Keys: m.kvListKeys}, nil
}
func (m *mockHermit) SqlInsert(_, _ string) (*pb.SqlInsertResponse, error) {
	return &pb.SqlInsertResponse{Queued: true}, nil
}
func (m *mockHermit) SqlQuery(_ string, _ uint32) (*pb.SqlQueryResponse, error) {
	return &pb.SqlQueryResponse{}, nil
}
func (m *mockHermit) DbStats() (*pb.DbStatsResponse, error)   { return m.dbStats, m.dbStatsErr }
func (m *mockHermit) TCPPing(_ string, _ bool) (int64, error) { return 1_000_000, nil }
func (m *mockHermit) Close()                                  {}

// --- Test helpers ---

// mustModel type-asserts the result of Update back to app.Model.
func mustModel(iface tea.Model) app.Model {
	return iface.(app.Model)
}

func sendKey(m app.Model, char rune) (app.Model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: char, Text: string(char)}
	next, cmd := m.Update(msg)
	return mustModel(next), cmd
}

func pressEnter(m app.Model) (app.Model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	next, cmd := m.Update(msg)
	return mustModel(next), cmd
}

func pressEsc(m app.Model) (app.Model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	next, cmd := m.Update(msg)
	return mustModel(next), cmd
}

func pressDown(m app.Model) (app.Model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: tea.KeyDown}
	next, cmd := m.Update(msg)
	return mustModel(next), cmd
}

func setSize(m app.Model, w, h int) (app.Model, tea.Cmd) {
	next, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return mustModel(next), cmd
}

// runCmd executes a tea.Cmd and dispatches the resulting message into the model.
func runCmd(m app.Model, cmd tea.Cmd) (app.Model, tea.Cmd) {
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	next, nextCmd := m.Update(msg)
	return mustModel(next), nextCmd
}

// doLogin drives the model through login and returns the model after serverInfo loads.
func doLogin(m app.Model) app.Model {
	m, _ = setSize(m, 120, 40)
	m, cmd := pressEnter(m) // submit → stateConnecting, fires doLogin cmd
	m, cmd = runCmd(m, cmd) // loginResultMsg → stateDashboard, fires doServerInfo
	m, _ = runCmd(m, cmd)   // serverInfoMsg
	return m
}

// hasContent asserts the view is non-empty.
func hasContent(t *testing.T, m app.Model, label string) {
	t.Helper()
	v := m.View()
	if v.Content == "" {
		t.Errorf("%s: expected non-empty view content", label)
	}
}

// --- Tests ---

func TestNew_InitialView(t *testing.T) {
	m := app.New("localhost:9090", "", nil, nil)
	m, _ = setSize(m, 80, 24)
	v := m.View()
	if !v.AltScreen {
		t.Error("expected AltScreen enabled")
	}
	if v.Content == "" {
		t.Error("expected non-empty view")
	}
}

func TestLogin_Success(t *testing.T) {
	h := &mockHermit{
		serverInfo: &pb.ServerInfoResponse{Version: "test-1.0", Region: "us-east"},
	}
	m := app.New("localhost:9090", "", h, nil)
	m = doLogin(m)
	hasContent(t, m, "after login")
}

func TestLogin_Failure(t *testing.T) {
	h := &mockHermit{loginErr: fmt.Errorf("auth failed")}
	m := app.New("localhost:9090", "", h, nil)
	m, _ = setSize(m, 80, 24)
	m, cmd := pressEnter(m)
	m, _ = runCmd(m, cmd)
	hasContent(t, m, "error state")
}

func TestDashboard_Navigation(t *testing.T) {
	h := &mockHermit{serverInfo: &pb.ServerInfoResponse{}}
	m := app.New("localhost:9090", "", h, nil)
	m = doLogin(m)

	m, _ = pressDown(m)
	m, _ = pressDown(m)

	hasContent(t, m, "after navigation")
}

func TestDBConsole_HelpCommand(t *testing.T) {
	h := &mockHermit{
		serverInfo: &pb.ServerInfoResponse{},
		dbStats:    &pb.DbStatsResponse{DocKeyCount: 3, RelRowCount: 1},
	}
	m := app.New("localhost:9090", "", h, nil)
	m = doLogin(m)

	// Enter DB console (menu index 0 = "Hermit DB")
	m, cmd := pressEnter(m)
	m, _ = runCmd(m, cmd) // dbStats

	// Type "help" and execute
	for _, c := range "help" {
		m, _ = sendKey(m, c)
	}
	m, cmd = pressEnter(m)
	m, _ = runCmd(m, cmd)

	hasContent(t, m, "after help command")
}

func TestDBConsole_KvSet(t *testing.T) {
	h := &mockHermit{
		serverInfo: &pb.ServerInfoResponse{},
		kvSetOK:    true,
		dbStats:    &pb.DbStatsResponse{DocKeyCount: 1},
	}
	m := app.New("localhost:9090", "", h, nil)
	m = doLogin(m)

	m, cmd := pressEnter(m)
	m, _ = runCmd(m, cmd) // dbStats

	for _, c := range "kv:set foo bar" {
		m, _ = sendKey(m, c)
	}
	m, cmd = pressEnter(m)
	m, cmd = runCmd(m, cmd) // kvSet result
	m, _ = runCmd(m, cmd)   // dbStats refresh

	hasContent(t, m, "after kv:set")
}

func TestDBConsole_EscReturns(t *testing.T) {
	h := &mockHermit{serverInfo: &pb.ServerInfoResponse{}, dbStats: &pb.DbStatsResponse{}}
	m := app.New("localhost:9090", "", h, nil)
	m = doLogin(m)

	m, cmd := pressEnter(m)
	m, _ = runCmd(m, cmd)

	m, _ = pressEsc(m)

	hasContent(t, m, "after esc from DB console")
}

// --- Secrets panel tests (httptest.Server) ---

type secretsState struct {
	secrets []app.Secret
	stats   app.SecretsStats
}

func newSecretsTestServer(t *testing.T) (*httptest.Server, *secretsState) {
	t.Helper()
	state := &secretsState{
		secrets: []app.Secret{
			{ID: "1", Value: "hello", SubmittedBy: "alice", Count: 1},
		},
		stats: app.SecretsStats{Total: 1, Secrets: 1},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/secrets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(state.secrets) //nolint:errcheck
		case http.MethodPost:
			var req struct {
				Value       string `json:"value"`
				SubmittedBy string `json:"submitted_by"`
			}
			json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
			s := app.Secret{
				ID:          fmt.Sprintf("%d", len(state.secrets)+1),
				Value:       req.Value,
				SubmittedBy: req.SubmittedBy,
				Count:       1,
			}
			state.secrets = append(state.secrets, s)
			state.stats.Total++
			state.stats.Secrets++
			result := app.SubmitResult{Secret: &s, WasNew: true, Message: "submitted"}
			json.NewEncoder(w).Encode(result) //nolint:errcheck
		}
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state.stats) //nolint:errcheck
	})
	return httptest.NewServer(mux), state
}

// navToSecrets navigates an already-logged-in model to the Secrets panel.
// menuItems = ["Hermit DB", "Benchmark", "Secrets", "Quit"] → Secrets is index 2.
func navToSecrets(m app.Model) (app.Model, tea.Cmd) {
	m, _ = pressDown(m) // index 1
	m, _ = pressDown(m) // index 2
	return pressEnter(m)
}

func TestSecrets_List(t *testing.T) {
	srv, _ := newSecretsTestServer(t)
	defer srv.Close()

	sc := app.NewSecretsClient(srv.URL)
	h := &mockHermit{serverInfo: &pb.ServerInfoResponse{}}
	m := app.New("localhost:9090", "", h, sc)
	m = doLogin(m)

	m, cmd := navToSecrets(m)
	// cmd is tea.Batch(doSecretsList, doSecretsStats) — run the batch msg
	if cmd != nil {
		msg := cmd()
		m, _ = mustModel2(m.Update(msg))
	}

	hasContent(t, m, "secrets list panel")
}

func TestSecrets_Submit(t *testing.T) {
	srv, _ := newSecretsTestServer(t)
	defer srv.Close()

	sc := app.NewSecretsClient(srv.URL)
	h := &mockHermit{serverInfo: &pb.ServerInfoResponse{}}
	m := app.New("localhost:9090", "", h, sc)
	m = doLogin(m)

	m, cmd := navToSecrets(m)
	if cmd != nil {
		msg := cmd()
		m, _ = mustModel2(m.Update(msg))
	}

	for _, c := range "mysecret" {
		m, _ = sendKey(m, c)
	}
	m, cmd = pressEnter(m)
	m, cmd = runCmd(m, cmd) // secretSubmitMsg → secretsLog updated, fires refresh batch
	if cmd != nil {
		msg := cmd()
		m, _ = mustModel2(m.Update(msg))
	}

	hasContent(t, m, "after secret submission")
}

// mustModel2 is a variant that works when Update() is called directly.
func mustModel2(iface tea.Model, cmd tea.Cmd) (app.Model, tea.Cmd) {
	return iface.(app.Model), cmd
}
