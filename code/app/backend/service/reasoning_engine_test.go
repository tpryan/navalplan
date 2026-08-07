package service

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestExtractTextFromValue(t *testing.T) {
	tests := []struct {
		name     string
		input    *structpb.Value
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "",
		},
		{
			name:     "string value",
			input:    structpb.NewStringValue("hello world"),
			expected: "hello world",
		},
		{
			name: "reasoning engine output wrapper with content string",
			input: func() *structpb.Value {
				v, _ := structpb.NewValue(map[string]any{
					"output": map[string]any{
						"session_id": "s123",
						"content":    "```json\n{\"summary\": \"Great voyage\"}\n```",
					},
				})
				return v
			}(),
			expected: "```json\n{\"summary\": \"Great voyage\"}\n```",
		},
		{
			name: "adk nested content parts",
			input: func() *structpb.Value {
				v, _ := structpb.NewValue(map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]any{"text": "Part 1 "},
							map[string]any{"text": "Part 2"},
						},
					},
				})
				return v
			}(),
			expected: "Part 1 Part 2",
		},
		{
			name: "simple text key in struct",
			input: func() *structpb.Value {
				v, _ := structpb.NewValue(map[string]any{
					"text": "Direct text",
				})
				return v
			}(),
			expected: "Direct text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromValue(tt.input)
			if got != tt.expected {
				t.Errorf("extractTextFromValue() = %q, want %q", got, tt.expected)
			}
		})
	}
}
