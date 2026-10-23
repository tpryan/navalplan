package tool

import (
	"fmt"
	"math"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const StateKeyPassageTimeline = "passage_timeline"

// TimelineRequest represents the parameters for the ComputePassageTimeline tool.
type TimelineRequest struct {
	FromLocation      string  `json:"from_location"`
	FromLatitude      float64 `json:"from_latitude"`
	FromLongitude     float64 `json:"from_longitude"`
	ToLocation        string  `json:"to_location"`
	ToLatitude        float64 `json:"to_latitude"`
	ToLongitude       float64 `json:"to_longitude"`
	Date              string  `json:"date"`
	PlannedDistanceNM float64 `json:"planned_distance_nm,omitempty"`
}

// DepartureWindowOption represents a single candidate departure time and its
// environmental/daylight outcomes at both the departure and arrival stops.
type DepartureWindowOption struct {
	DepartureTime       string  `json:"departure_time"`
	SpeedKts            float64 `json:"speed_kts"`
	EffectiveSpeedKts   float64 `json:"effective_speed_kts"`
	TransitHours        float64 `json:"transit_hours"`
	ETA                 string  `json:"eta"`
	ArrivesAfterSunset  bool    `json:"arrives_after_sunset"`
	MinutesBeforeSunset int     `json:"minutes_before_sunset"`
	DepartureTideState  string  `json:"departure_tide_state,omitempty"`
	ArrivalTideState    string  `json:"arrival_tide_state,omitempty"`
	WindKnots           float64 `json:"wind_knots,omitempty"`
	WindDirection       string  `json:"wind_direction,omitempty"`
	IsAdverseWind       bool    `json:"is_adverse_wind"`
	Notes               string  `json:"notes,omitempty"`
}

// TimelineResponse contains the aligned departure and arrival environmental timeline.
type TimelineResponse struct {
	FromLocation          string                  `json:"from_location"`
	ToLocation            string                  `json:"to_location"`
	Date                  string                  `json:"date"`
	DistanceNM            float64                 `json:"distance_nm"`
	BearingDeg            float64                 `json:"bearing_deg"`
	CardinalDirection     string                  `json:"cardinal_direction"`
	DepartureSunrise      string                  `json:"departure_sunrise,omitempty"`
	DepartureSunset       string                  `json:"departure_sunset,omitempty"`
	ArrivalSunrise        string                  `json:"arrival_sunrise,omitempty"`
	ArrivalSunset         string                  `json:"arrival_sunset,omitempty"`
	DepartureTidesSummary string                  `json:"departure_tides_summary,omitempty"`
	ArrivalTidesSummary   string                  `json:"arrival_tides_summary,omitempty"`
	RecommendedWindow     string                  `json:"recommended_window"`
	InteractionWarnings   []string                `json:"interaction_warnings,omitempty"`
	Windows               []DepartureWindowOption `json:"windows"`
	Digest                string                  `json:"digest"`
}

// TimelineProvider orchestrates weather, tide, and sun lookups for both ends of a leg.
type TimelineProvider struct {
	weather *WeatherProvider
	tides   *TideManager
	sunrise *SunriseProvider
}

func NewTimelineTool(weather *WeatherProvider, tides *TideManager, sunrise *SunriseProvider) (tool.Tool, *TimelineProvider, error) {
	tp := &TimelineProvider{
		weather: weather,
		tides:   tides,
		sunrise: sunrise,
	}
	t, err := functiontool.New(functiontool.Config{
		Name:        "ComputePassageTimeline",
		Description: "Aligns departure stop and arrival destination tides, sunrise/sunset, weather, and headwind penalties across candidate morning departure times (06:00-12:00) to detect tide-gated departures, arrival after sunset, and wind-against-tide interactions.",
	}, tp.ComputeTimeline)
	return t, tp, err
}

func (tp *TimelineProvider) Close() error {
	return nil
}

func (tp *TimelineProvider) ComputeTimeline(ctx agent.Context, req TimelineRequest) (TimelineResponse, error) {
	distNM := req.PlannedDistanceNM
	if distNM <= 0 {
		distNM = CalculateDistanceNM(req.FromLatitude, req.FromLongitude, req.ToLatitude, req.ToLongitude)
	}
	bearing := CalculateBearing(req.FromLatitude, req.FromLongitude, req.ToLatitude, req.ToLongitude)
	cardinal := DegreesToDirection(bearing)

	resp := TimelineResponse{
		FromLocation:      req.FromLocation,
		ToLocation:        req.ToLocation,
		Date:              req.Date,
		DistanceNM:        math.Round(distNM*10) / 10,
		BearingDeg:        math.Round(bearing*10) / 10,
		CardinalDirection: cardinal,
	}

	var depWeather WeatherResult
	if tp.weather != nil && (req.FromLatitude != 0 || req.FromLongitude != 0) {
		if w, err := tp.weather.GetWeatherForecast(ctx, WeatherArgs{
			Latitude:  req.FromLatitude,
			Longitude: req.FromLongitude,
			Date:      req.Date,
		}); err == nil {
			depWeather = w
		}
	}

	var depTides, arrTides TideResult
	if tp.tides != nil {
		if req.FromLatitude != 0 || req.FromLongitude != 0 {
			if t, err := tp.tides.GetTides(ctx, TideArgs{
				Latitude:  req.FromLatitude,
				Longitude: req.FromLongitude,
				Date:      req.Date,
			}); err == nil {
				depTides = t
				resp.DepartureTidesSummary = t.Digest
			}
		}
		if req.ToLatitude != 0 || req.ToLongitude != 0 {
			if t, err := tp.tides.GetTides(ctx, TideArgs{
				Latitude:  req.ToLatitude,
				Longitude: req.ToLongitude,
				Date:      req.Date,
			}); err == nil {
				arrTides = t
				resp.ArrivalTidesSummary = t.Digest
			}
		}
	}

	sunsetMinutes := 18 * 60
	if tp.sunrise != nil {
		if req.FromLatitude != 0 || req.FromLongitude != 0 {
			if s, err := tp.sunrise.GetSunriseSunset(ctx, SunriseArgs{
				Latitude:  req.FromLatitude,
				Longitude: req.FromLongitude,
				Date:      req.Date,
			}); err == nil {
				resp.DepartureSunrise = s.Sunrise
				resp.DepartureSunset = s.Sunset
			}
		}
		if req.ToLatitude != 0 || req.ToLongitude != 0 {
			if s, err := tp.sunrise.GetSunriseSunset(ctx, SunriseArgs{
				Latitude:  req.ToLatitude,
				Longitude: req.ToLongitude,
				Date:      req.Date,
			}); err == nil {
				resp.ArrivalSunrise = s.Sunrise
				resp.ArrivalSunset = s.Sunset
				if mins, ok := parseClockMinutes(s.Sunset); ok {
					sunsetMinutes = mins
				}
			}
		}
	}
	if resp.ArrivalSunset == "" && resp.DepartureSunset != "" {
		resp.ArrivalSunset = resp.DepartureSunset
		if mins, ok := parseClockMinutes(resp.DepartureSunset); ok {
			sunsetMinutes = mins
		}
	}

	candidateHours := []int{6, 8, 10, 12}
	const nominalSpeedKts = 5.0
	var bestWindow string

	for _, depHour := range candidateHours {
		depMins := depHour * 60
		depTimeStr := formatClockAMPM(depMins)

		windKts := depWeather.MaxWindKts
		if depHour < len(depWeather.HourlyWind) && depWeather.HourlyWind[depHour] > 0 {
			windKts = depWeather.HourlyWind[depHour]
		}
		windDir := depWeather.WindDirection
		if depHour < len(depWeather.HourlyWindDir) && depWeather.HourlyWindDir[depHour] != "" {
			windDir = depWeather.HourlyWindDir[depHour]
		}

		isAdverse := false
		effectiveSpeed := nominalSpeedKts
		if windDeg, ok := cardinalStringToDeg(windDir); ok && windKts >= 8 {
			diff := math.Mod(math.Abs(bearing-windDeg), 360.0)
			if diff > 180 {
				diff = 360 - diff
			}
			if diff <= 45 {
				isAdverse = true
				effectiveSpeed = nominalSpeedKts * 0.78
			}
		}

		transitHours := 0.0
		if distNM > 0 && effectiveSpeed > 0 {
			transitHours = distNM / effectiveSpeed
		}
		etaMins := depMins + int(math.Round(transitHours*60.0))
		etaStr := formatClockAMPM(etaMins % (24 * 60))
		marginBeforeSunset := sunsetMinutes - etaMins
		afterSunset := marginBeforeSunset < 0

		depTideDesc, depNearLow := describeTideNearHour(depTides.Tides, depHour)
		arrHour := (etaMins / 60) % 24
		arrTideDesc, arrNearLow := describeTideNearHour(arrTides.Tides, arrHour)

		var notes []string
		if depNearLow {
			notes = append(notes, "Departing near Low Tide (verify controlling channel/bar depth)")
		}
		if isAdverse {
			notes = append(notes, fmt.Sprintf("Headwind penalty applied (%.0f kt %s dead ahead)", windKts, windDir))
		}
		if afterSunset {
			notes = append(notes, fmt.Sprintf("Arrives %d min AFTER sunset (verify lighted aids/night entry)", -marginBeforeSunset))
		} else if marginBeforeSunset < 60 {
			notes = append(notes, fmt.Sprintf("Tight daylight margin (%d min before sunset)", marginBeforeSunset))
		}
		if arrNearLow {
			notes = append(notes, "Arrives near Low Tide at destination")
		}

		if bestWindow == "" && !depNearLow && !afterSunset && marginBeforeSunset >= 60 {
			bestWindow = fmt.Sprintf("%s (ETA %s, %d min before sunset)", depTimeStr, etaStr, marginBeforeSunset)
		}

		resp.Windows = append(resp.Windows, DepartureWindowOption{
			DepartureTime:       depTimeStr,
			SpeedKts:            nominalSpeedKts,
			EffectiveSpeedKts:   math.Round(effectiveSpeed*10) / 10,
			TransitHours:        math.Round(transitHours*10) / 10,
			ETA:                 etaStr,
			ArrivesAfterSunset:  afterSunset,
			MinutesBeforeSunset: marginBeforeSunset,
			DepartureTideState:  depTideDesc,
			ArrivalTideState:    arrTideDesc,
			WindKnots:           windKts,
			WindDirection:       windDir,
			IsAdverseWind:       isAdverse,
			Notes:               strings.Join(notes, "; "),
		})
	}

	if bestWindow == "" && len(resp.Windows) > 0 {
		w0 := resp.Windows[0]
		bestWindow = fmt.Sprintf("%s (ETA %s)", w0.DepartureTime, w0.ETA)
	}
	resp.RecommendedWindow = bestWindow

	for _, w := range resp.Windows {
		if strings.Contains(w.Notes, "Low Tide") && w.ArrivesAfterSunset {
			resp.InteractionWarnings = append(resp.InteractionWarnings,
				fmt.Sprintf("Tide-delayed departure around %s pushes arrival (%s) past sunset (%s); check Coast Pilot and NGA warnings for unlit buoys or extinguished lights at %s.", w.DepartureTime, w.ETA, resp.ArrivalSunset, req.ToLocation))
			break
		}
	}

	resp.Digest = fmt.Sprintf("Leg %s -> %s: %.1f NM at %.0f° (%s). Recommended departure: %s.",
		req.FromLocation, req.ToLocation, resp.DistanceNM, resp.BearingDeg, resp.CardinalDirection, resp.RecommendedWindow)

	setSessionState(ctx, StateKeyPassageTimeline, resp)
	return resp, nil
}

func describeTideNearHour(events []TideEvent, targetHour int) (string, bool) {
	if len(events) == 0 {
		return "", false
	}
	bestDiff := 999
	var closest TideEvent
	for _, e := range events {
		parts := strings.Fields(e.Time)
		clockPart := e.Time
		if len(parts) >= 2 {
			clockPart = parts[1]
		}
		hm := strings.Split(clockPart, ":")
		if len(hm) < 2 {
			continue
		}
		var h int
		_, _ = fmt.Sscanf(hm[0], "%d", &h)
		diff := int(math.Abs(float64(h - targetHour)))
		if diff < bestDiff {
			bestDiff = diff
			closest = e
		}
	}
	if bestDiff > 6 {
		return "", false
	}
	isLow := strings.EqualFold(closest.Type, "L") || strings.EqualFold(closest.Type, "Low")
	nearLow := isLow && bestDiff <= 1
	label := closest.Type
	if isLow {
		label = "Low"
	} else {
		label = "High"
	}
	return fmt.Sprintf("Nearest %s tide (%.1f ft) at %s", label, closest.Height, closest.Time), nearLow
}

func parseClockMinutes(clockStr string) (int, bool) {
	s := strings.TrimSpace(strings.ToUpper(clockStr))
	if len(s) >= 19 && strings.Contains(s, "T") {
		if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
			return t.Hour()*60 + t.Minute(), true
		}
	}
	if s == "" {
		return 0, false
	}
	t, err := time.Parse("03:04 PM", s)
	if err != nil {
		t, err = time.Parse("3:04 PM", s)
	}
	if err != nil {
		t, err = time.Parse("15:04", s)
	}
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func formatClockAMPM(totalMins int) string {
	h24 := (totalMins / 60) % 24
	m := totalMins % 60
	ampm := "AM"
	h12 := h24
	if h24 >= 12 {
		ampm = "PM"
		if h24 > 12 {
			h12 = h24 - 12
		}
	}
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%02d:%02d %s", h12, m, ampm)
}

func cardinalStringToDeg(dir string) (float64, bool) {
	d := strings.ToUpper(strings.TrimSpace(dir))
	m := map[string]float64{
		"N": 0, "NNE": 22.5, "NE": 45, "ENE": 67.5,
		"E": 90, "ESE": 112.5, "SE": 135, "SSE": 157.5,
		"S": 180, "SSW": 202.5, "SW": 225, "WSW": 247.5,
		"W": 270, "WNW": 292.5, "NW": 315, "NNW": 337.5,
	}
	deg, ok := m[d]
	return deg, ok
}
