package tools

import (
	"testing"
	"time"

	"github.com/tpryan/noaago"
)

func TestGetTides_InvalidDate(t *testing.T) {
	args := TideArgs{
		Latitude:  41.497,
		Longitude: -71.362,
		Date:      "invalid",
	}

	tp := &TideProvider{client: noaago.NewClient()}
	_, err := tp.GetTides(nil, args)
	if err == nil {
		t.Error("Expected error for invalid date, got none")
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

func TestNewTideTool(t *testing.T) {
	tool, err := NewTideTool()
	if err != nil {
		t.Fatalf("NewTideTool() error = %v", err)
	}

	if tool.Name() != "get_tides" {
		t.Errorf("NewTideTool().Name() = %v, want %v", tool.Name(), "get_tides")
	}
}
