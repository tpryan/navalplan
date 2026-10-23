package gpx

import (
	"strings"
	"testing"
)

func TestAnalyzeGPXTelemetry_TacksAndXTE(t *testing.T) {
	plannedGeoJSON := []byte(`{"type":"Feature","geometry":{"type":"LineString","coordinates":[[-71.50,41.10],[-71.50,41.30]]}}`)
	rawXML := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1">
  <trk><name>Upwind Leg</name><trkseg>
    <trkpt lat="41.1000" lon="-71.5000"><time>2025-07-15T12:00:00Z</time></trkpt>
    <trkpt lat="41.1400" lon="-71.4600"><time>2025-07-15T12:30:00Z</time></trkpt>
    <trkpt lat="41.1800" lon="-71.5400"><time>2025-07-15T13:00:00Z</time></trkpt>
    <trkpt lat="41.2200" lon="-71.4600"><time>2025-07-15T13:30:00Z</time></trkpt>
    <trkpt lat="41.2400" lon="-71.4800"><time>2025-07-15T14:30:00Z</time></trkpt>
    <trkpt lat="41.2600" lon="-71.5000"><time>2025-07-15T15:30:00Z</time></trkpt>
    <trkpt lat="41.2800" lon="-71.5000"><time>2025-07-15T16:00:00Z</time></trkpt>
    <trkpt lat="41.3000" lon="-71.5000"><time>2025-07-15T16:30:00Z</time></trkpt>
  </trkseg></trk>
</gpx>`

	got := AnalyzeGPXTelemetry(plannedGeoJSON, nil, &rawXML, 5.2)
	if got.PointCount != 8 {
		t.Fatalf("expected 8 points, got %d", got.PointCount)
	}
	if got.TackAndGybeCount < 2 {
		t.Errorf("expected at least 2 tacks, got %d", got.TackAndGybeCount)
	}
	if got.MaxCrossTrackNM <= 0.5 {
		t.Errorf("expected MaxCrossTrackNM > 0.5 NM, got %.2f", got.MaxCrossTrackNM)
	}
	if got.EstimatedVMGKts <= 0 {
		t.Errorf("expected positive EstimatedVMGKts, got %.2f", got.EstimatedVMGKts)
	}
	if !strings.Contains(got.FormattedSummary, "Cross-Track Error") {
		t.Errorf("expected FormattedSummary to include Cross-Track Error, got: %s", got.FormattedSummary)
	}
}
