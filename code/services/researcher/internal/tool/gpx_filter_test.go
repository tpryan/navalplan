package tool

import (
	"strings"
	"testing"
)

func TestFilterGPXDataResult(t *testing.T) {
	ptrFloat := func(v float64) *float64 { return &v }

	tests := []struct {
		name              string
		req               FilterGPXRequest
		wantErr           bool
		wantGlitchesCount int
		wantCleanMaxSpeed float64
		wantRawMaxSpeed   float64
		checkFilteredGPX  bool
	}{
		{
			name: "Normal track with smooth speeds (no glitches)",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(5.0)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:00:10Z", SpeedKts: ptrFloat(5.3)},
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:00:20Z", SpeedKts: ptrFloat(5.7)},
					{Latitude: 25.03, Longitude: -80.03, Time: "2026-08-15T12:00:30Z", SpeedKts: ptrFloat(5.5)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 0,
			wantCleanMaxSpeed: 5.7,
			wantRawMaxSpeed:   5.7,
		},
		{
			name: "Sudden acceleration spike glitch in short timeframe",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(6.0)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:00:03Z", SpeedKts: ptrFloat(29.0)}, // +23 kts in 3s (~7.6 kts/s) -> GLITCH
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:00:06Z", SpeedKts: ptrFloat(6.2)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 1,
			wantCleanMaxSpeed: 6.2,
			wantRawMaxSpeed:   29.0,
		},
		{
			name: "Speed exceeding maximum plausible threshold",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(6.0)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:01:00Z", SpeedKts: ptrFloat(58.0)}, // > 45 kts cutoff
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:02:00Z", SpeedKts: ptrFloat(6.5)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 1,
			wantCleanMaxSpeed: 6.5,
			wantRawMaxSpeed:   58.0,
		},
		{
			name: "Isolated 3-point peak speed spike",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(4.5)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:00:05Z", SpeedKts: ptrFloat(15.0)}, // spikes up and returns to 4.8
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:00:10Z", SpeedKts: ptrFloat(4.8)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 1,
			wantCleanMaxSpeed: 4.8,
			wantRawMaxSpeed:   15.0,
		},
		{
			name: "Consecutive GPS glitches",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(5.0)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:00:02Z", SpeedKts: ptrFloat(25.0)},
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:00:04Z", SpeedKts: ptrFloat(27.0)},
					{Latitude: 25.03, Longitude: -80.03, Time: "2026-08-15T12:00:08Z", SpeedKts: ptrFloat(5.5)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 2,
			wantCleanMaxSpeed: 5.5,
			wantRawMaxSpeed:   27.0,
		},
		{
			name: "Gradual acceleration over reasonable timeframe is valid",
			req: FilterGPXRequest{
				TrackPoints: []TrackPointInput{
					{Latitude: 25.0, Longitude: -80.0, Time: "2026-08-15T12:00:00Z", SpeedKts: ptrFloat(2.0)},
					{Latitude: 25.01, Longitude: -80.01, Time: "2026-08-15T12:00:30Z", SpeedKts: ptrFloat(5.0)},
					{Latitude: 25.02, Longitude: -80.02, Time: "2026-08-15T12:01:00Z", SpeedKts: ptrFloat(8.0)},
					{Latitude: 25.03, Longitude: -80.03, Time: "2026-08-15T12:01:30Z", SpeedKts: ptrFloat(11.0)},
				},
			},
			wantErr:           false,
			wantGlitchesCount: 0,
			wantCleanMaxSpeed: 11.0,
			wantRawMaxSpeed:   11.0,
		},
		{
			name: "Raw GPX XML track with speed spike",
			req: FilterGPXRequest{
				GPXData: `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="Plotter" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Leg 1</name>
    <trkseg>
      <trkpt lat="25.000" lon="-80.000">
        <time>2026-08-15T12:00:00Z</time>
        <speed>3.086</speed> <!-- 6.0 kts -->
      </trkpt>
      <trkpt lat="25.001" lon="-80.001">
        <time>2026-08-15T12:00:02Z</time>
        <speed>16.977</speed> <!-- 33.0 kts: +27 kts in 2s -> GLITCH -->
      </trkpt>
      <trkpt lat="25.002" lon="-80.002">
        <time>2026-08-15T12:00:05Z</time>
        <speed>3.344</speed> <!-- 6.5 kts -->
      </trkpt>
    </trkseg>
  </trk>
</gpx>`,
			},
			wantErr:           false,
			wantGlitchesCount: 1,
			wantCleanMaxSpeed: 6.5,
			wantRawMaxSpeed:   33.0,
			checkFilteredGPX:  true,
		},
		{
			name: "Empty request errors",
			req: FilterGPXRequest{
				GPXData: "",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := FilterGPXDataResult(tc.req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("FilterGPXDataResult() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if res.GlitchesCount != tc.wantGlitchesCount {
				t.Errorf("GlitchesCount = %d, want %d", res.GlitchesCount, tc.wantGlitchesCount)
			}
			if res.CleanMaxSpeedKts != tc.wantCleanMaxSpeed {
				t.Errorf("CleanMaxSpeedKts = %v, want %v", res.CleanMaxSpeedKts, tc.wantCleanMaxSpeed)
			}
			if res.RawMaxSpeedKts != tc.wantRawMaxSpeed {
				t.Errorf("RawMaxSpeedKts = %v, want %v", res.RawMaxSpeedKts, tc.wantRawMaxSpeed)
			}
			if tc.checkFilteredGPX {
				if res.FilteredGPX == "" {
					t.Errorf("FilteredGPX is empty, expected sanitized GPX XML")
				}
				if strings.Contains(res.FilteredGPX, "16.977") {
					t.Errorf("FilteredGPX contains glitched speed point")
				}
			}
			if res.Summary == "" {
				t.Errorf("Expected non-empty Summary")
			}
		})
	}
}
