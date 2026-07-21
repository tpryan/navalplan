package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tpryan/navalplan/services/researcher/tools"
)

// JSONRPCMessage represents a standard protocol frame.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCResponse represents a standard protocol response frame.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Handler handles external MCP client discovery and execution calls.
type Handler struct {
	Ctx      context.Context
	Nautical *tools.NauticalToolService
}

// NewHandler creates a new MCP server handler.
func NewHandler(ctx context.Context, nautical *tools.NauticalToolService) *Handler {
	return &Handler{
		Ctx:      ctx,
		Nautical: nautical,
	}
}

// ServeHTTP implements the http.Handler interface for the MCP server.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method must be POST", http.StatusMethodNotAllowed)
		return
	}

	var msg JSONRPCMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Malformed JSON payload", http.StatusBadRequest)
		return
	}

	var res interface{}
	var err error

	switch msg.Method {
	case "tools/list":
		res = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "GetTides",
					"description": "Queries hydrographic station databases for current and historic tidal matrices.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"station_id": map[string]string{"type": "string"},
							"date":       map[string]string{"type": "string"},
						},
						"required": []string{"station_id", "date"},
					},
				},
				{
					"name":        "GetWeather",
					"description": "Fetches real-time NOAA offshore marine warnings and wind velocity vectors.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"coordinates": map[string]string{"type": "string"},
						},
						"required": []string{"coordinates"},
					},
				},
				{
					"name":        "GetSafetyAlerts",
					"description": "Extracts active global navigational warnings and localized security alerts.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"region": map[string]string{"type": "string"},
						},
						"required": []string{"region"},
					},
				},
			},
		}
	case "tools/call":
		res, err = h.handleToolCall(msg.Params)
	default:
		err = fmt.Errorf("method not found: %s", msg.Method)
	}

	response := JSONRPCResponse{JSONRPC: "2.0", ID: msg.ID}
	if err != nil {
		response.Error = map[string]string{"message": err.Error()}
	} else {
		response.Result = res
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) handleToolCall(params json.RawMessage) (interface{}, error) {
	var callReq struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callReq); err != nil {
		return nil, err
	}

	switch callReq.Name {
	case "GetTides":
		var req tools.TideRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchTides(nil, req)
	case "GetWeather":
		var req tools.WeatherRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchWeather(nil, req)
	case "GetSafetyAlerts":
		var req tools.SafetyRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchSafetyAlerts(nil, req)
	}
	return nil, fmt.Errorf("unknown tool %s", callReq.Name)
}
