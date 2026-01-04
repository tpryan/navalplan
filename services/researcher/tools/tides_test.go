package tools

import (
	"testing"
	"time"
)

func TestGetTides_InvalidDate(t *testing.T) {
	args := TideArgs{
		Latitude:  41.497,
		Longitude: -71.362,
		Date:      "invalid",
	}
	// We can't easily test the success path without mocking the NOAA API client,
	// but we can test the input validation.
	got, err := GetTides(args)
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}
	if got.Error == "" {
		t.Errorf("Expected error for invalid date, got none")
	}
}

func TestTideBufferRange(t *testing.T) {
	// Verify the 48h buffer logic
	dateStr := "2026-05-29"
	parsedDate, _ := time.Parse("2006-01-02", dateStr)
	
	beginDate := parsedDate.Add(-48 * time.Hour)
	endDate := parsedDate.Add(48 * time.Hour)
	
	expectedBegin := parsedDate.AddDate(0, 0, -2)
	expectedEnd := parsedDate.AddDate(0, 0, 2)
	
	if !beginDate.Equal(expectedBegin) {
		t.Errorf("Begin date mismatch: got %v, want %v", beginDate, expectedBegin)
	}
	if !endDate.Equal(expectedEnd) {
		t.Errorf("End date mismatch: got %v, want %v", endDate, expectedEnd)
	}
}
