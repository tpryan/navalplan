package handlers

import (
	"encoding/json"
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
		{
			name:     "Truncated JSON",
			input:    `[{"a": 1`,
			expected: "",
		},
		{
			name:     "Garbage after JSON",
			input:    `{"a": 1} garbage`,
			expected: `{"a": 1}`,
		},
		{
			name:     "Garbage before JSON",
			input:    `garbage {"a": 1}`,
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

func TestRepairTruncatedJSONArray(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectValid   bool
		expectedCount int
	}{
		{
			name:          "Already valid object wrapper",
			input:         `{"recommendations": [{"name": "A"}, {"name": "B"}]}`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name:          "Already valid direct array",
			input:         `[{"name": "A"}, {"name": "B"}]`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name: "Truncated object wrapper ending after last complete object",
			input: `{"recommendations": [
				{"name": "Spot 1", "type": "Hub"},
				{"name": "Spot 2", "type": "Anchorage"}`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name: "Truncated object wrapper with incomplete item at end",
			input: `{"recommendations": [
				{"name": "Spot 1", "type": "Hub"},
				{"name": "Spot 2", "type": "Anchorage"},
				{"name": "Spot 3", "type": "Moo`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name: "Truncated direct array with partial trailing item",
			input: `[
				{"name": "Spot 1", "type": "Hub"},
				{"name": "Spot 2", "type": "Anchorage"},
				{"name": "Spot 3"`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name: "Truncated with nested array in objects",
			input: `{"recommendations": [
				{"name": "Spot 1", "reference_links": ["https://a.com", "https://b.com"]},
				{"name": "Spot 2", "reference_links": ["https://c.com"]},
				{"name": "Spot 3", "reference_links": ["https://d.com"`,
			expectValid:   true,
			expectedCount: 2,
		},
		{
			name:        "Empty or non-JSON input",
			input:       "plain text with no json structure",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repaired := repairTruncatedJSONArray(tt.input)
			if tt.expectValid {
				if !json.Valid([]byte(repaired)) {
					t.Fatalf("repairTruncatedJSONArray() produced invalid JSON: %s", repaired)
				}
				var wrapper struct {
					Recommendations []map[string]any `json:"recommendations"`
				}
				if err := json.Unmarshal([]byte(repaired), &wrapper); err == nil && len(wrapper.Recommendations) > 0 {
					if len(wrapper.Recommendations) != tt.expectedCount {
						t.Errorf("expected %d recommendations, got %d", tt.expectedCount, len(wrapper.Recommendations))
					}
				} else {
					var list []map[string]any
					if err := json.Unmarshal([]byte(repaired), &list); err == nil {
						if len(list) != tt.expectedCount {
							t.Errorf("expected %d items in list, got %d", tt.expectedCount, len(list))
						}
					} else {
						t.Fatalf("failed to unmarshal repaired JSON: %v", err)
					}
				}
			}
		})
	}
}
