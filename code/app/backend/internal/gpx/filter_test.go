package gpx

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestFilterGPX(t *testing.T) {
	tests := []struct {
		name          string
		inputXML      string
		wantGlitches  int
		wantMaxSpeed  float64
		checkCleanXML bool
	}{
		{
			name: "Clean track with no glitches",
			inputXML: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Clean Sail</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time><speed>3.0</speed></trkpt>
      <trkpt lat="25.01" lon="-80.0"><time>2026-07-01T10:05:00Z</time><speed>3.2</speed></trkpt>
      <trkpt lat="25.02" lon="-80.0"><time>2026-07-01T10:10:00Z</time><speed>3.1</speed></trkpt>
    </trkseg>
  </trk>
</gpx>`,
			wantGlitches:  0,
			checkCleanXML: false,
		},
		{
			name: "Track with speed exceeding MaxPlausibleSpeedKts (55 kts)",
			inputXML: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Glitch Sail</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time><speed>3.0</speed></trkpt>
      <trkpt lat="25.01" lon="-80.0"><time>2026-07-01T10:05:00Z</time><speed>30.0</speed></trkpt>
      <trkpt lat="25.02" lon="-80.0"><time>2026-07-01T10:10:00Z</time><speed>3.2</speed></trkpt>
    </trkseg>
  </trk>
</gpx>`,
			wantGlitches:  1,
			checkCleanXML: true,
		},
		{
			name: "Track with acceleration spike over short time window",
			inputXML: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Acceleration Glitch</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time><speed>2.5</speed></trkpt>
      <trkpt lat="25.001" lon="-80.0"><time>2026-07-01T10:00:02Z</time><speed>12.0</speed></trkpt>
      <trkpt lat="25.002" lon="-80.0"><time>2026-07-01T10:00:04Z</time><speed>2.6</speed></trkpt>
    </trkseg>
  </trk>
</gpx>`,
			wantGlitches:  1,
			checkCleanXML: true,
		},
		{
			name: "Track with 3-point peak spike",
			inputXML: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Peak Spike</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time><speed>3.0</speed></trkpt>
      <trkpt lat="25.005" lon="-80.0"><time>2026-07-01T10:00:05Z</time><speed>15.0</speed></trkpt>
      <trkpt lat="25.010" lon="-80.0"><time>2026-07-01T10:00:10Z</time><speed>3.2</speed></trkpt>
    </trkseg>
  </trk>
</gpx>`,
			wantGlitches:  1,
			checkCleanXML: true,
		},
		{
			name: "Track with location jump without speed tag",
			inputXML: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Position Jump</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time></trkpt>
      <trkpt lat="25.2" lon="-80.0"><time>2026-07-01T10:00:05Z</time></trkpt>
      <trkpt lat="25.001" lon="-80.0"><time>2026-07-01T10:00:10Z</time></trkpt>
    </trkseg>
  </trk>
</gpx>`,
			wantGlitches:  1,
			checkCleanXML: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanData, glitches, err := FilterGPX([]byte(tt.inputXML))
			if err != nil {
				t.Fatalf("FilterGPX error = %v", err)
			}
			if glitches != tt.wantGlitches {
				t.Errorf("FilterGPX glitches = %d, want %d", glitches, tt.wantGlitches)
			}
			if tt.checkCleanXML {
				if string(cleanData) == tt.inputXML {
					t.Errorf("FilterGPX returned original XML unmodified when glitches were expected")
				}
				// Verify clean XML parses back into valid GPX
				var doc filterGPXDoc
				if err := xml.Unmarshal(cleanData, &doc); err != nil {
					t.Fatalf("Clean XML failed to unmarshal: %v", err)
				}
				if len(doc.Tracks) == 0 || len(doc.Tracks[0].Segments) == 0 {
					t.Fatalf("Clean XML has empty tracks or segments")
				}
				// Check that glitch point was removed
				if len(doc.Tracks[0].Segments[0].TrackPoints) != 2 {
					t.Errorf("Clean track has %d points, want 2", len(doc.Tracks[0].Segments[0].TrackPoints))
				}
			} else {
				if string(cleanData) != tt.inputXML {
					t.Errorf("FilterGPX altered clean XML unnecessarily")
				}
			}
		})
	}
}

func TestFilterTrackPoints(t *testing.T) {
	t0 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Second)
	t2 := t0.Add(10 * time.Second)

	sNormal := 5.0
	sGlitch := 80.0

	tests := []struct {
		name         string
		points       []Point
		wantCount    int
		wantGlitches int
	}{
		{
			name: "All clean points",
			points: []Point{
				{Lat: 25.0, Lng: -80.0, Time: &t0, SpeedKts: &sNormal},
				{Lat: 25.001, Lng: -80.0, Time: &t1, SpeedKts: &sNormal},
				{Lat: 25.002, Lng: -80.0, Time: &t2, SpeedKts: &sNormal},
			},
			wantCount:    3,
			wantGlitches: 0,
		},
		{
			name: "Middle point is 80 knot speed glitch",
			points: []Point{
				{Lat: 25.0, Lng: -80.0, Time: &t0, SpeedKts: &sNormal},
				{Lat: 25.001, Lng: -80.0, Time: &t1, SpeedKts: &sGlitch},
				{Lat: 25.002, Lng: -80.0, Time: &t2, SpeedKts: &sNormal},
			},
			wantCount:    2,
			wantGlitches: 1,
		},
		{
			name: "Two points (below minimum threshold)",
			points: []Point{
				{Lat: 25.0, Lng: -80.0, Time: &t0, SpeedKts: &sNormal},
				{Lat: 25.001, Lng: -80.0, Time: &t1, SpeedKts: &sNormal},
			},
			wantCount:    2,
			wantGlitches: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanPts, glitches := FilterTrackPoints(tt.points)
			if glitches != tt.wantGlitches {
				t.Errorf("FilterTrackPoints glitches = %d, want %d", glitches, tt.wantGlitches)
			}
			if len(cleanPts) != tt.wantCount {
				t.Errorf("FilterTrackPoints len = %d, want %d", len(cleanPts), tt.wantCount)
			}
		})
	}
}

func TestParseGPX_WithGlitchFiltering(t *testing.T) {
	// Verify that ParseGPX automatically cleans up glitchy tracks and produces clean ParsedTrack
	glitchyXML := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Glitchy Import</name>
    <trkseg>
      <trkpt lat="25.0" lon="-80.0"><time>2026-07-01T10:00:00Z</time><speed>3.0</speed></trkpt>
      <trkpt lat="25.001" lon="-80.0"><time>2026-07-01T10:00:02Z</time><speed>40.0</speed></trkpt>
      <trkpt lat="25.002" lon="-80.0"><time>2026-07-01T10:00:04Z</time><speed>3.1</speed></trkpt>
    </trkseg>
  </trk>
</gpx>`

	tracks, err := ParseGPX([]byte(glitchyXML), "recorded")
	if err != nil {
		t.Fatalf("ParseGPX failed: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("ParseGPX returned %d tracks, want 1", len(tracks))
	}

	track := tracks[0]
	if track.MaxSpeedKts > MaxPlausibleSpeedKts {
		t.Errorf("Track MaxSpeedKts %.1f exceeds MaxPlausibleSpeedKts %.1f", track.MaxSpeedKts, MaxPlausibleSpeedKts)
	}
	if len(track.Points) != 2 {
		t.Errorf("Track has %d points, want 2 (glitch removed)", len(track.Points))
	}

	// Verify GeoJSON does not contain the glitch point
	if strings.Contains(string(track.GeoJSON), "40.0") {
		t.Errorf("Track GeoJSON still contains glitch point speed 40.0")
	}
}
