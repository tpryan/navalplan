package tools

import (
	"testing"

	"github.com/tpryan/openmeteogo"
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

func TestNewWeatherTool(t *testing.T) {
	tool, _, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool() error = %v", err)
	}

	if tool.Name() != "get_weather_forecast" {
		t.Errorf("NewWeatherTool().Name() = %v, want %v", tool.Name(), "get_weather_forecast")
	}
}

func TestGetWeatherForecast_InvalidDate(t *testing.T) {
	args := WeatherArgs{
		Latitude:  41.497,
		Longitude: -71.362,
		Date:      "invalid",
	}

	wp := &WeatherProvider{client: openmeteogo.NewClient()}
	_, err := wp.GetWeatherForecast(nil, args)
	if err == nil {
		t.Error("Expected error for invalid date, got none")
	}
}
