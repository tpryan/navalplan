package handlers

import "testing"

func TestNormalizeDuration(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"02:46:58", "2h 46m 58s"},
		{"02h 58m", "2h 58m 0s"},
		{"1 day 02:00:05", "26h 0m 5s"},
		{"45m", "0h 45m 0s"},
		{"", ""},
		{"N/A", "N/A"},
	}
	for _, tc := range tests {
		if got := normalizeDuration(tc.in); got != tc.want {
			t.Errorf("normalizeDuration(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := formatHMS(16.3 / 5.5 * 60); got != "2h 57m 49s" {
		t.Errorf("formatHMS = %q, want 2h 57m 49s", got)
	}
}
