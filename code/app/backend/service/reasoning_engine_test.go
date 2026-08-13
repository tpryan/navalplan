package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func parseStreamChunk(data []byte) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if line == "" || line == "[DONE]" {
				continue
			}
		}

		var event AgentEvent
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			if (event.Content.Role == "" || event.Content.Role == "model") && len(event.Content.Parts) > 0 {
				for _, p := range event.Content.Parts {
					if p.Text != "" && !p.Thought {
						sb.WriteString(p.Text)
					}
				}
			}
		} else {
			var generic struct {
				Content string `json:"content"`
				Text    string `json:"text"`
				Output  struct {
					Content string `json:"content"`
					Text    string `json:"text"`
				} `json:"output"`
			}
			if err := json.Unmarshal([]byte(line), &generic); err == nil && (generic.Content != "" || generic.Text != "" || generic.Output.Content != "" || generic.Output.Text != "") {
				if generic.Content != "" {
					sb.WriteString(generic.Content)
				} else if generic.Text != "" {
					sb.WriteString(generic.Text)
				} else if generic.Output.Content != "" {
					sb.WriteString(generic.Output.Content)
				} else if generic.Output.Text != "" {
					sb.WriteString(generic.Output.Text)
				}
			} else if !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[") {
				sb.WriteString(line)
			}
		}
	}
	return sb.String()
}

func TestParseStreamChunk(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "SSE data with model text",
			input:    "data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"{\\\"recommendations\\\":[]}\"}]}}\n\n",
			expected: "{\"recommendations\":[]}",
		},
		{
			name:     "Filter out thoughts",
			input:    "data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"thinking...\",\"thought\":true},{\"text\":\"actual result\"}]}}\n",
			expected: "actual result",
		},
		{
			name:     "SSE DONE signal",
			input:    "data: [DONE]\n\n",
			expected: "",
		},
		{
			name:     "Generic content field",
			input:    "data: {\"content\":\"hello world\"}\n",
			expected: "hello world",
		},
		{
			name:     "Multi-line stream data",
			input:    "data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"part 1\"}]}}\ndata: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"part 2\"}]}}\n",
			expected: "part 1part 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStreamChunk([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("parseStreamChunk() = %q, want %q", got, tt.expected)
			}
		})
	}
}
