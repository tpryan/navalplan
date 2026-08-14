package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tpryan/navalplan/services/researcher/internal/tool"
)

func TestHandler_ServeHTTP_ListTools(t *testing.T) {
	handler := NewHandler(context.Background(), &tool.NauticalToolService{})
	reqBody, _ := json.Marshal(JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      1,
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
	}

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != float64(1) {
		t.Errorf("expected ID 1, got %v", resp.ID)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %T", result["tools"])
	}

	if len(tools) != 5 {
		t.Errorf("expected 5 tools, got %v", len(tools))
	}
}

func TestHandler_ServeHTTP_CallTool(t *testing.T) {
	handler := NewHandler(context.Background(), &tool.NauticalToolService{})
	args, _ := json.Marshal(map[string]string{"region": "Atlantic"})
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "GetSafetyAlerts",
		"arguments": json.RawMessage(args),
	})
	reqBody, _ := json.Marshal(JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params:  params,
		ID:      1,
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
	}

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	if result["security_level"] != "Normal" {
		t.Errorf("expected security_level Normal, got %v", result["security_level"])
	}
}
