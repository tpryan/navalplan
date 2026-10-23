// Package safety performs deterministic, numeric maritime-safety threshold
// checks that the lookout LLM previously did by hand. Moving these simple
// arithmetic comparisons into Go makes the lookout audit faster, cheaper, and
// consistent, letting the model focus on qualitative hazard synthesis.
package safety

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

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

// RouteSegment represents a single leg or segment of a planned route for
// deterministic adverse-wind / heading evaluation.
type RouteSegment struct {
	Name       string  `json:"name,omitempty"`
	BearingDeg float64 `json:"bearing_deg"`
	DistanceNM float64 `json:"distance_nm,omitempty"`
}

type tideEvent struct {
	HeightFt float64 `json:"height_ft"`
	Type     string  `json:"type"`
	Time     string  `json:"time"`
}

type tideData struct {
	Events []tideEvent `json:"events"`
}

// Thresholds (knots / feet / seconds / inches / degrees), mirroring the lookout safety rules.
const (
	windDangerKt  = 33.0
	windWarningKt = 18.0
	waveDangerFt  = 13.0
	waveWarningFt = 7.0
	// Steep, dangerous seas: tall waves on a short period.
	waveSteepHeightFt   = 10.0
	waveSteepPeriodSecs = 10.0
	tideRangeWarningFt  = 10.0

	precipWarningInHr     = 0.1
	tempDropWarningF      = 15.0
	windShiftWarningDeg   = 90.0
	windShiftMinWindKt    = 10.0
	choppyOppositionMin   = 135.0
	choppyOppositionMax   = 225.0
	choppyMinWindKt       = 15.0
	choppyMinWaveFt       = 3.0
	adverseWindAngleDeg   = 45.0
	adverseWindHeavyKt    = 15.0
	adverseWindModerateKt = 5.0
)

// Evaluate runs the deterministic numeric and time-series threshold checks over
// the forecast and returns the alerts they imply. Peak values across the 48h
// hourly forecast are used (falling back to the summary values) so a brief
// spike is not missed. Malformed or empty inputs yield no alerts rather than an error.
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

	// Sudden 3-hour deterioration (Safe -> Danger within 3 hours).
	if msg, ok := suddenDeterioration(w); ok {
		alerts = append(alerts, Alert{
			Severity: "danger", Category: "weather", Icon: "trending_up",
			Message: msg,
			Action:  "Monitor barometer and radar closely; reach sheltered harbor before the front arrives.",
		})
	}

	// Precipitation (> 0.1 in/hr or snow).
	if msg, ok := precipitationWarning(w); ok {
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "storm",
			Message: msg,
			Action:  "Prepare foul-weather gear, radar/AIS watch, and reduced-visibility navigation.",
		})
	}

	// Serious weather changes (wind speed doubling within 6h or temperature drop > 15°F).
	if msg, ok := seriousWeatherShift(w); ok {
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "thermostat",
			Message: msg,
			Action:  "Anticipate squalls or frontal passage; reef before the shift arrives.",
		})
	}

	// Wind direction shift (> 90° while wind > 10 kts).
	if msg, ok := windDirectionShift(w); ok {
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "air",
			Message: msg,
			Action:  "Avoid lee-shore anchorages and prepare for a major shift in tack or shelter exposure.",
		})
	}

	// Wind vs. wave opposition (diametrically opposed within 45° of 180° when wind > 15 kt and waves >= 3 ft).
	if msg, ok := windWaveOpposition(w); ok {
		alerts = append(alerts, Alert{
			Severity: "warning", Category: "weather", Icon: "waves",
			Message: msg,
			Action:  "Expect steep, confused, choppy seas; reduce sail and adjust course for sea comfort.",
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

// EvaluateHeadings evaluates direct leg and planned route segment bearings against
// the prevailing forecast wind, producing deterministic navigation heading alerts.
func EvaluateHeadings(weatherJSON []byte, directBearingDeg, directDistNM float64, hasDirectLeg bool, nextStopName string, segments []RouteSegment) []Alert {
	var w model.WeatherSummary
	if len(weatherJSON) > 0 {
		_ = json.Unmarshal(weatherJSON, &w)
	}
	windSpeed := w.WindSpeedKt
	if windSpeed <= 0 && len(w.HourlyWind) > 0 {
		for _, v := range w.HourlyWind {
			if v > windSpeed {
				windSpeed = v
			}
		}
	}
	windDirDeg, ok := cardinalToDegrees(w.WindDirection)
	if !ok && len(w.HourlyWindDir) > 0 {
		for _, d := range w.HourlyWindDir {
			if deg, valid := cardinalToDegrees(d); valid {
				windDirDeg = deg
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil
	}

	var alerts []Alert
	if hasDirectLeg && directDistNM > 0 {
		diff := angularDiff(directBearingDeg, windDirDeg)
		destLabel := nextStopName
		if destLabel == "" {
			destLabel = "next stop"
		}
		if diff <= adverseWindAngleDeg && windSpeed > adverseWindHeavyKt {
			alerts = append(alerts, Alert{
				Severity: "warning", Category: "navigation", Icon: "air",
				Message: fmt.Sprintf("Direct transit course (%.0f°, %.1f NM to %s) faces %.0f kt headwinds from %s (within %.0f° of bow), requiring strenuous upwind beating.", directBearingDeg, directDistNM, destLabel, windSpeed, w.WindDirection, diff),
				Action:  "Allow 25-35%% extra transit time for tacking or motor-sailing into the wind.",
			})
		} else if diff <= adverseWindAngleDeg && windSpeed >= adverseWindModerateKt {
			alerts = append(alerts, Alert{
				Severity: "info", Category: "navigation", Icon: "air",
				Message: fmt.Sprintf("Transit course (%.0f°, %.1f NM to %s) faces moderate %.0f kt headwinds from %s, requiring upwind tacking.", directBearingDeg, directDistNM, destLabel, windSpeed, w.WindDirection),
				Action:  "Plan tacking angles or motor-assisted progress to hold schedule.",
			})
		}
	}

	for idx, seg := range segments {
		diff := angularDiff(seg.BearingDeg, windDirDeg)
		if diff > adverseWindAngleDeg || windSpeed < adverseWindModerateKt {
			continue
		}
		segLabel := seg.Name
		if segLabel == "" {
			segLabel = fmt.Sprintf("Segment %d", idx+1)
		}
		if windSpeed > adverseWindHeavyKt {
			alerts = append(alerts, Alert{
				Severity: "warning", Category: "navigation", Icon: "route",
				Message: fmt.Sprintf("Planned route %s (heading %.0f°) turns directly into %.0f kt headwinds from %s.", segLabel, seg.BearingDeg, windSpeed, w.WindDirection),
				Action:  "Expect slow upwind progress on this leg segment; verify sea room for tacking.",
			})
		} else {
			alerts = append(alerts, Alert{
				Severity: "info", Category: "navigation", Icon: "route",
				Message: fmt.Sprintf("Planned route %s (heading %.0f°) faces moderate %.0f kt headwinds from %s.", segLabel, seg.BearingDeg, windSpeed, w.WindDirection),
				Action:  "Account for tacking angles along this planned route segment.",
			})
		}
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

// suddenDeterioration checks if wind or wave conditions jump from Safe (<18 kt / <7 ft)
// to Danger (>33 kt / >13 ft) within any 3-hour sliding window.
func suddenDeterioration(w model.WeatherSummary) (string, bool) {
	for i := 0; i < len(w.HourlyWind); i++ {
		if w.HourlyWind[i] >= windWarningKt {
			continue
		}
		for j := i + 1; j <= i+3 && j < len(w.HourlyWind); j++ {
			if w.HourlyWind[j] > windDangerKt {
				return fmt.Sprintf("Sudden severe wind deterioration: winds escalate from %.0f knots (safe) to %.0f knots (danger) within %d hours.", w.HourlyWind[i], w.HourlyWind[j], j-i), true
			}
		}
	}
	for i := 0; i < len(w.HourlyWaveHeight); i++ {
		if w.HourlyWaveHeight[i] >= waveWarningFt {
			continue
		}
		for j := i + 1; j <= i+3 && j < len(w.HourlyWaveHeight); j++ {
			if w.HourlyWaveHeight[j] > waveDangerFt {
				return fmt.Sprintf("Sudden severe sea-state deterioration: waves build from %.1f ft (safe) to %.1f ft (danger) within %d hours.", w.HourlyWaveHeight[i], w.HourlyWaveHeight[j], j-i), true
			}
		}
	}
	return "", false
}

// precipitationWarning checks for heavy rain (> 0.1 in/hr) or snow in conditions.
func precipitationWarning(w model.WeatherSummary) (string, bool) {
	condLower := strings.ToLower(w.Condition + " " + w.Summary)
	if strings.Contains(condLower, "snow") || strings.Contains(condLower, "blizzard") || strings.Contains(condLower, "flurr") {
		return "Snow is forecast, bringing freezing deck hazards and severely reduced visibility.", true
	}
	for _, c := range w.HourlyConditions {
		cl := strings.ToLower(c)
		if strings.Contains(cl, "snow") || strings.Contains(cl, "blizzard") {
			return "Snow is forecast during the passage window, reducing visibility and creating slick decks.", true
		}
	}
	var maxPrecip float64
	for _, p := range w.HourlyPrecip {
		if p > maxPrecip {
			maxPrecip = p
		}
	}
	if maxPrecip > precipWarningInHr {
		return fmt.Sprintf("Significant precipitation forecast (peaks at %.2f in/hr, exceeding %.1f in/hr), degrading visibility.", maxPrecip, precipWarningInHr), true
	}
	return "", false
}

// seriousWeatherShift checks for wind speed doubling (to >= 16 kts) within 6 hours
// or a temperature drop exceeding 15°F.
func seriousWeatherShift(w model.WeatherSummary) (string, bool) {
	for i := 0; i < len(w.HourlyWind); i++ {
		base := w.HourlyWind[i]
		if base < 6.0 {
			continue
		}
		for j := i + 1; j <= i+6 && j < len(w.HourlyWind); j++ {
			if w.HourlyWind[j] >= base*2.0 && w.HourlyWind[j] >= 16.0 {
				return fmt.Sprintf("Wind speed more than doubles from %.0f knots to %.0f knots within %d hours.", base, w.HourlyWind[j], j-i), true
			}
		}
	}
	if len(w.HourlyTemp) > 1 {
		maxPrior := w.HourlyTemp[0]
		var maxDrop float64
		for _, t := range w.HourlyTemp[1:] {
			if maxPrior-t > maxDrop {
				maxDrop = maxPrior - t
			}
			if t > maxPrior {
				maxPrior = t
			}
		}
		if maxDrop > tempDropWarningF {
			return fmt.Sprintf("Sharp temperature drop of %.0f°F forecast during the period, signaling a strong frontal passage.", maxDrop), true
		}
	}
	return "", false
}

// windDirectionShift checks for a >90° wind direction change within a 6-hour window
// when wind speed exceeds 10 knots.
func windDirectionShift(w model.WeatherSummary) (string, bool) {
	n := len(w.HourlyWindDir)
	if n == 0 {
		return "", false
	}
	for i := 0; i < n; i++ {
		deg1, ok1 := cardinalToDegrees(w.HourlyWindDir[i])
		if !ok1 {
			continue
		}
		spd1 := w.WindSpeedKt
		if i < len(w.HourlyWind) {
			spd1 = w.HourlyWind[i]
		}
		for j := i + 1; j <= i+6 && j < n; j++ {
			deg2, ok2 := cardinalToDegrees(w.HourlyWindDir[j])
			if !ok2 {
				continue
			}
			spd2 := w.WindSpeedKt
			if j < len(w.HourlyWind) {
				spd2 = w.HourlyWind[j]
			}
			if spd1 <= windShiftMinWindKt && spd2 <= windShiftMinWindKt {
				continue
			}
			diff := angularDiff(deg1, deg2)
			if diff > windShiftWarningDeg {
				return fmt.Sprintf("Major wind shift of %.0f° (%s to %s) with winds over %.0f knots.", diff, w.HourlyWindDir[i], w.HourlyWindDir[j], math.Max(spd1, spd2)), true
			}
		}
	}
	return "", false
}

// windWaveOpposition checks whether wind direction and wave direction are opposed
// (135° to 225° difference) while wind > 15 kts and wave height >= 3 ft.
func windWaveOpposition(w model.WeatherSummary) (string, bool) {
	n := len(w.HourlyWindDir)
	if len(w.HourlyWaveDir) < n {
		n = len(w.HourlyWaveDir)
	}
	for i := 0; i < n; i++ {
		windDeg, ok := cardinalToDegrees(w.HourlyWindDir[i])
		if !ok {
			continue
		}
		windKt := w.WindSpeedKt
		if i < len(w.HourlyWind) {
			windKt = w.HourlyWind[i]
		}
		waveFt := w.WaveHeightFt
		if i < len(w.HourlyWaveHeight) {
			waveFt = w.HourlyWaveHeight[i]
		}
		if windKt <= choppyMinWindKt || waveFt < choppyMinWaveFt {
			continue
		}
		diff := angularDiff(windDeg, w.HourlyWaveDir[i])
		if diff >= choppyOppositionMin && diff <= choppyOppositionMax {
			return fmt.Sprintf("Opposing wind (%s at %.0f kts) and wave train (%.0f° at %.1f ft) will generate steep, choppy seas.", w.HourlyWindDir[i], windKt, w.HourlyWaveDir[i], waveFt), true
		}
	}
	if w.WindSpeedKt > choppyMinWindKt && w.WaveHeightFt >= choppyMinWaveFt && w.WaveDirection > 0 {
		if windDeg, ok := cardinalToDegrees(w.WindDirection); ok {
			diff := angularDiff(windDeg, w.WaveDirection)
			if diff >= choppyOppositionMin && diff <= choppyOppositionMax {
				return fmt.Sprintf("Opposing wind (%s at %.0f kts) and swell direction (%.0f°) will produce steep, choppy seas.", w.WindDirection, w.WindSpeedKt, w.WaveDirection), true
			}
		}
	}
	return "", false
}

func cardinalToDegrees(dir string) (float64, bool) {
	d := strings.ToUpper(strings.TrimSpace(dir))
	if d == "" {
		return 0, false
	}
	cardinals := map[string]float64{
		"N": 0, "NNE": 22.5, "NE": 45, "ENE": 67.5,
		"E": 90, "ESE": 112.5, "SE": 135, "SSE": 157.5,
		"S": 180, "SSW": 202.5, "SW": 225, "WSW": 247.5,
		"W": 270, "WNW": 292.5, "NW": 315, "NNW": 337.5,
	}
	if deg, ok := cardinals[d]; ok {
		return deg, true
	}
	return 0, false
}

func angularDiff(a, b float64) float64 {
	diff := math.Mod(math.Abs(a-b), 360.0)
	if diff > 180.0 {
		diff = 360.0 - diff
	}
	return diff
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
