package handlers

import (
	"testing"
)

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal JSON",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "Markdown JSON block",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "Escaped single quote",
			input:    `{"name": "JB\'s on the Water"}`,
			expected: `{"name": "JB's on the Water"}`,
		},
		{
			name:     "Multiple escaped single quotes",
			input:    `{"description": "It\'s a sunny day at JB\'s."}`,
			expected: `{"description": "It's a sunny day at JB's."}`,
		},
		{
			name:     "Whitespace handling",
			input:    "   ```json   \n  {\"a\": 1}  \n  ```   ",
			expected: `{"a": 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSON(tt.input)
			if got != tt.expected {
				t.Errorf("cleanJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}
