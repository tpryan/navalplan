package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObscureString(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "Obscure full string",
			input:    []string{"secretpassword"},
			expected: "**************",
		},
		{
			name:     "Obscure partial string",
			input:    []string{"postgres://user:password@localhost:5432/db", "password"},
			expected: "postgres://user:********@localhost:5432/db",
		},
		{
			name:     "Obscure empty string",
			input:    []string{"hello", ""},
			expected: "hello",
		},
		{
			name:     "Input is empty",
			input:    []string{"", "something"},
			expected: "",
		},
		{
			name:     "Obscure multiple occurrences",
			input:    []string{"password-password", "password"},
			expected: "********-********",
		},
		{
			name:     "toObscure not found",
			input:    []string{"hello world", "secret"},
			expected: "hello world",
		},
		{
			name:     "Unicode characters",
			input:    []string{"my password is 😊", "😊"},
			expected: "my password is *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ObscureString(tt.input...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadConfig_ContentDir(t *testing.T) {
	tests := []struct {
		name           string
		envValue       string
		expectedResult string
	}{
		{
			name:           "Env used when set",
			envValue:       "./env-dir",
			expectedResult: "./env-dir",
		},
		{
			name:           "Default used when env empty",
			envValue:       "",
			expectedResult: "./static.min",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGetEnv := func(key string) string {
				if key == "NAVALPLAN_CONTENT_DIR" {
					return tt.envValue
				}
				if key == "NAVALPLAN_OA_CLIENT" {
					return "fake-client"
				}
				if key == "NAVALPLAN_OA_SECRET" {
					return "fake-secret"
				}
				if key == "NAVALPLAN_SYSTEM_KEY" {
					return "fake-key"
				}
				if key == "NAVALPLAN_BACKEND_MAPS_API_KEY" {
					return "fake-maps-key"
				}
				return ""
			}

			cfg, err := New(mockGetEnv)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, cfg.ContentDir)
		})
	}
}
