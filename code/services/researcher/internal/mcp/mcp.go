package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/tpryan/navalplan/services/researcher/internal/tool"
)

type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type Handler struct {
	Ctx      context.Context
	Nautical *tool.NauticalToolService
}

func NewHandler(ctx context.Context, nautical *tool.NauticalToolService) *Handler {
	return &Handler{
		Ctx:      ctx,
		Nautical: nautical,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
							"latitude":   map[string]string{"type": "number"},
							"longitude":  map[string]string{"type": "number"},
							"date":       map[string]string{"type": "string"},
						},
						"required": []string{"date"},
					},
				},
				{
					"name":        "GetWeather",
					"description": "Fetches real-time NOAA offshore marine warnings and wind velocity vectors.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"coordinates": map[string]string{"type": "string"},
							"date":        map[string]string{"type": "string"},
						},
						"required": []string{"coordinates", "date"},
					},
				},
				{
					"name":        "GetSunriseSunset",
					"description": "Retrieves sunrise and sunset times for a specific location and date.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"latitude":  map[string]string{"type": "number"},
							"longitude": map[string]string{"type": "number"},
							"date":      map[string]string{"type": "string"},
						},
						"required": []string{"latitude", "longitude", "date"},
					},
				},
				{
					"name":        "FindPlacesNearby",
					"description": "Finds places (e.g. marinas, restaurants) near a location.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query":     map[string]string{"type": "string"},
							"latitude":  map[string]string{"type": "number"},
							"longitude": map[string]string{"type": "number"},
							"radius":    map[string]string{"type": "number"},
						},
						"required": []string{"query", "latitude", "longitude", "radius"},
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
		slog.Log(ctx, slog.LevelInfo, "MCP tools/call request", "params", string(msg.Params))
		res, err = h.handleToolCall(ctx, msg.Params)
		if err != nil {
			slog.Log(ctx, slog.LevelError, "MCP tools/call error", "error", err)
		} else {
			slog.Log(ctx, slog.LevelInfo, "MCP tools/call success")
		}
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

func (h *Handler) handleToolCall(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var callReq struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callReq); err != nil {
		return nil, err
	}

	switch callReq.Name {
	case "GetTides":
		var req tool.TideRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchTides(nil, req)
	case "GetWeather":
		var req tool.WeatherRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchWeather(nil, req)
	case "GetSunriseSunset":
		var req tool.SunriseRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchSunriseSunset(nil, req)
	case "FindPlacesNearby":
		var req tool.PlacesRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchPlacesNearby(nil, req)
	case "GetSafetyAlerts":
		var req tool.SafetyRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return h.Nautical.FetchSafetyAlerts(nil, req)
	}
	return nil, fmt.Errorf("unknown tool %s", callReq.Name)
}
