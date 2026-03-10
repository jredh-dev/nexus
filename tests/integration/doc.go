// Package integration contains integration tests for nexus services.
//
// These tests require running service instances and are gated behind
// the "integration" build tag:
//
//	go test -tags integration ./tests/integration/...
//
// Environment variables:
//
//	HERMIT_ADDR         gRPC address (default: localhost:9090)
//	HERMIT_SECRET       shared secret for x-hermit-secret header (optional)
//	HERMIT_INSECURE     set to "true" to disable TLS (default: TLS with system CAs)
//	HERMIT_BEARER_TOKEN OAuth2/IAM bearer token for Cloud Run auth (optional)
//	SECRETS_URL         HTTP base URL (default: http://localhost:8082)
//	VN_URL              HTTP base URL for vn service (default: http://localhost:8082)
//
// MCP services:
//
//	GITHUB_MCP_URL      github-mcp base URL (default: http://localhost:8091)
//	DISCORD_MCP_URL     discord-mcp base URL (default: http://localhost:8092)
//	GCLOUD_MCP_URL      gcloud-mcp base URL (default: http://localhost:8093)
package integration
