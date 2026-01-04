package tools

import (
	"testing"
)

func TestDegreesToDirection(t *testing.T) {
	tests := []struct {
		deg  float64
		want string
	}{
		{0, "N"},
		{22.5, "NNE"},
		{45, "NE"},
		{90, "E"},
		{180, "S"},
		{270, "W"},
		{350, "N"}, // 350 is close to 360/0
		{337.5, "NNW"},
		{11.24, "N"},
		{11.26, "NNE"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.deg)), func(t *testing.T) {
			if got := DegreesToDirection(tt.deg); got != tt.want {
				t.Errorf("DegreesToDirection(%v) = %v, want %v", tt.deg, got, tt.want)
			}
		})
	}
}
