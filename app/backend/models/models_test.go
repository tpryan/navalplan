package models_test

import (
	"encoding/json"
	"testing"

	"app/models"
	"github.com/stretchr/testify/assert"
)

func TestRawJSON_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    models.RawJSON
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "valid json",
			input:    models.RawJSON(`{"foo":"bar"}`),
			expected: `{"foo":"bar"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.MarshalJSON()
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestRawJSON_UnmarshalJSON(t *testing.T) {
	input := []byte(`{"key":"value"}`)
	var r models.RawJSON
	err := r.UnmarshalJSON(input)
	assert.NoError(t, err)
	assert.Equal(t, string(input), string(r))
}

func TestBriefing_JSON(t *testing.T) {
	// Higher level test to ensure it works with json.Marshal
	b := models.Briefing{
		WeatherSummary: models.RawJSON(`{"temp":20}`),
	}
	
	data, err := json.Marshal(b)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `{"temp":20}`)
	
	var b2 models.Briefing
	err = json.Unmarshal(data, &b2)
	assert.NoError(t, err)
	assert.JSONEq(t, string(b.WeatherSummary), string(b2.WeatherSummary))
}
