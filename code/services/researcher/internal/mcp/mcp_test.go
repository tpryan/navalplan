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

	if len(tools) != 8 {
		t.Errorf("expected 8 tools, got %v", len(tools))
	}
}

func TestHandler_AllADKToolsExposedInMCP(t *testing.T) {
	svc := &tool.NauticalToolService{}
	adkTools, err := svc.AsTools()
	if err != nil {
		t.Fatalf("AsTools() error: %v", err)
	}

	handler := NewHandler(context.Background(), svc)
	reqBody, _ := json.Marshal(JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      1,
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	result := resp.Result.(map[string]interface{})
	rawTools := result["tools"].([]interface{})

	mcpToolNames := make(map[string]bool, len(rawTools))
	for _, raw := range rawTools {
		tm := raw.(map[string]interface{})
		mcpToolNames[tm["name"].(string)] = true
	}

	for _, at := range adkTools {
		if !mcpToolNames[at.Name()] {
			t.Errorf("ADK tool %q from NauticalToolService.AsTools() is missing from MCP tools/list", at.Name())
		}
	}
}

func TestHandler_ServeHTTP_CallCalculateHeading(t *testing.T) {
	handler := NewHandler(context.Background(), &tool.NauticalToolService{})
	args, _ := json.Marshal(map[string]interface{}{
		"from_latitude":  37.7749,
		"from_longitude": -122.4194,
		"to_latitude":    37.8044,
		"to_longitude":   -122.2712,
	})
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "CalculateHeading",
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
		t.Fatalf("expected status OK, got %v", rr.Code)
	}
	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if _, ok := result["heading_degrees"]; !ok {
		t.Errorf("expected heading_degrees in result, got %v", result)
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

	// With the live navigational-warnings feed disabled (default), GetSafetyAlerts
	// returns an explicit no-data response rather than fabricated hazards.
	if result["security_level"] != "unknown" {
		t.Errorf("expected security_level unknown (feed disabled), got %v", result["security_level"])
	}
}

func TestHandler_ServeHTTP_CallSailingDirections(t *testing.T) {
	_, sdp, _ := tool.NewSailingDirectionsTool("", "us-central1", "", "")
	svc := &tool.NauticalToolService{
		SailingDirections: sdp,
	}
	handler := NewHandler(context.Background(), svc)

	args, _ := json.Marshal(map[string]string{"query": "Cape Cod Canal"})
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "QuerySailingDirections",
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
}
