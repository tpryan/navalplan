package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"golang.org/x/oauth2"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// newTestServer uses httptest.NewServer when loopback TCP bind is permitted,
// and falls back to an in-memory httptest.ResponseRecorder RoundTripper inside
// network-restricted sandboxes.
func newTestServer(t *testing.T, handler http.HandlerFunc) (string, *http.Client) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		srv := httptest.NewUnstartedServer(handler)
		srv.Listener = ln
		srv.Start()
		t.Cleanup(srv.Close)
		return srv.URL, srv.Client()
	}
	return "http://mock.claude.local", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler(rec, r)
			return rec.Result(), nil
		}),
	}
}

func TestClaudeModel_VertexAIToolUseRoundTrip(t *testing.T) {
	var capturedHeaders http.Header
	var capturedPath string
	var capturedBody map[string]any

	baseURL, httpClient := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedPath = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(raw, &capturedBody); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":          "msg_01",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-sonnet-4-5@20250929",
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Checking tides now.",
				},
				{
					"type": "tool_use",
					"id":   "toolu_resp_1",
					"name": "GetTides",
					"input": map[string]any{
						"station": "9414290",
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  120,
				"output_tokens": 45,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	cfg := &config.Config{
		Project:        "navallog-prod",
		GeminiLocation: "global",
	}
	llm, err := NewClaudeModel(context.Background(), "claude-sonnet-4-5", cfg)
	if err != nil {
		t.Fatalf("NewClaudeModel() error = %v", err)
	}
	cm := llm.(*claudeModel)
	cm.baseURL = baseURL
	cm.httpClient = httpClient
	cm.tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "vertex-test-token"})

	temp := float32(0.2)
	req := &model.LLMRequest{
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 65536, // should be capped at 8192 for Claude
			Temperature:     &temp,
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: "You are a maritime pilot."},
				},
			},
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "GetTides",
							Description: "Fetches tide predictions.",
							Parameters: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"station": {
										Type:        genai.TypeString,
										Description: "NOAA station ID",
									},
								},
								Required: []string{"station"},
							},
						},
						{
							Name:        "batch_web_search",
							Description: "Runs parallel web searches.",
							ParametersJsonSchema: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"queries": {
										Type: genai.TypeArray,
										Items: &genai.Schema{
											Type: genai.TypeString,
										},
									},
								},
								Required: []string{"queries"},
							},
						},
					},
				},
			},
		},
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: "First user part."},
				},
			},
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: "Second consecutive user part."},
				},
			},
			{
				Role: genai.RoleModel,
				Parts: []*genai.Part{
					{
						FunctionCall: &genai.FunctionCall{
							Name: "GetTides",
							Args: map[string]any{},
						},
					},
				},
			},
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: "Follow-up text in same user turn as tool_result."},
					{
						FunctionResponse: &genai.FunctionResponse{
							Name:     "GetTides",
							Response: map[string]any{"high": "5.4ft"},
						},
					},
				},
			},
		},
	}

	var responses []*model.LLMResponse
	for resp, err := range llm.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		responses = append(responses, resp)
	}

	wantPath := "/v1/projects/navallog-prod/locations/global/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict"
	if capturedPath != wantPath {
		t.Errorf("request path = %q, want %q", capturedPath, wantPath)
	}
	if got := capturedHeaders.Get("Authorization"); got != "Bearer vertex-test-token" {
		t.Errorf("Authorization = %q, want Bearer vertex-test-token", got)
	}
	if got := capturedBody["anthropic_version"]; got != "vertex-2023-10-16" {
		t.Errorf("anthropic_version = %v, want vertex-2023-10-16", got)
	}
	if _, hasModel := capturedBody["model"]; hasModel {
		t.Errorf("Vertex rawPredict request body must omit 'model', got %v", capturedBody["model"])
	}
	if got := capturedBody["max_tokens"]; got != float64(defaultClaudeMaxTokens) {
		t.Errorf("body max_tokens = %v, want %d", got, defaultClaudeMaxTokens)
	}
	if got := capturedBody["system"]; got != "You are a maritime pilot." {
		t.Errorf("body system = %v, want 'You are a maritime pilot.'", got)
	}

	// Verify tools conversion
	toolsSlice, ok := capturedBody["tools"].([]any)
	if !ok || len(toolsSlice) != 2 {
		t.Fatalf("tools = %#v, want 2 tools", capturedBody["tools"])
	}
	tool0 := toolsSlice[0].(map[string]any)
	schema0 := tool0["input_schema"].(map[string]any)
	if schema0["type"] != "object" {
		t.Errorf("tool[0] input_schema.type = %v, want object", schema0["type"])
	}
	tool1 := toolsSlice[1].(map[string]any)
	schema1 := tool1["input_schema"].(map[string]any)
	props1 := schema1["properties"].(map[string]any)
	queriesSchema := props1["queries"].(map[string]any)
	if queriesSchema["type"] != "array" {
		t.Errorf("tool[1] queries.type = %v, want array", queriesSchema["type"])
	}

	// Verify consecutive user message merging and tool_result ordering
	msgs, ok := capturedBody["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("messages count = %d, want 3 (merged user, assistant tool_use, user tool_result+text)", len(msgs))
	}
	msg0 := msgs[0].(map[string]any)
	if len(msg0["content"].([]any)) != 2 {
		t.Errorf("msg[0] content count = %d, want 2 merged blocks", len(msg0["content"].([]any)))
	}
	msg1 := msgs[1].(map[string]any)
	toolUseBlock := msg1["content"].([]any)[0].(map[string]any)
	callID := toolUseBlock["id"].(string)
	if callID == "" {
		t.Errorf("expected generated non-empty tool_use id")
	}
	if _, hasInput := toolUseBlock["input"]; !hasInput {
		t.Errorf("tool_use block missing required 'input' object when args are empty")
	}
	msg2 := msgs[2].(map[string]any)
	msg2Content := msg2["content"].([]any)
	if len(msg2Content) != 2 {
		t.Fatalf("msg[2] content count = %d, want 2", len(msg2Content))
	}
	firstBlock := msg2Content[0].(map[string]any)
	if firstBlock["type"] != "tool_result" {
		t.Errorf("msg[2] first block type = %v, want tool_result before text", firstBlock["type"])
	}
	if firstBlock["tool_use_id"] != callID {
		t.Errorf("tool_result.tool_use_id = %v, want %q", firstBlock["tool_use_id"], callID)
	}

	// Verify translated LLMResponse
	if len(responses) != 1 {
		t.Fatalf("responses len = %d, want 1", len(responses))
	}
	gotResp := responses[0]
	if !gotResp.TurnComplete {
		t.Errorf("TurnComplete = false, want true")
	}
	if gotResp.Content == nil || len(gotResp.Content.Parts) != 2 {
		t.Fatalf("response parts = %#v, want 2 parts", gotResp.Content)
	}
	if gotResp.Content.Parts[0].Text != "Checking tides now." {
		t.Errorf("part[0].Text = %q, want 'Checking tides now.'", gotResp.Content.Parts[0].Text)
	}
	fc := gotResp.Content.Parts[1].FunctionCall
	if fc == nil || fc.Name != "GetTides" || fc.ID != "toolu_resp_1" || fc.Args["station"] != "9414290" {
		t.Errorf("part[1].FunctionCall = %#v, want GetTides(station=9414290, id=toolu_resp_1)", fc)
	}
}

func TestClaudeModel_VertexAIRawPredict(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedBody map[string]any

	baseURL, httpClient := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &capturedBody); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_vertex_1",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-sonnet-4-5@20250929",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Vertex response OK."},
			},
		})
	})

	cfg := &config.Config{
		Project:        "navallog-prod",
		GeminiLocation: "us-east5",
	}
	llm, err := NewClaudeModel(context.Background(), "claude-sonnet-4-5", cfg)
	if err != nil {
		t.Fatalf("NewClaudeModel() error = %v", err)
	}
	cm := llm.(*claudeModel)
	cm.baseURL = baseURL
	cm.httpClient = httpClient
	cm.tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "vertex-test-token"})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("Hello Vertex Claude", "user"),
		},
	}

	for resp, err := range llm.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		if resp.Content.Parts[0].Text != "Vertex response OK." {
			t.Errorf("response text = %q, want 'Vertex response OK.'", resp.Content.Parts[0].Text)
		}
	}

	wantPath := "/v1/projects/navallog-prod/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict"
	if capturedPath != wantPath {
		t.Errorf("capturedPath = %q, want %q", capturedPath, wantPath)
	}
	if capturedAuth != "Bearer vertex-test-token" {
		t.Errorf("Authorization = %q, want 'Bearer vertex-test-token'", capturedAuth)
	}
	if got := capturedBody["anthropic_version"]; got != "vertex-2023-10-16" {
		t.Errorf("anthropic_version = %v, want vertex-2023-10-16", got)
	}
	if _, hasModel := capturedBody["model"]; hasModel {
		t.Errorf("Vertex rawPredict request body must omit 'model', got %v", capturedBody["model"])
	}

	// Verify production Vertex endpoint URL construction for regional and global locations
	cm.baseURL = ""
	if got := cm.endpointURL("claude-sonnet-4-5"); got != "https://us-east5-aiplatform.googleapis.com"+wantPath {
		t.Errorf("regional endpointURL = %q", got)
	}
	cm.location = "global"
	wantGlobal := "https://aiplatform.googleapis.com/v1/projects/navallog-prod/locations/global/publishers/anthropic/models/claude-haiku-4-5@20251001:rawPredict"
	if got := cm.endpointURL("claude-haiku-4-5"); got != wantGlobal {
		t.Errorf("global endpointURL = %q, want %q", got, wantGlobal)
	}
}
