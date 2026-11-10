package handlers

import (
	"testing"

	"app/internal/model"
)

// A voyage day runs between two positions whose sea state can differ sharply:
// a passage may leave an exposed coast for an estuary the wave model does not
// cover. These cases pin how the two endpoints collapse into the flat summary
// fields that internal/safety reads.
func TestApplyRepresentativeWaves(t *testing.T) {
	covered := func(h, period float64) *model.WavePoint {
		return &model.WavePoint{HeightFt: h, Period: period, Direction: 270, HourlyHeight: []float64{h}}
	}
	uncovered := func() *model.WavePoint {
		return &model.WavePoint{Unavailable: true}
	}

	tests := []struct {
		name            string
		start, end      *model.WavePoint
		wantHeight      float64
		wantUnavailable bool
	}{
		{
			// The Benicia case: depart San Francisco into real swell, arrive
			// somewhere the model has nothing. The swell must still be
			// reported, or the day silently reads as calm.
			name:  "start covered, end uncovered",
			start: covered(11.7, 16.5), end: uncovered(),
			wantHeight: 11.7, wantUnavailable: false,
		},
		{
			name:  "both covered picks the heavier sea",
			start: covered(3.6, 12), end: covered(9.2, 14),
			wantHeight: 9.2, wantUnavailable: false,
		},
		{
			name:  "both covered, heavier at start",
			start: covered(9.2, 14), end: covered(3.6, 12),
			wantHeight: 9.2, wantUnavailable: false,
		},
		{
			name:  "neither covered",
			start: uncovered(), end: uncovered(),
			wantHeight: 0, wantUnavailable: true,
		},
		{
			// Stationary day: the backend fetches once and leaves end nil.
			name:  "stationary day with coverage",
			start: covered(4.1, 9), end: nil,
			wantHeight: 4.1, wantUnavailable: false,
		},
		{
			name:  "stationary day without coverage",
			start: uncovered(), end: nil,
			wantHeight: 0, wantUnavailable: true,
		},
		{
			// A nil endpoint means the request failed, which is unknown
			// rather than absent; the other end still carries the day.
			name:  "start fetch failed, end covered",
			start: nil, end: covered(5.5, 11),
			wantHeight: 5.5, wantUnavailable: false,
		},
		{
			name:  "both missing entirely",
			start: nil, end: nil,
			wantHeight: 0, wantUnavailable: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := model.WeatherSummary{StartWaves: tc.start, EndWaves: tc.end}
			applyRepresentativeWaves(&ws)

			if ws.WaveHeightFt != tc.wantHeight {
				t.Errorf("WaveHeightFt = %.1f, want %.1f", ws.WaveHeightFt, tc.wantHeight)
			}
			if ws.WaveDataUnavailable != tc.wantUnavailable {
				t.Errorf("WaveDataUnavailable = %v, want %v", ws.WaveDataUnavailable, tc.wantUnavailable)
			}
			// Where a representative was chosen its hourly series must come
			// across too, since the safety checks scan it for peaks.
			if !tc.wantUnavailable && tc.wantHeight > 0 && len(ws.HourlyWaveHeight) == 0 {
				t.Errorf("expected hourly series from the representative endpoint, got none")
			}
		})
	}
}
