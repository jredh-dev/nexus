package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

const (
	// ProtocolVersion is the MCP protocol version this server supports.
	ProtocolVersion = "2025-03-26"
)

// Version is set via ldflags at build time.
var Version = "dev"

// ToolHandler handles a tool call; receives raw JSON args, returns result or error.
type ToolHandler func(args json.RawMessage) (*ToolCallResult, error)

// Server holds the tool registry and dispatches JSON-RPC requests.
type Server struct {
	mu           sync.RWMutex
	tools        map[string]Tool
	handlers     map[string]ToolHandler
	initialized  bool
	instructions string
	logger       *log.Logger
	serverName   string
}

// NewServer creates a new MCP server with the given logger, server name, and system instructions.
func NewServer(logger *log.Logger, serverName, instructions string) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		tools:        make(map[string]Tool),
		handlers:     make(map[string]ToolHandler),
		instructions: instructions,
		logger:       logger,
		serverName:   serverName,
	}
}

// ServerName returns the server's name as provided to NewServer.
func (s *Server) ServerName() string {
	return s.serverName
}

// RegisterTool adds a tool and its handler to the server.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = handler
}

// HandleRequest processes a single JSON-RPC request and returns a response.
// Returns nil for notifications (no response expected).
func (s *Server) HandleRequest(raw []byte) []byte {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.errorResponse(nil, ErrCodeParse, "parse error", err.Error())
	}

	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, ErrCodeInvalidReq, "invalid jsonrpc version", nil)
	}

	s.logger.Printf("[mcp] method=%s id=%s", req.Method, string(req.ID))

	var result any
	var rpcErr *RPCError

	switch req.Method {
	case "initialize":
		result, rpcErr = s.handleInitialize(req.Params)
	case "initialized":
		return nil // notification
	case "notifications/cancelled":
		return nil // notification
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result, rpcErr = s.handleToolsList()
	case "tools/call":
		result, rpcErr = s.handleToolCall(req.Params)
	default:
		rpcErr = &RPCError{Code: ErrCodeNoMethod, Message: fmt.Sprintf("unknown method: %s", req.Method)}
	}

	if req.IsNotification() {
		return nil
	}

	if rpcErr != nil {
		return s.errorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return s.successResponse(req.ID, result)
}

func (s *Server) handleInitialize(params json.RawMessage) (any, *RPCError) {
	var p InitializeParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: ErrCodeBadParams, Message: "invalid initialize params"}
		}
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	s.logger.Printf("[mcp] client=%s/%s protocol=%s", p.ClientInfo.Name, p.ClientInfo.Version, p.ProtocolVersion)

	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
		ServerInfo:      Implementation{Name: s.serverName, Version: Version},
		Instructions:    s.instructions,
	}, nil
}

func (s *Server) handleToolsList() (any, *RPCError) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}
	return ToolsListResult{Tools: tools}, nil
}

func (s *Server) handleToolCall(params json.RawMessage) (any, *RPCError) {
	var p ToolCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &RPCError{Code: ErrCodeBadParams, Message: "invalid tool call params"}
	}

	s.mu.RLock()
	handler, ok := s.handlers[p.Name]
	s.mu.RUnlock()

	if !ok {
		return &ToolCallResult{
			Content: []ContentBlock{TextContent(fmt.Sprintf("unknown tool: %s", p.Name))},
			IsError: true,
		}, nil
	}

	s.logger.Printf("[mcp] tool=%s", p.Name)
	result, err := handler(p.Arguments)
	if err != nil {
		return &ToolCallResult{
			Content: []ContentBlock{TextContent(fmt.Sprintf("error: %v", err))},
			IsError: true,
		}, nil
	}
	return result, nil
}

func (s *Server) successResponse(id json.RawMessage, result any) []byte {
	data, err := json.Marshal(Response{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		s.logger.Printf("[mcp] marshal error: %v", err)
		return s.errorResponse(id, ErrCodeInternal, "marshal error", nil)
	}
	return data
}

func (s *Server) errorResponse(id json.RawMessage, code int, message string, data any) []byte {
	out, _ := json.Marshal(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message, Data: data},
	})
	return out
}
