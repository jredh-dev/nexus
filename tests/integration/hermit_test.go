//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	pb "github.com/jredh-dev/nexus/cmd/tui/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func hermitAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("HERMIT_ADDR")
	if addr == "" {
		addr = "localhost:9090"
	}
	return addr
}

func hermitClient(t *testing.T) pb.HermitClient {
	t.Helper()
	addr := hermitAddr(t)

	var opts []grpc.DialOption

	// Use TLS if not explicitly disabled
	if os.Getenv("HERMIT_INSECURE") == "true" {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsCfg := &tls.Config{InsecureSkipVerify: true} // Self-signed OK for dev/test
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		t.Fatalf("dial hermit at %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })

	return pb.NewHermitClient(conn)
}

func hermitCtx(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx := context.Background()
	if secret := os.Getenv("HERMIT_SECRET"); secret != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-hermit-secret", secret)
	}
	return context.WithTimeout(ctx, timeout)
}

func TestPing(t *testing.T) {
	client := hermitClient(t)
	ctx, cancel := hermitCtx(t, 5*time.Second)
	defer cancel()

	resp, err := client.Ping(ctx, &pb.PingRequest{ClientSendNs: time.Now().UnixNano()})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if resp.ServerRecvNs == 0 {
		t.Error("ServerRecvNs should be non-zero")
	}
}

func TestServerInfo(t *testing.T) {
	client := hermitClient(t)
	ctx, cancel := hermitCtx(t, 5*time.Second)
	defer cancel()

	resp, err := client.ServerInfo(ctx, &pb.ServerInfoRequest{})
	if err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	if resp.Version == "" {
		t.Error("Version should not be empty")
	}
	if resp.UptimeSeconds < 0 {
		t.Error("Uptime should be non-negative")
	}
}

func TestBenchmark(t *testing.T) {
	client := hermitClient(t)
	ctx, cancel := hermitCtx(t, 30*time.Second)
	defer cancel()

	resp, err := client.Benchmark(ctx, &pb.BenchmarkRequest{Iterations: 10, PayloadBytes: 0})
	if err != nil {
		t.Fatalf("Benchmark: %v", err)
	}
	if len(resp.LatenciesNs) != 10 {
		t.Errorf("expected 10 latencies, got %d", len(resp.LatenciesNs))
	}
}

func TestKvSetGetList(t *testing.T) {
	client := hermitClient(t)

	// Set
	ctx, cancel := hermitCtx(t, 5*time.Second)
	setResp, err := client.KvSet(ctx, &pb.KvSetRequest{Key: "test-key", Value: []byte("test-value")})
	cancel()
	if err != nil {
		t.Fatalf("KvSet: %v", err)
	}
	if !setResp.Ok {
		t.Errorf("KvSet ok=false: %s", setResp.Error)
	}

	// Get
	ctx, cancel = hermitCtx(t, 5*time.Second)
	getResp, err := client.KvGet(ctx, &pb.KvGetRequest{Key: "test-key"})
	cancel()
	if err != nil {
		t.Fatalf("KvGet: %v", err)
	}
	if !getResp.Found {
		t.Error("KvGet: key not found")
	}
	if string(getResp.Value) != "test-value" {
		t.Errorf("KvGet: got %q, want %q", getResp.Value, "test-value")
	}

	// List
	ctx, cancel = hermitCtx(t, 5*time.Second)
	listResp, err := client.KvList(ctx, &pb.KvListRequest{})
	cancel()
	if err != nil {
		t.Fatalf("KvList: %v", err)
	}
	found := false
	for _, k := range listResp.Keys {
		if k == "test-key" {
			found = true
		}
	}
	if !found {
		t.Error("KvList: test-key not in list")
	}
}

func TestSqlInsertQuery(t *testing.T) {
	client := hermitClient(t)

	// Insert
	ctx, cancel := hermitCtx(t, 5*time.Second)
	insResp, err := client.SqlInsert(ctx, &pb.SqlInsertRequest{Key: "greeting", Value: "hello world"})
	cancel()
	if err != nil {
		t.Fatalf("SqlInsert: %v", err)
	}
	if !insResp.Queued {
		t.Errorf("SqlInsert queued=false: %s", insResp.Error)
	}

	// Query
	ctx, cancel = hermitCtx(t, 5*time.Second)
	qResp, err := client.SqlQuery(ctx, &pb.SqlQueryRequest{KeyFilter: "greeting", Limit: 10})
	cancel()
	if err != nil {
		t.Fatalf("SqlQuery: %v", err)
	}
	if len(qResp.Rows) == 0 {
		t.Error("SqlQuery: no rows returned")
	}
	if qResp.TotalCommitted == 0 {
		t.Error("SqlQuery: total_committed should be > 0")
	}
}

func TestDbStats(t *testing.T) {
	client := hermitClient(t)
	ctx, cancel := hermitCtx(t, 5*time.Second)
	defer cancel()

	resp, err := client.DbStats(ctx, &pb.DbStatsRequest{})
	if err != nil {
		t.Fatalf("DbStats: %v", err)
	}
	// Just verify it returns without error -- values depend on prior tests
	_ = resp.DocKeyCount
	_ = resp.RelRowCount
}
