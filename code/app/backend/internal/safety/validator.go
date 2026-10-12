// Package safety performs deterministic, numeric maritime-safety threshold
// checks that the lookout LLM previously did by hand. Moving these simple
// arithmetic comparisons into Go makes the lookout audit faster, cheaper, and
// consistent, letting the model focus on qualitative hazard synthesis.
//
// Adverse-wind / headwind is intentionally NOT handled here: it is already
// computed deterministically by the researcher's CalculateHeading tool.
package safety

import (
	"encoding/json"
	"fmt"

	"app/internal/model"
)

// Alert mirrors the structured alert objects the lookout agent emits.
type Alert struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
	Action   string `json:"action"`
	Icon     string `json:"icon"`
}

type tideEvent struct {
	HeightFt float64 `json:"height_ft"`
	Type     string  `json:"type"`
	Time     string  `json:"time"`
}

type tideData struct {
	Events []tideEvent `json:"events"`
}

// Thresholds (knots / feet / seconds), mirroring the lookout safety rules.
const (
	windDangerKt  = 33.0
	windWarningKt = 18.0
	waveDangerFt  = 13.0
	waveWarningFt = 7.0
	// Steep, dangerous seas: tall waves on a short period.
	waveSteepHeightFt   = 10.0
	waveSteepPeriodSecs = 10.0
	tideRangeWarningFt  = 10.0
)

// Evaluate runs the deterministic numeric threshold checks over the forecast and
// returns the alerts they imply. Peak values across the 48h hourly forecast are
// used (falling back to the summary values) so a brief spike is not missed.
// Malformed or empty inputs yield no alerts rather than an error.
func Evaluate(weatherJSON, tidesJSON []byte) []Alert {
	var alerts []Alert

	var w model.WeatherSummary
	if len(weatherJSON) > 0 {
		_ = json.Unmarshal(weatherJSON, &w)
	}

	// Peak sustained wind (knots). Gusts are excluded: the rule is on wind speed.
	peakWind := w.WindSpeedKt
	for _, v := range w.HourlyWind {
		if v > peakWind {
			peakWind = v
		}
	}
	switch {
	case peakWind > windDangerKt:
		alerts = append(alerts, Alert{
			Severity: "danger", Category: "weather", Icon: "storm",
			Message: fmt.Sprintf("Forecast wind peaks at %.0f knots, exceeding the %.0f-knot danger threshold.", peakWind, windDangerKt),
			Action:  "Postpone departure and secure the vessel until winds subside.",
		})
	case peakWind >= windWarningKt:
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "air",
			Message: fmt.Sprintf("Forecast wind peaks at %.0f knots (%.0f-%.0f knot warning range).", peakWind, windWarningKt, windDangerKt),
			Action:  "Reef early and expect a strenuous, uncomfortable passage.",
		})
	}

	// Peak wave height (feet).
	peakWave := w.WaveHeightFt
	for _, v := range w.HourlyWaveHeight {
		if v > peakWave {
			peakWave = v
		}
	}
	switch {
	case peakWave > waveDangerFt:
		alerts = append(alerts, Alert{
			Severity: "danger", Category: "weather", Icon: "waves",
			Message: fmt.Sprintf("Forecast wave height peaks at %.0f ft, exceeding the %.0f-ft danger threshold.", peakWave, waveDangerFt),
			Action:  "Avoid open-water passage until seas moderate.",
		})
	case peakWave >= waveWarningFt:
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "waves",
			Message: fmt.Sprintf("Forecast wave height peaks at %.0f ft (%.0f-%.0f ft warning range).", peakWave, waveWarningFt, waveDangerFt),
			Action:  "Expect rough, tiring conditions; stow gear and brief the crew.",
		})
	}

	// Steep seas: tall waves on a short period, checked pairwise by hour then
	// falling back to the summary pair.
	if steepSeas(w) {
		alerts = append(alerts, Alert{
			Severity: "danger", Category: "weather", Icon: "waves",
			Message: fmt.Sprintf("Short wave period (<= %.0fs) with wave height over %.0f ft indicates steep, dangerous seas.", waveSteepPeriodSecs, waveSteepHeightFt),
			Action:  "Avoid the passage; steep breaking seas risk broaching and knockdown.",
		})
	}

	// Tidal range.
	if r, ok := tidalRange(tidesJSON); ok && r > tideRangeWarningFt {
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "tides", Icon: "water",
			Message: fmt.Sprintf("Tidal range is %.1f ft (exceeds %.0f ft), producing strong tidal streams.", r, tideRangeWarningFt),
			Action:  "Time departures and chokepoint transits for favorable or slack tide.",
		})
	}

	return alerts
}

// steepSeas reports whether any forecast hour (or the summary) pairs a wave
// height above waveSteepHeightFt with a period at or below waveSteepPeriodSecs.
func steepSeas(w model.WeatherSummary) bool {
	n := len(w.HourlyWaveHeight)
	if n > len(w.HourlyWavePeriod) {
		n = len(w.HourlyWavePeriod)
	}
	for i := 0; i < n; i++ {
		p := w.HourlyWavePeriod[i]
		if w.HourlyWaveHeight[i] > waveSteepHeightFt && p > 0 && p <= waveSteepPeriodSecs {
			return true
		}
	}
	return w.WaveHeightFt > waveSteepHeightFt && w.WavePeriod > 0 && w.WavePeriod <= waveSteepPeriodSecs
}

// tidalRange returns the max-minus-min tide-event height, and whether enough
// data was present to compute it.
func tidalRange(tidesJSON []byte) (float64, bool) {
	if len(tidesJSON) == 0 {
		return 0, false
	}
	var t tideData
	if err := json.Unmarshal(tidesJSON, &t); err != nil || len(t.Events) == 0 {
		return 0, false
	}
	min, max := t.Events[0].HeightFt, t.Events[0].HeightFt
	for _, e := range t.Events[1:] {
		if e.HeightFt < min {
			min = e.HeightFt
		}
		if e.HeightFt > max {
			max = e.HeightFt
		}
	}
	return max - min, true
}
