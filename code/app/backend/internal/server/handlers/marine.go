package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// marineAPIURL is the Open-Meteo marine endpoint. The wave model is a global
// ocean model: for a point outside its water mask (far up an estuary, for
// example) it answers 200 with a null for every hour rather than an error.
const marineAPIURL = "https://marine-api.open-meteo.com/v1/marine"

// marineResponse decodes the marine forecast with pointer slices so that a JSON
// null stays distinguishable from a real 0.0 reading. Decoding these into plain
// []float64 silently turns "the model has no data here" into "the sea is flat",
// which is how a marina 25 nm up a strait ends up reporting dead calm.
type marineResponse struct {
	Daily struct {
		WaveHeightMax         []*float64 `json:"wave_height_max"`
		WaveDirectionDominant []*float64 `json:"wave_direction_dominant"`
		WavePeriodMax         []*float64 `json:"wave_period_max"`
	} `json:"daily"`
	Hourly struct {
		WaveHeight    []*float64 `json:"wave_height"`
		WavePeriod    []*float64 `json:"wave_period"`
		WaveDirection []*float64 `json:"wave_direction"`
	} `json:"hourly"`
}

// hasCoverage reports whether the wave model returned at least one real value.
// An all-null response means this position sits outside the model domain.
func (m *marineResponse) hasCoverage() bool {
	return anyNonNil(m.Daily.WaveHeightMax) || anyNonNil(m.Hourly.WaveHeight)
}

func anyNonNil(vals []*float64) bool {
	for _, v := range vals {
		if v != nil {
			return true
		}
	}
	return false
}

// firstValue returns the first non-null entry, scaled, and whether one existed.
func firstValue(vals []*float64, scale float64) (float64, bool) {
	for _, v := range vals {
		if v != nil {
			return *v * scale, true
		}
	}
	return 0, false
}

// toFloats flattens a pointer slice, substituting 0 for individual null hours.
// Only called once coverage is established, so these are gaps inside a covered
// series rather than a wholesale absence of data.
func toFloats(vals []*float64, scale float64) []float64 {
	if len(vals) == 0 {
		return nil
	}
	out := make([]float64, len(vals))
	for i, v := range vals {
		if v != nil {
			out[i] = *v * scale
		}
	}
	return out
}

// fetchMarineWaves retrieves the wave forecast for a single day. A nil result
// with a nil error means the request succeeded but the point has no wave-model
// coverage.
func fetchMarineWaves(ctx context.Context, lat, lng float64, date time.Time) (*marineResponse, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%v", lat))
	q.Set("longitude", fmt.Sprintf("%v", lng))
	q.Set("start_date", date.Format("2006-01-02"))
	q.Set("end_date", date.AddDate(0, 0, 1).Format("2006-01-02"))
	q.Set("daily", strings.Join([]string{"wave_height_max", "wave_direction_dominant", "wave_period_max"}, ","))
	q.Set("hourly", strings.Join([]string{"wave_height", "wave_period", "wave_direction"}, ","))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, marineAPIURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build marine request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marine request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marine API returned status %d", resp.StatusCode)
	}

	var out marineResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode marine response: %w", err)
	}

	if !out.hasCoverage() {
		return nil, nil
	}
	return &out, nil
}
