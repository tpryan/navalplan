package tools

import (
	"testing"
)

func TestGetSunriseSunset(t *testing.T) {
	tests := []struct {
		name        string
		args        SunriseArgs
		expected    SunriseResult
		expectError bool
	}{
		{
			name: "Jamestown, RI",
			args: SunriseArgs{
				Latitude:  41.497,
				Longitude: -71.362,
				Date:      "2026-05-29",
			},
			// These values are deterministic for the given library and rounding logic
			expected: SunriseResult{
				Date:    "2026-05-29",
				Sunrise: "2026-05-29T04:15:45",
				Sunset:  "2026-05-29T19:10:19",
			},
			expectError: false,
		},
		{
			name: "Invalid Date",
			args: SunriseArgs{
				Latitude:  0,
				Longitude: 0,
				Date:      "invalid",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &SunriseProvider{}
			got, err := sp.GetSunriseSunset(nil, tt.args)

			if tt.expectError {
				if err == nil {
					t.Errorf("GetSunriseSunset() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetSunriseSunset() error = %v", err)
				return
			}
			if got.Sunrise != tt.expected.Sunrise {
				t.Errorf("GetSunriseSunset() sunrise = %v, want %v", got.Sunrise, tt.expected.Sunrise)
			}
			if got.Sunset != tt.expected.Sunset {
				t.Errorf("GetSunriseSunset() sunset = %v, want %v", got.Sunset, tt.expected.Sunset)
			}
		})
	}
}

func TestNewSunriseTool(t *testing.T) {
	tool, _, err := NewSunriseTool("dummy-key")
	if err != nil {
		t.Fatalf("NewSunriseTool() error = %v", err)
	}

	if tool.Name() != "get_sunrise_sunset" {
		t.Errorf("NewSunriseTool().Name() = %v, want %v", tool.Name(), "get_sunrise_sunset")
	}
}
