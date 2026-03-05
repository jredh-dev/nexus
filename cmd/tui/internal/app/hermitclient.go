// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	pb "github.com/jredh-dev/nexus/cmd/tui/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const secretMetadataKey = "x-hermit-secret"

// HermitClient is the interface for the hermit gRPC server.
// Tests inject a mock; production uses grpcHermitClient.
type HermitClient interface {
	Login(username, token string) error
	ServerInfo() (*pb.ServerInfoResponse, error)
	Benchmark(iterations, payloadBytes uint32) (*pb.BenchmarkResponse, error)
	KvSet(key string, value []byte) (*pb.KvSetResponse, error)
	KvGet(key string) (*pb.KvGetResponse, error)
	KvList() (*pb.KvListResponse, error)
	SqlInsert(key, value string) (*pb.SqlInsertResponse, error)
	SqlQuery(keyFilter string, limit uint32) (*pb.SqlQueryResponse, error)
	DbStats() (*pb.DbStatsResponse, error)
	Close()
}

// --- gRPC implementation ---

type grpcHermitClient struct {
	conn   *grpc.ClientConn
	client pb.HermitClient
	secret string
}

// NewHermitClient dials addr and returns a HermitClient.
//
// TLS modes:
//   - insecurePlaintext=true:  plaintext h2c (local Docker, dev mode)
//   - insecurePlaintext=false: proper TLS with system CA pool (Cloud Run, production)
//
// Non-blocking: errors surface on first RPC call.
func NewHermitClient(addr, secret string, insecurePlaintext bool) (HermitClient, error) {
	var creds credentials.TransportCredentials
	if insecurePlaintext {
		creds = insecure.NewCredentials()
	} else {
		// Production: use system CA pool. Cloud Run has valid certs from Google.
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	return &grpcHermitClient{
		conn:   conn,
		client: pb.NewHermitClient(conn),
		secret: secret,
	}, nil
}

func (c *grpcHermitClient) ctx(timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if c.secret != "" {
		base = metadata.AppendToOutgoingContext(base, secretMetadataKey, c.secret)
	}
	return context.WithTimeout(base, timeout)
}

func (c *grpcHermitClient) Login(username, token string) error {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	resp, err := c.client.Login(ctx, &pb.LoginRequest{Username: username, Token: token})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("login failed: %s", resp.Error)
	}
	return nil
}

func (c *grpcHermitClient) ServerInfo() (*pb.ServerInfoResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.ServerInfo(ctx, &pb.ServerInfoRequest{})
}

func (c *grpcHermitClient) Benchmark(iterations, payloadBytes uint32) (*pb.BenchmarkResponse, error) {
	ctx, cancel := c.ctx(30 * time.Second)
	defer cancel()
	return c.client.Benchmark(ctx, &pb.BenchmarkRequest{Iterations: iterations, PayloadBytes: payloadBytes})
}

func (c *grpcHermitClient) KvSet(key string, value []byte) (*pb.KvSetResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.KvSet(ctx, &pb.KvSetRequest{Key: key, Value: value})
}

func (c *grpcHermitClient) KvGet(key string) (*pb.KvGetResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.KvGet(ctx, &pb.KvGetRequest{Key: key})
}

func (c *grpcHermitClient) KvList() (*pb.KvListResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.KvList(ctx, &pb.KvListRequest{})
}

func (c *grpcHermitClient) SqlInsert(key, value string) (*pb.SqlInsertResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.SqlInsert(ctx, &pb.SqlInsertRequest{Key: key, Value: value})
}

func (c *grpcHermitClient) SqlQuery(keyFilter string, limit uint32) (*pb.SqlQueryResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.SqlQuery(ctx, &pb.SqlQueryRequest{KeyFilter: keyFilter, Limit: limit})
}

func (c *grpcHermitClient) DbStats() (*pb.DbStatsResponse, error) {
	ctx, cancel := c.ctx(5 * time.Second)
	defer cancel()
	return c.client.DbStats(ctx, &pb.DbStatsRequest{})
}

func (c *grpcHermitClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
