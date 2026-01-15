package main

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
			expected: "my password is *", // 😊 is 1 rune
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
	// Satisfy required checks using t.Setenv (modifies real env for test duration)
	t.Setenv("NAVALPLAN_OA_CLIENT", "fake-client")
	t.Setenv("NAVALPLAN_OA_SECRET", "fake-secret")
	t.Setenv("NAVALPLAN_SYSTEM_KEY", "fake-key")
	t.Setenv("NAVALPLAN_BACKEND_MAPS_API_KEY", "fake-maps-key")

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
			// Mock getEnv for logic inside loadConfig
			mockGetEnv := func(key string) string {
				if key == "NAVALPLAN_CONTENT_DIR" {
					return tt.envValue
				}
				return ""
			}

			cfg, err := loadConfig(mockGetEnv)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, cfg.ContentDir)
		})
	}
}
