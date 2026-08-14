package model_test

import (
	"encoding/json"
	"testing"

	"app/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestRawJSON_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    model.RawJSON
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "valid json",
			input:    model.RawJSON(`{"foo":"bar"}`),
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
	var r model.RawJSON
	err := r.UnmarshalJSON(input)
	assert.NoError(t, err)
	assert.Equal(t, string(input), string(r))
}

func TestBriefing_JSON(t *testing.T) {
	b := model.Briefing{
		WeatherSummary: model.RawJSON(`{"temp":20}`),
	}

	data, err := json.Marshal(b)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `{"temp":20}`)

	var b2 model.Briefing
	err = json.Unmarshal(data, &b2)
	assert.NoError(t, err)
	assert.JSONEq(t, string(b.WeatherSummary), string(b2.WeatherSummary))
}

func TestRawJSON_Value(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		var r model.RawJSON
		v, err := r.Value()
		assert.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("non-nil value", func(t *testing.T) {
		r := model.RawJSON(`{"foo":"bar"}`)
		v, err := r.Value()
		assert.NoError(t, err)
		assert.Equal(t, `{"foo":"bar"}`, v)
	})
}

func TestRawJSON_Scan(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		var r model.RawJSON
		err := r.Scan(nil)
		assert.NoError(t, err)
		assert.Nil(t, r)
	})

	t.Run("bytes value", func(t *testing.T) {
		var r model.RawJSON
		input := []byte(`{"foo":"bar"}`)
		err := r.Scan(input)
		assert.NoError(t, err)
		assert.Equal(t, string(input), string(r))
	})

	t.Run("string value", func(t *testing.T) {
		var r model.RawJSON
		input := `{"foo":"bar"}`
		err := r.Scan(input)
		assert.NoError(t, err)
		assert.Equal(t, input, string(r))
	})

	t.Run("invalid type", func(t *testing.T) {
		var r model.RawJSON
		err := r.Scan(123)
		assert.Error(t, err)
		assert.Equal(t, "type assertion to []byte failed", err.Error())
	})
}
