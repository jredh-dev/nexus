package mcp

import (
	"encoding/json"
	"fmt"
)

// JSONResult marshals v as indented JSON and wraps it in a successful ToolCallResult.
func JSONResult(v any) (*ToolCallResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &ToolCallResult{
		Content: []ContentBlock{TextContent(string(data))},
	}, nil
}

// TextResult wraps a plain text string in a ToolCallResult.
func TextResult(text string) *ToolCallResult {
	return &ToolCallResult{
		Content: []ContentBlock{TextContent(text)},
	}
}

// ParseArgs unmarshals raw JSON arguments into dst.
func ParseArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// ErrMissing returns a standard missing-parameter error.
func ErrMissing(param string) error {
	return fmt.Errorf("missing required parameter: %s", param)
}
