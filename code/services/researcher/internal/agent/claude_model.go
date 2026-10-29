package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const (
	vertexAnthropicVersion = "vertex-2023-10-16"
	defaultClaudeMaxTokens = 32768
)

// claudeModel implements model.LLM for Anthropic Claude models on Google Cloud
// Vertex AI Model Garden (using the same cfg.Project, cfg.GeminiLocation, and
// GCP Application Default Credentials as the rest of the service).
type claudeModel struct {
	name        string
	project     string
	location    string
	baseURL     string
	httpClient  *http.Client
	mu          sync.Mutex
	tokenSource oauth2.TokenSource
}

// NewClaudeModel constructs a model.LLM backed by Anthropic Claude on Vertex AI
// Model Garden using cfg.Project and cfg.GeminiLocation (GOOGLE_CLOUD_LOCATION,
// defaulting to "global") with Application Default Credentials.
func NewClaudeModel(_ context.Context, modelName string, cfg *config.Config) (model.LLM, error) {
	if strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("claude model name must not be empty")
	}
	var project, location string
	if cfg != nil {
		project = cfg.Project
		location = cfg.GeminiLocation
	}
	if project == "" {
		project = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if project == "" {
		project = os.Getenv("GCP_PROJECT")
	}
	if project == "" {
		project = os.Getenv("GCLOUD_PROJECT")
	}
	if project == "" {
		project = os.Getenv("PROJECT_ID")
	}
	if location == "" {
		location = os.Getenv("GOOGLE_CLOUD_LOCATION")
	}
	if location == "" {
		location = "global"
	}
	if project == "" {
		return nil, fmt.Errorf("claude model %q requires GOOGLE_CLOUD_PROJECT / PROJECT_ID for Vertex AI", modelName)
	}
	return &claudeModel{
		name:       modelName,
		project:    project,
		location:   location,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}, nil
}

func (m *claudeModel) Name() string {
	return m.name
}

func (m *claudeModel) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(resp, nil)
	}
}

type anthropicRequest struct {
	Model            string             `json:"model,omitempty"`
	AnthropicVersion string             `json:"anthropic_version,omitempty"`
	MaxTokens        int32              `json:"max_tokens"`
	Temperature      *float32           `json:"temperature,omitempty"`
	System           string             `json:"system,omitempty"`
	Messages         []anthropicMessage `json:"messages"`
	Tools            []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

func (b anthropicContentBlock) MarshalJSON() ([]byte, error) {
	switch b.Type {
	case "text":
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			Type: "text",
			Text: b.Text,
		})
	case "tool_use":
		input := b.Input
		if input == nil {
			input = map[string]any{}
		}
		return json.Marshal(struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}{
			Type:  "tool_use",
			ID:    b.ID,
			Name:  b.Name,
			Input: input,
		})
	case "tool_result":
		return json.Marshal(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content"`
		}{
			Type:      "tool_result",
			ToolUseID: b.ToolUseID,
			Content:   b.Content,
		})
	default:
		type alias anthropicContentBlock
		return json.Marshal(alias(b))
	}
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      *struct {
		InputTokens  int32 `json:"input_tokens"`
		OutputTokens int32 `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

func (m *claudeModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	targetModel := m.name
	if req != nil && req.Model != "" && config.IsClaudeModel(req.Model) {
		targetModel = req.Model
	}
	payload := buildAnthropicRequest(req)
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal claude request: %w", err)
	}

	endpoint := m.endpointURL(targetModel)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create claude request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	token, err := m.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtain vertex oauth token for claude: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	var respBody []byte
	var lastStatus int
	backoffs := []time.Duration{0, 2 * time.Second, 5 * time.Second}
	for attempt, d := range backoffs {
		if d > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}
		reqAttempt, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create claude request: %w", err)
		}
		reqAttempt.Header = httpReq.Header.Clone()

		httpResp, err := client.Do(reqAttempt)
		if err != nil {
			if attempt < len(backoffs)-1 && ctx.Err() == nil {
				continue
			}
			return nil, fmt.Errorf("call claude endpoint %s: %w", endpoint, err)
		}
		respBody, err = io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read claude response body: %w", err)
		}
		lastStatus = httpResp.StatusCode
		if lastStatus >= 200 && lastStatus < 300 {
			break
		}
		if lastStatus == 404 && m.baseURL == "" && (m.location == "" || m.location == "global") {
			// Some Anthropic models on Vertex AI (e.g. Sonnet) are provisioned in us-east5
			// rather than the global endpoint; retry against us-east5 automatically.
			m.location = "us-east5"
			endpoint = m.endpointURL(targetModel)
			continue
		}
		if (lastStatus == 429 || lastStatus == 500 || lastStatus == 502 || lastStatus == 503 || lastStatus == 504 || lastStatus == 529) && attempt < len(backoffs)-1 {
			continue
		}
		return nil, fmt.Errorf("claude API error (status %d): %s", lastStatus, strings.TrimSpace(string(respBody)))
	}
	if lastStatus < 200 || lastStatus >= 300 {
		return nil, fmt.Errorf("claude API error (status %d): %s", lastStatus, strings.TrimSpace(string(respBody)))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode claude response: %w", err)
	}
	return translateAnthropicResponse(&apiResp), nil
}

func (m *claudeModel) endpointURL(targetModel string) string {
	vertexModel := resolveVertexClaudeModelID(targetModel)
	loc := m.location
	if loc == "" {
		loc = "global"
	}
	base := strings.TrimRight(m.baseURL, "/")
	if base != "" {
		return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict", base, m.project, loc, vertexModel)
	}
	if loc == "global" {
		return fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/global/publishers/anthropic/models/%s:rawPredict", m.project, vertexModel)
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict", loc, m.project, loc, vertexModel)
}

func (m *claudeModel) bearerToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	ts := m.tokenSource
	m.mu.Unlock()

	if ts == nil {
		creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", err
		}
		ts = creds.TokenSource
		m.mu.Lock()
		if m.tokenSource == nil {
			m.tokenSource = ts
		} else {
			ts = m.tokenSource
		}
		m.mu.Unlock()
	}

	tok, err := ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// resolveVertexClaudeModelID maps convenience Claude model aliases to their
// canonical Vertex AI Model Garden publisher model IDs.
func resolveVertexClaudeModelID(modelName string) string {
	trimmed := strings.TrimSpace(modelName)
	if strings.Contains(trimmed, "@") {
		return trimmed
	}
	switch strings.ToLower(trimmed) {
	case "claude-sonnet-4-5", "claude-sonnet-4-5-20250929":
		return "claude-sonnet-4-5@20250929"
	case "claude-haiku-4-5", "claude-haiku-4-5-20251001":
		return "claude-haiku-4-5@20251001"
	case "claude-opus-4-5", "claude-opus-4-5-20251101":
		return "claude-opus-4-5@20251101"
	case "claude-opus-4-1", "claude-opus-4-1-20250805":
		return "claude-opus-4-1@20250805"
	case "claude-sonnet-4", "claude-sonnet-4-0", "claude-sonnet-4-20250514":
		return "claude-sonnet-4@20250514"
	case "claude-opus-4", "claude-opus-4-0", "claude-opus-4-20250514":
		return "claude-opus-4@20250514"
	case "claude-3-7-sonnet", "claude-3-7-sonnet-latest", "claude-3-7-sonnet-20250219":
		return "claude-3-7-sonnet@20250219"
	case "claude-3-5-sonnet", "claude-3-5-sonnet-v2", "claude-3-5-sonnet-latest", "claude-3-5-sonnet-20241022":
		return "claude-3-5-sonnet-v2@20241022"
	case "claude-3-5-haiku", "claude-3-5-haiku-latest", "claude-3-5-haiku-20241022":
		return "claude-3-5-haiku@20241022"
	default:
		return trimmed
	}
}

func buildAnthropicRequest(req *model.LLMRequest) *anthropicRequest {
	maxTokens := int32(defaultClaudeMaxTokens)
	var temperature *float32
	var systemPrompt string
	var tools []anthropicTool

	if req != nil && req.Config != nil {
		if req.Config.MaxOutputTokens > 0 && req.Config.MaxOutputTokens < maxTokens {
			maxTokens = req.Config.MaxOutputTokens
		}
		if req.Config.Temperature != nil {
			t := *req.Config.Temperature
			temperature = &t
		}
		if req.Config.SystemInstruction != nil {
			var sysParts []string
			for _, part := range req.Config.SystemInstruction.Parts {
				if part != nil && !part.Thought && part.Text != "" {
					sysParts = append(sysParts, part.Text)
				}
			}
			systemPrompt = strings.Join(sysParts, "\n\n")
		}
		for _, toolGroup := range req.Config.Tools {
			if toolGroup == nil {
				continue
			}
			for _, fd := range toolGroup.FunctionDeclarations {
				if fd == nil || fd.Name == "" {
					continue
				}
				tools = append(tools, anthropicTool{
					Name:        fd.Name,
					Description: fd.Description,
					InputSchema: convertFunctionSchema(fd),
				})
			}
		}
	}

	var contents []*genai.Content
	if req != nil {
		contents = req.Contents
	}
	messages := convertContentsToMessages(contents)

	return &anthropicRequest{
		AnthropicVersion: vertexAnthropicVersion,
		MaxTokens:        maxTokens,
		Temperature:      temperature,
		System:           systemPrompt,
		Messages:         messages,
		Tools:            tools,
	}
}

func convertContentsToMessages(contents []*genai.Content) []anthropicMessage {
	var messages []anthropicMessage
	idMap := make(map[string]string)
	pendingByName := make(map[string][]string)
	nextCallSeq := 0

	appendBlock := func(role string, block anthropicContentBlock) {
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, block)
			return
		}
		messages = append(messages, anthropicMessage{
			Role:    role,
			Content: []anthropicContentBlock{block},
		})
	}

	for _, content := range contents {
		if content == nil {
			continue
		}
		defaultRole := "user"
		if content.Role == genai.RoleModel || content.Role == "assistant" {
			defaultRole = "assistant"
		}
		for _, part := range content.Parts {
			if part == nil || part.Thought {
				continue
			}
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				var callID string
				if fc.ID != "" {
					callID = sanitizeToolID(fc.ID)
					idMap[fc.ID] = callID
				} else {
					nextCallSeq++
					callID = fmt.Sprintf("toolu_%s_%d", sanitizeToolID(fc.Name), nextCallSeq)
				}
				pendingByName[fc.Name] = append(pendingByName[fc.Name], callID)
				input := fc.Args
				if input == nil {
					input = map[string]any{}
				}
				appendBlock("assistant", anthropicContentBlock{
					Type:  "tool_use",
					ID:    callID,
					Name:  fc.Name,
					Input: input,
				})
				continue
			}
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse
				var respID string
				if fr.ID != "" {
					if mapped, ok := idMap[fr.ID]; ok {
						respID = mapped
					} else {
						respID = sanitizeToolID(fr.ID)
					}
					if q := pendingByName[fr.Name]; len(q) > 0 && q[0] == respID {
						pendingByName[fr.Name] = q[1:]
					}
				} else if q := pendingByName[fr.Name]; len(q) > 0 {
					respID = q[0]
					pendingByName[fr.Name] = q[1:]
				} else {
					nextCallSeq++
					respID = fmt.Sprintf("toolu_%s_%d", sanitizeToolID(fr.Name), nextCallSeq)
				}
				appendBlock("user", anthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: respID,
					Content:   jsonStringOf(fr.Response),
				})
				continue
			}
			if part.Text != "" {
				appendBlock(defaultRole, anthropicContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
		}
	}

	// Anthropic requires tool_result blocks in a "user" message to precede any
	// text blocks in that same message.
	for i := range messages {
		if messages[i].Role != "user" || len(messages[i].Content) <= 1 {
			continue
		}
		var toolResults, otherBlocks []anthropicContentBlock
		for _, b := range messages[i].Content {
			if b.Type == "tool_result" {
				toolResults = append(toolResults, b)
			} else {
				otherBlocks = append(otherBlocks, b)
			}
		}
		if len(toolResults) > 0 && len(otherBlocks) > 0 {
			messages[i].Content = append(toolResults, otherBlocks...)
		}
	}

	if len(messages) == 0 {
		messages = append(messages, anthropicMessage{
			Role: "user",
			Content: []anthropicContentBlock{{
				Type: "text",
				Text: "Continue.",
			}},
		})
	} else if messages[0].Role != "user" {
		messages = append([]anthropicMessage{{
			Role: "user",
			Content: []anthropicContentBlock{{
				Type: "text",
				Text: "Continue.",
			}},
		}}, messages...)
	}

	return messages
}

func sanitizeToolID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "toolu_call"
	}
	return b.String()
}

func jsonStringOf(v map[string]any) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func convertFunctionSchema(fd *genai.FunctionDeclaration) map[string]any {
	if fd == nil {
		return map[string]any{"type": "object"}
	}
	if fd.ParametersJsonSchema != nil {
		if s, ok := fd.ParametersJsonSchema.(*genai.Schema); ok {
			out := schemaToMap(s)
			out["type"] = "object"
			return out
		}
		raw, err := json.Marshal(fd.ParametersJsonSchema)
		if err == nil {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err == nil && m != nil {
				normalizeJSONSchemaMap(m)
				m["type"] = "object"
				return m
			}
		}
	}
	if fd.Parameters != nil {
		out := schemaToMap(fd.Parameters)
		out["type"] = "object"
		return out
	}
	return map[string]any{"type": "object"}
}

func schemaToMap(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object"}
	}
	m := make(map[string]any)
	if s.Type != "" && s.Type != genai.TypeUnspecified {
		m["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if s.Pattern != "" {
		m["pattern"] = s.Pattern
	}
	if s.Title != "" {
		m["title"] = s.Title
	}
	if s.Default != nil {
		m["default"] = s.Default
	}
	if s.Minimum != nil {
		m["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		m["maximum"] = *s.Maximum
	}
	if s.MinItems != nil {
		m["minItems"] = *s.MinItems
	}
	if s.MaxItems != nil {
		m["maxItems"] = *s.MaxItems
	}
	if s.MinLength != nil {
		m["minLength"] = *s.MinLength
	}
	if s.MaxLength != nil {
		m["maxLength"] = *s.MaxLength
	}
	if s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
		if _, ok := m["type"]; !ok {
			m["type"] = "object"
		}
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if len(s.AnyOf) > 0 {
		anyOf := make([]any, 0, len(s.AnyOf))
		for _, sub := range s.AnyOf {
			anyOf = append(anyOf, schemaToMap(sub))
		}
		m["anyOf"] = anyOf
	}
	return m
}

func normalizeJSONSchemaMap(m map[string]any) {
	if m == nil {
		return
	}
	delete(m, "propertyOrdering")
	if t, ok := m["type"].(string); ok {
		switch strings.ToUpper(t) {
		case "OBJECT", "STRING", "NUMBER", "INTEGER", "BOOLEAN", "ARRAY", "NULL":
			m["type"] = strings.ToLower(t)
		case "TYPE_UNSPECIFIED":
			delete(m, "type")
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				normalizeJSONSchemaMap(sub)
			}
		}
	}
	if items, ok := m["items"].(map[string]any); ok {
		normalizeJSONSchemaMap(items)
	}
	if addProps, ok := m["additionalProperties"].(map[string]any); ok {
		normalizeJSONSchemaMap(addProps)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := m[key].([]any); ok {
			for _, elem := range arr {
				if sub, ok := elem.(map[string]any); ok {
					normalizeJSONSchemaMap(sub)
				}
			}
		}
	}
}

func translateAnthropicResponse(resp *anthropicResponse) *model.LLMResponse {
	var parts []*genai.Part
	if resp != nil {
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					parts = append(parts, &genai.Part{Text: block.Text})
				}
			case "tool_use":
				args := block.Input
				if args == nil {
					args = map[string]any{}
				}
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   block.ID,
						Name: block.Name,
						Args: args,
					},
				})
			}
		}
	}
	var usage *genai.GenerateContentResponseUsageMetadata
	var modelVersion string
	if resp != nil {
		modelVersion = resp.Model
		if resp.Usage != nil {
			usage = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     resp.Usage.InputTokens,
				CandidatesTokenCount: resp.Usage.OutputTokens,
				TotalTokenCount:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
			}
		}
	}
	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  genai.RoleModel,
			Parts: parts,
		},
		UsageMetadata: usage,
		ModelVersion:  modelVersion,
		TurnComplete:  true,
	}
}
