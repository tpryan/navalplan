package server

import "testing"

func TestLookupString(t *testing.T) {
	tests := []struct {
		name     string
		fallback string
		maps     []map[string]any
		keys     []string
		want     string
	}{
		{
			name:     "first map first key wins",
			fallback: "fallback",
			maps:     []map[string]any{{"a": "one", "b": "two"}},
			keys:     []string{"a", "b"},
			want:     "one",
		},
		{
			name:     "falls through to second key in same map",
			fallback: "fallback",
			maps:     []map[string]any{{"b": "two"}},
			keys:     []string{"a", "b"},
			want:     "two",
		},
		{
			name:     "falls through to second map",
			fallback: "fallback",
			maps:     []map[string]any{{"a": ""}, {"a": "second-map"}},
			keys:     []string{"a"},
			want:     "second-map",
		},
		{
			name:     "empty string values are skipped, not returned",
			fallback: "fallback",
			maps:     []map[string]any{{"a": ""}},
			keys:     []string{"a"},
			want:     "fallback",
		},
		{
			name:     "non-string values are skipped",
			fallback: "fallback",
			maps:     []map[string]any{{"a": 42}},
			keys:     []string{"a"},
			want:     "fallback",
		},
		{
			name:     "nil map is safe to scan",
			fallback: "fallback",
			maps:     []map[string]any{nil},
			keys:     []string{"a"},
			want:     "fallback",
		},
		{
			name:     "no match returns fallback",
			fallback: "fallback",
			maps:     []map[string]any{{"z": "irrelevant"}},
			keys:     []string{"a", "b"},
			want:     "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lookupString(tt.fallback, tt.maps, tt.keys...)
			if got != tt.want {
				t.Errorf("lookupString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseReasoningEngineRequest(t *testing.T) {
	tests := []struct {
		name          string
		req           reasoningEngineRequest
		wantMessage   string
		wantAppName   string
		wantUserID    string
		wantSessionID string
	}{
		{
			name: "message from input.message",
			req: reasoningEngineRequest{
				Input: map[string]any{"message": "hello"},
			},
			wantMessage:   "hello",
			wantAppName:   "harbourmaster",
			wantUserID:    "default_user",
			wantSessionID: "default_session",
		},
		{
			name: "message falls back to input.input",
			req: reasoningEngineRequest{
				Input: map[string]any{"input": "hi there"},
			},
			wantMessage:   "hi there",
			wantAppName:   "harbourmaster",
			wantUserID:    "default_user",
			wantSessionID: "default_session",
		},
		{
			name: "message never falls back to parameters",
			req: reasoningEngineRequest{
				Input:      map[string]any{},
				Parameters: map[string]any{"message": "should not be used"},
			},
			wantMessage:   "",
			wantAppName:   "harbourmaster",
			wantUserID:    "default_user",
			wantSessionID: "default_session",
		},
		{
			name: "camelCase routing fields in input",
			req: reasoningEngineRequest{
				Input: map[string]any{
					"message":   "hi",
					"appName":   "pilot",
					"userID":    "u1",
					"sessionID": "s1",
				},
			},
			wantMessage:   "hi",
			wantAppName:   "pilot",
			wantUserID:    "u1",
			wantSessionID: "s1",
		},
		{
			name: "snake_case routing fields in input",
			req: reasoningEngineRequest{
				Input: map[string]any{
					"message":    "hi",
					"app_name":   "commodore",
					"user_id":    "u2",
					"session_id": "s2",
				},
			},
			wantMessage:   "hi",
			wantAppName:   "commodore",
			wantUserID:    "u2",
			wantSessionID: "s2",
		},
		{
			name: "routing fields fall back to parameters (camelCase and snake_case)",
			req: reasoningEngineRequest{
				Input: map[string]any{"message": "hi"},
				Parameters: map[string]any{
					"appName":   "specialist",
					"userID":    "u3",
					"sessionID": "s3",
				},
			},
			wantMessage:   "hi",
			wantAppName:   "specialist",
			wantUserID:    "u3",
			wantSessionID: "s3",
		},
		{
			name: "input takes precedence over parameters",
			req: reasoningEngineRequest{
				Input:      map[string]any{"message": "hi", "appName": "lookout"},
				Parameters: map[string]any{"appName": "specialist"},
			},
			wantMessage:   "hi",
			wantAppName:   "lookout",
			wantUserID:    "default_user",
			wantSessionID: "default_session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, appName, userID, sessionID := parseReasoningEngineRequest(tt.req)
			if message != tt.wantMessage {
				t.Errorf("message = %q, want %q", message, tt.wantMessage)
			}
			if appName != tt.wantAppName {
				t.Errorf("appName = %q, want %q", appName, tt.wantAppName)
			}
			if userID != tt.wantUserID {
				t.Errorf("userID = %q, want %q", userID, tt.wantUserID)
			}
			if sessionID != tt.wantSessionID {
				t.Errorf("sessionID = %q, want %q", sessionID, tt.wantSessionID)
			}
		})
	}
}
