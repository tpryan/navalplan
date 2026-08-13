package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func parseStreamChunk(data []byte) string {
	var sb strings.Builder
	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if line == "" || line == "[DONE]" {
				continue
			}
		}

		parseAndAppendChunk(context.Background(), line, &sb)
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

func TestStreamLineBuffering(t *testing.T) {
	// Simulate gRPC chunks split across network packets
	chunk1 := []byte("data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"chunk")
	chunk2 := []byte(" 1 and chunk 2\"}]}}\n")

	var sb strings.Builder
	var lineBuf bytes.Buffer

	// Process chunk 1
	lineBuf.Write(chunk1)
	for {
		lineBytes, err := lineBuf.ReadBytes('\n')
		if err != nil {
			if len(lineBytes) > 0 {
				lineBuf.Write(lineBytes)
			}
			break
		}
		line := strings.TrimSpace(strings.TrimPrefix(string(lineBytes), "data:"))
		parseAndAppendChunk(context.Background(), line, &sb)
	}

	// Should not have parsed incomplete chunk 1 yet
	if sb.Len() > 0 {
		t.Errorf("expected empty output after chunk 1, got %q", sb.String())
	}

	// Process chunk 2
	lineBuf.Write(chunk2)
	for {
		lineBytes, err := lineBuf.ReadBytes('\n')
		if err != nil {
			if len(lineBytes) > 0 {
				lineBuf.Write(lineBytes)
			}
			break
		}
		line := strings.TrimSpace(strings.TrimPrefix(string(lineBytes), "data:"))
		parseAndAppendChunk(context.Background(), line, &sb)
	}

	expected := "chunk 1 and chunk 2"
	if sb.String() != expected {
		t.Errorf("got %q, want %q", sb.String(), expected)
	}
}
