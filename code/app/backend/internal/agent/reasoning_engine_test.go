package agent

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestParseAndAppendChunk(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{
			name:     "ADK model event with text parts",
			line:     `{"content":{"role":"model","parts":[{"text":"recommendation chunk"}]}}`,
			expected: "recommendation chunk",
		},
		{
			name:     "ADK model event with thought ignored",
			line:     `{"content":{"role":"model","parts":[{"thought":true,"text":"internal reasoning"},{"text":"visible content"}]}}`,
			expected: "visible content",
		},
		{
			name:     "ADK non-model role ignored",
			line:     `{"content":{"role":"user","parts":[{"text":"user prompt"}]}}`,
			expected: "",
		},
		{
			name:     "Reasoning Engine output content structure",
			line:     `{"output":{"content":"reasoning engine chunk","session_id":"sess_123"}}`,
			expected: "reasoning engine chunk",
		},
		{
			name:     "Reasoning Engine output text structure",
			line:     `{"output":{"text":"reasoning engine text chunk"}}`,
			expected: "reasoning engine text chunk",
		},
		{
			name:     "Generic top-level content",
			line:     `{"content":"generic content string"}`,
			expected: "generic content string",
		},
		{
			name:     "Generic top-level text",
			line:     `{"text":"generic text string"}`,
			expected: "generic text string",
		},
		{
			name:     "Raw map with string output",
			line:     `{"output":"direct output string"}`,
			expected: "direct output string",
		},
		{
			name:     "Plain text non-JSON line",
			line:     `plain text stream line`,
			expected: "plain text stream line",
		},
		{
			name:     "Malformed JSON object ignored",
			line:     `{"incomplete_json": `,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			parseAndAppendChunk(context.Background(), tt.line, &sb)
			if got := sb.String(); got != tt.expected {
				t.Errorf("parseAndAppendChunk() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractTextFromValue(t *testing.T) {
	tests := []struct {
		name     string
		value    *structpb.Value
		expected string
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: "",
		},
		{
			name:     "string value",
			value:    structpb.NewStringValue("simple string"),
			expected: "simple string",
		},
		{
			name: "struct value with content field",
			value: structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"content": structpb.NewStringValue("content value"),
				},
			}),
			expected: "content value",
		},
		{
			name: "struct value with text field",
			value: structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"text": structpb.NewStringValue("text value"),
				},
			}),
			expected: "text value",
		},
		{
			name: "struct value with nested output content",
			value: structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"output": structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"content": structpb.NewStringValue("nested output content"),
						},
					}),
				},
			}),
			expected: "nested output content",
		},
		{
			name: "struct value with nested output string",
			value: structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"output": structpb.NewStringValue("output string value"),
				},
			}),
			expected: "output string value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextFromValue(tt.value); got != tt.expected {
				t.Errorf("extractTextFromValue() = %q, want %q", got, tt.expected)
			}
		})
	}
}
