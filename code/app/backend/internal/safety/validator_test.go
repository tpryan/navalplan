package safety

import (
	"strings"
	"testing"
)

// alertsBy indexes alerts by severity for convenient assertions.
func sevSet(alerts []Alert) map[string]int {
	m := map[string]int{}
	for _, a := range alerts {
		m[a.Severity]++
	}
	return m
}

func TestEvaluate_HeavyWeather(t *testing.T) {
	// Wind 36 kt (> 33 danger), wave 14 ft (> 13 danger).
	weather := []byte(`{"wind_speed_kt":36,"wave_height_ft":14,"hourly_wind":[30,36,28],"hourly_wave_height":[12,14,10]}`)
	got := Evaluate(weather, nil)
	if len(got) < 2 {
		t.Fatalf("expected at least wind+wave danger alerts, got %d: %+v", len(got), got)
	}
	if sevSet(got)["danger"] < 2 {
		t.Errorf("expected 2 danger alerts (wind+wave), got %+v", got)
	}
}

func TestEvaluate_ModerateWarnings(t *testing.T) {
	// Wind 20 kt (18-33 warning), wave 8 ft (7-13 warning).
	weather := []byte(`{"wind_speed_kt":20,"wave_height_ft":8}`)
	got := Evaluate(weather, nil)
	if sevSet(got)["warning"] != 2 {
		t.Errorf("expected 2 warnings (wind+wave), got %+v", got)
	}
	if sevSet(got)["danger"] != 0 {
		t.Errorf("expected no danger alerts, got %+v", got)
	}
}

func TestEvaluate_Calm(t *testing.T) {
	weather := []byte(`{"wind_speed_kt":8,"wave_height_ft":2}`)
	tides := []byte(`{"events":[{"height_ft":1.0},{"height_ft":4.0}]}`)
	got := Evaluate(weather, tides)
	if len(got) != 0 {
		t.Errorf("expected no alerts for calm conditions, got %+v", got)
	}
}

func TestEvaluate_SteepSeas(t *testing.T) {
	// Wave height 11 ft on a 9s period -> steep-seas danger (plus wave warning).
	weather := []byte(`{"wind_speed_kt":10,"hourly_wave_height":[8,11],"hourly_wave_period":[12,9]}`)
	got := Evaluate(weather, nil)
	if sevSet(got)["danger"] < 1 {
		t.Errorf("expected a steep-seas danger alert, got %+v", got)
	}
}

func TestEvaluate_TidalRange(t *testing.T) {
	// Range 12.2 ft (> 10 warning).
	tides := []byte(`{"events":[{"height_ft":-0.2,"type":"L"},{"height_ft":12.0,"type":"H"}]}`)
	got := Evaluate(nil, tides)
	if sevSet(got)["warning"] != 1 {
		t.Fatalf("expected 1 tidal-range warning, got %+v", got)
	}
	if got[0].Category != "tides" {
		t.Errorf("expected tides category, got %q", got[0].Category)
	}
}

func TestEvaluate_EmptyInputs(t *testing.T) {
	if got := Evaluate(nil, nil); len(got) != 0 {
		t.Errorf("expected no alerts for empty inputs, got %+v", got)
	}
	if got := Evaluate([]byte(`not json`), []byte(`also not`)); len(got) != 0 {
		t.Errorf("expected no alerts for malformed inputs, got %+v", got)
	}
}

func TestEvaluate_TimeSeriesAndShifts(t *testing.T) {
	tests := []struct {
		name         string
		weatherJSON  string
		wantSeverity string
		wantSubstr   string
	}{
		{
			name:         "sudden 3h wind deterioration from safe to danger",
			weatherJSON:  `{"wind_speed_kt":12,"hourly_wind":[12,15,35]}`,
			wantSeverity: "danger",
			wantSubstr:   "Sudden severe wind deterioration",
		},
		{
			name:         "heavy precipitation over 0.1 in/hr",
			weatherJSON:  `{"wind_speed_kt":10,"hourly_precip":[0.0,0.25,0.05]}`,
			wantSeverity: "warning",
			wantSubstr:   "Significant precipitation",
		},
		{
			name:         "snow condition forecast",
			weatherJSON:  `{"wind_speed_kt":10,"condition":"Light Snow"}`,
			wantSeverity: "warning",
			wantSubstr:   "Snow is forecast",
		},
		{
			name:         "temperature drop exceeding 15F",
			weatherJSON:  `{"wind_speed_kt":10,"hourly_temp":[72,68,54]}`,
			wantSeverity: "warning",
			wantSubstr:   "temperature drop",
		},
		{
			name:         "wind direction shift over 90 deg above 10 kts",
			weatherJSON:  `{"wind_speed_kt":14,"hourly_wind":[14,15],"hourly_wind_dir":["SW","NE"]}`,
			wantSeverity: "warning",
			wantSubstr:   "Major wind shift",
		},
		{
			name:         "opposing wind and waves choppy seas",
			weatherJSON:  `{"wind_speed_kt":16,"wave_height_ft":4,"hourly_wind":[16],"hourly_wind_dir":["N"],"hourly_wave_height":[4],"hourly_wave_dir":[180]}`,
			wantSeverity: "warning",
			wantSubstr:   "Opposing wind",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			alerts := Evaluate([]byte(tc.weatherJSON), nil)
			found := false
			for _, a := range alerts {
				if a.Severity == tc.wantSeverity && strings.Contains(a.Message, tc.wantSubstr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected alert with severity=%q containing %q, got %+v", tc.wantSeverity, tc.wantSubstr, alerts)
			}
		})
	}
}

func TestEvaluateHeadings(t *testing.T) {
	weather := []byte(`{"wind_speed_kt":20,"wind_direction":"SW"}`)
	segments := []RouteSegment{
		{Name: "Leg 1 (Channel)", BearingDeg: 135, DistanceNM: 4.0},
		{Name: "Leg 2 (Sound)", BearingDeg: 220, DistanceNM: 8.0},
	}
	alerts := EvaluateHeadings(weather, 225, 12.0, true, "Cuttyhunk", segments)
	if len(alerts) != 2 {
		t.Fatalf("expected 2 heading alerts (direct leg + Leg 2), got %d: %+v", len(alerts), alerts)
	}
	if alerts[0].Severity != "warning" || alerts[0].Category != "navigation" {
		t.Errorf("unexpected direct leg alert: %+v", alerts[0])
	}
	if !strings.Contains(alerts[1].Message, "Leg 2 (Sound)") {
		t.Errorf("expected segment name in alert message, got %q", alerts[1].Message)
	}
}
