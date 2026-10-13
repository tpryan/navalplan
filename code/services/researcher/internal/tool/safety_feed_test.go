package tool

import (
	"strings"
	"testing"
)

// sampleBroadcastWarnings mirrors the shape of the NGA MSI broadcast-warn API.
// Positions are near Newport, RI (~41.49, -71.31) for the in-range cases.
const sampleBroadcastWarnings = `{
  "broadcast-warn": [
    {
      "msgYear": 2026, "msgNumber": 101, "navArea": "IV", "status": "A",
      "text": "Shoaling reported in approach channel, buoy 2 off station.",
      "positions": [{"latitude": 41.50, "longitude": -71.30}]
    },
    {
      "msgYear": 2026, "msgNumber": 102, "navArea": "IV", "status": "A",
      "text": "Armed robbery and piracy reported against vessels at anchor.",
      "positions": [{"latitude": 41.40, "longitude": -71.35}]
    },
    {
      "msgYear": 2026, "msgNumber": 103, "navArea": "IV", "status": "A",
      "text": "Derelict vessel adrift far offshore.",
      "positions": [{"latitude": 25.00, "longitude": -80.00}]
    },
    {
      "msgYear": 2026, "msgNumber": 104, "navArea": "IV", "status": "C",
      "text": "Cancelled: buoy restored to station near Newport.",
      "positions": [{"latitude": 41.49, "longitude": -71.31}]
    }
  ]
}`

func TestParseBroadcastWarnings_InRangeAndSecurity(t *testing.T) {
	hazards, level := parseBroadcastWarnings([]byte(sampleBroadcastWarnings), 41.49, -71.31, 50)

	// Expect the two active, in-range warnings (shoaling + piracy); the far-off
	// one and the cancelled one are excluded.
	if len(hazards) != 2 {
		t.Fatalf("expected 2 in-range active hazards, got %d: %+v", len(hazards), hazards)
	}
	if level != "elevated" {
		t.Errorf("expected security level 'elevated' (piracy present), got %q", level)
	}
	joined := strings.Join(hazards, " | ")
	if !strings.Contains(joined, "Shoaling") || !strings.Contains(joined, "piracy") {
		t.Errorf("hazards missing expected content: %q", joined)
	}
	if strings.Contains(joined, "Derelict") {
		t.Errorf("out-of-range warning should be excluded: %q", joined)
	}
	if strings.Contains(joined, "Cancelled") {
		t.Errorf("cancelled (status C) warning should be excluded: %q", joined)
	}
}

func TestParseBroadcastWarnings_NoneInRange(t *testing.T) {
	// Far from all positions -> no hazards, normal level.
	hazards, level := parseBroadcastWarnings([]byte(sampleBroadcastWarnings), -33.0, 151.0, 100)
	if len(hazards) != 0 {
		t.Errorf("expected no hazards far from all positions, got %+v", hazards)
	}
	if level != "normal" {
		t.Errorf("expected 'normal' with no hazards, got %q", level)
	}
}

func TestParseBroadcastWarnings_AdvisoriesLevel(t *testing.T) {
	// A single non-security in-range warning -> advisories.
	oneWarn := `{"broadcast-warn":[{"msgYear":2026,"msgNumber":200,"navArea":"IV","status":"A","text":"Fog signal inoperative.","positions":[{"latitude":41.49,"longitude":-71.31}]}]}`
	hazards, level := parseBroadcastWarnings([]byte(oneWarn), 41.49, -71.31, 50)
	if len(hazards) != 1 {
		t.Fatalf("expected 1 hazard, got %+v", hazards)
	}
	if level != "advisories" {
		t.Errorf("expected 'advisories', got %q", level)
	}
}

func TestParseBroadcastWarnings_Malformed(t *testing.T) {
	hazards, level := parseBroadcastWarnings([]byte("not json"), 41.49, -71.31, 50)
	if hazards != nil {
		t.Errorf("expected nil hazards on malformed input, got %+v", hazards)
	}
	if level != "unknown" {
		t.Errorf("expected 'unknown' on malformed input, got %q", level)
	}
}

func TestFetchSafetyAlerts_DisabledByDefault(t *testing.T) {
	// With the feed disabled (env not set), no network call is made and the
	// result is a graceful no-data response.
	t.Setenv("NAVALPLAN_SAFETY_ALERTS_ENABLED", "")
	hazards, level := fetchSafetyAlerts(nil, 41.49, -71.31, 50)
	if hazards != nil || level != "unknown" {
		t.Errorf("expected no-data (nil, unknown) when disabled, got %+v / %q", hazards, level)
	}
}

func TestHaversineNM(t *testing.T) {
	// ~1 degree of latitude is ~60 NM.
	d := haversineNM(41.0, -71.0, 42.0, -71.0)
	if d < 59 || d > 61 {
		t.Errorf("expected ~60 NM for 1 deg latitude, got %.1f", d)
	}
}
