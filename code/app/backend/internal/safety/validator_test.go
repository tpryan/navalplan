package safety

import "testing"

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
