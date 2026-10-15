package gpx

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
)

const (
	EarthRadiusNM            = 3440.065
	MPS2Knots                = 1.943844 // 1 m/s in knots (3600 / 1852)
	MaxPlausibleSpeedKts     = 45.0     // filter out GPS jitter/glitches
	MaxAccelerationKtsPerSec = 2.0      // max plausible boat speed change rate
	MaxGlitchWindowSec       = 60.0     // time window for acceleration check
	SustainedSpeedWindowSec  = 60.0     // max speed is measured over at least this span to suppress GPS jitter
	maxTrackDistanceNM       = 999999.99
)

// ParseGPX parses raw GPX XML and produces one or more processed ParsedTrack models.
func ParseGPX(data []byte, preferredKind string) ([]ParsedTrack, error) {
	cleanData, glitches, err := FilterGPX(data)
	if err != nil {
		slog.Warn("GPX glitch filter returned error; proceeding with raw GPX", "glitches_detected", glitches, "error", err)
	} else if glitches > 0 {
		data = cleanData
	}

	var g GPX
	if err := xml.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("failed to parse GPX XML: %w", err)
	}

	var tracks []ParsedTrack

	// 1. Process routes (<rte>) as planned tracks
	for i, r := range g.Routes {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = fmt.Sprintf("Route %d", i+1)
		}
		// Route point <time> values are waypoint metadata (often creation
		// time, in arbitrary order), not travel times, so they are dropped.
		var pts []Point
		for _, pt := range r.RoutePoints {
			pts = append(pts, Point{
				Lat: pt.Lat,
				Lng: pt.Lon,
				Ele: pt.Ele,
			})
		}
		if len(pts) > 0 {
			kind := "planned"
			if preferredKind != "" {
				kind = preferredKind
			}
			track := buildParsedTrack(name, kind, pts)
			tracks = append(tracks, track)
		}
	}

	// 2. Process tracks (<trk>) as recorded tracks
	for i, t := range g.Tracks {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = fmt.Sprintf("Track %d", i+1)
		}
		var pts []Point
		for _, seg := range t.Segments {
			for _, pt := range seg.TrackPoints {
				var spdKts *float64
				if pt.Speed != nil {
					k := *pt.Speed * MPS2Knots
					spdKts = &k
				}
				pts = append(pts, Point{
					Lat:       pt.Lat,
					Lng:       pt.Lon,
					Ele:       pt.Ele,
					Time:      pt.Time,
					SpeedKts:  spdKts,
					CourseDeg: pt.Course,
				})
			}
		}
		if len(pts) > 0 {
			kind := "recorded"
			if preferredKind != "" {
				kind = preferredKind
			}
			track := buildParsedTrack(name, kind, pts)
			tracks = append(tracks, track)
		}
	}

	// 3. Fallback: if only waypoints are present, assemble them into a planned route
	if len(tracks) == 0 && len(g.Waypoints) > 0 {
		name := "Waypoints Route"
		if g.Metadata != nil && g.Metadata.Name != "" {
			name = g.Metadata.Name
		}
		var pts []Point
		for _, wpt := range g.Waypoints {
			pts = append(pts, Point{
				Lat:  wpt.Lat,
				Lng:  wpt.Lon,
				Ele:  wpt.Ele,
				Time: wpt.Time,
			})
		}
		kind := "planned"
		if preferredKind != "" {
			kind = preferredKind
		}
		tracks = append(tracks, buildParsedTrack(name, kind, pts))
	}

	if len(tracks) == 0 {
		return nil, fmt.Errorf("GPX file contains no routes, tracks, or waypoints")
	}

	return tracks, nil
}

type trackMetrics struct {
	totalDist        float64
	maxSpeed         float64
	avgSpeed         float64
	durationInterval string
	startTime        *time.Time
	endTime          *time.Time
}

func buildParsedTrack(name, kind string, points []Point) ParsedTrack {
	points, _ = FilterTrackPoints(points)
	hasReportedSpeed := hasAnyReportedSpeed(points)
	m := computeTrackMetrics(kind, points, hasReportedSpeed)

	geoJSONBytes := BuildGeoJSON(name, kind, points, m.totalDist, m.startTime, m.endTime, m.maxSpeed, m.avgSpeed)
	simplifiedPoints := SimplifyRDP(points, DefaultEpsilonNM)
	simplifiedGeoJSONBytes := BuildGeoJSON(name, kind, simplifiedPoints, m.totalDist, m.startTime, m.endTime, m.maxSpeed, m.avgSpeed)

	return ParsedTrack{
		Name:              name,
		Kind:              kind,
		Points:            points,
		StartTime:         m.startTime,
		EndTime:           m.endTime,
		DistanceNM:        math.Round(m.totalDist*100) / 100,
		DurationInterval:  m.durationInterval,
		MaxSpeedKts:       math.Round(m.maxSpeed*10) / 10,
		AvgSpeedKts:       math.Round(m.avgSpeed*10) / 10,
		GeoJSON:           geoJSONBytes,
		SimplifiedGeoJSON: simplifiedGeoJSONBytes,
	}
}

func hasAnyReportedSpeed(points []Point) bool {
	for i := range points {
		if points[i].SpeedKts != nil {
			return true
		}
	}
	return false
}

func computeTrackMetrics(kind string, points []Point, hasReportedSpeed bool) trackMetrics {
	var m trackMetrics
	var speedSum float64
	var speedPointsCount int
	lastValidIdx := -1

	for i := range points {
		if points[i].Time != nil {
			if m.startTime == nil || points[i].Time.Before(*m.startTime) {
				m.startTime = points[i].Time
			}
			if m.endTime == nil || points[i].Time.After(*m.endTime) {
				m.endTime = points[i].Time
			}
		}

		if i > 0 {
			prev := points[i-1]
			dist := CalculateHaversineNM(prev.Lat, prev.Lng, points[i].Lat, points[i].Lng)
			m.totalDist += dist

			if points[i].CourseDeg == nil {
				bearing := CalculateBearing(prev.Lat, prev.Lng, points[i].Lat, points[i].Lng)
				points[i].CourseDeg = &bearing
			}

			if points[i].SpeedKts == nil && prev.Time != nil && points[i].Time != nil {
				dtHours := points[i].Time.Sub(*prev.Time).Hours()
				if dtHours > 0 {
					calcSpeed := dist / dtHours
					points[i].SpeedKts = &calcSpeed
				}
			}

			if points[i].SpeedKts != nil {
				spd := *points[i].SpeedKts
				if !isStepSpeedGlitch(points, lastValidIdx, i, spd) {
					lastValidIdx = i
					if spd > m.maxSpeed {
						m.maxSpeed = spd
					}
					speedSum += spd
					speedPointsCount++
				}
			}
		} else if points[0].SpeedKts != nil && *points[0].SpeedKts <= MaxPlausibleSpeedKts {
			lastValidIdx = 0
			if *points[0].SpeedKts > m.maxSpeed {
				m.maxSpeed = *points[0].SpeedKts
			}
			speedSum += *points[0].SpeedKts
			speedPointsCount++
		}
	}

	// Speeds derived from position deltas on short sample intervals amplify
	// GPS jitter (a 10 m error over 1 s reads as ~19 kts), so when the device
	// did not report SOG, the max is the highest speed sustained over
	// SustainedSpeedWindowSec.
	if !hasReportedSpeed {
		if sustained, ok := sustainedMaxSpeed(points, SustainedSpeedWindowSec); ok {
			m.maxSpeed = sustained
		}
	}

	if m.startTime != nil && m.endTime != nil {
		dur := m.endTime.Sub(*m.startTime)
		m.durationInterval = FormatPostgresInterval(dur)
		if durHours := dur.Hours(); durHours > 0 && m.totalDist > 0 {
			if calcAvg := m.totalDist / durHours; calcAvg <= MaxPlausibleSpeedKts {
				m.avgSpeed = calcAvg
			}
		}
	}
	if m.avgSpeed == 0 && speedPointsCount > 0 {
		if calcAvg := speedSum / float64(speedPointsCount); calcAvg <= MaxPlausibleSpeedKts {
			m.avgSpeed = calcAvg
		}
	}
	if kind == "planned" {
		m.maxSpeed = 0
		m.avgSpeed = 0
	}

	m.avgSpeed = sanitizeMetric(m.avgSpeed, MaxPlausibleSpeedKts)
	m.maxSpeed = sanitizeMetric(m.maxSpeed, MaxPlausibleSpeedKts)
	m.totalDist = sanitizeMetric(m.totalDist, maxTrackDistanceNM)
	return m
}

func isStepSpeedGlitch(points []Point, lastValidIdx, i int, spd float64) bool {
	if spd > MaxPlausibleSpeedKts {
		return true
	}
	if lastValidIdx >= 0 && points[lastValidIdx].SpeedKts != nil && points[lastValidIdx].Time != nil && points[i].Time != nil {
		dtSec := points[i].Time.Sub(*points[lastValidIdx].Time).Seconds()
		if dtSec > 0 && dtSec <= MaxGlitchWindowSec {
			deltaV := math.Abs(spd - *points[lastValidIdx].SpeedKts)
			if deltaV/dtSec > MaxAccelerationKtsPerSec {
				return true
			}
		}
	}
	return false
}

func sanitizeMetric(val, maxAllowed float64) float64 {
	if val < 0 || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	if val > maxAllowed {
		if maxAllowed == MaxPlausibleSpeedKts {
			return 0
		}
		return maxAllowed
	}
	return val
}

// BuildGeoJSON constructs a GeoJSON Feature string representation for a set of points.
func BuildGeoJSON(name, kind string, points []Point, distNM float64, start, end *time.Time, maxSpeed, avgSpeed float64) []byte {
	coords := make([][]float64, len(points))
	ptsData := make([]map[string]interface{}, len(points))

	for i, pt := range points {
		coords[i] = []float64{pt.Lng, pt.Lat}
		item := map[string]interface{}{
			"lat": pt.Lat,
			"lng": pt.Lng,
		}
		if pt.Ele != nil {
			item["ele"] = *pt.Ele
		}
		if pt.Time != nil {
			item["time"] = pt.Time.Format(time.RFC3339)
		}
		if pt.SpeedKts != nil {
			item["speed_kts"] = math.Round(*pt.SpeedKts*10) / 10
		}
		if pt.CourseDeg != nil {
			item["course_deg"] = math.Round(*pt.CourseDeg)
		}
		ptsData[i] = item
	}

	props := map[string]interface{}{
		"name":          name,
		"kind":          kind,
		"distance_nm":   math.Round(distNM*100) / 100,
		"max_speed_kts": math.Round(maxSpeed*10) / 10,
		"avg_speed_kts": math.Round(avgSpeed*10) / 10,
		"points":        ptsData,
	}
	if start != nil {
		props["start_time"] = start.Format(time.RFC3339)
	}
	if end != nil {
		props["end_time"] = end.Format(time.RFC3339)
	}

	feat := GeoJSONFeature{
		Type: "Feature",
		Geometry: GeoJSONGeometry{
			Type:        "LineString",
			Coordinates: coords,
		},
		Properties: props,
	}

	data, _ := json.Marshal(feat)
	return data
}

// CalculateHaversineNM computes great-circle distance in nautical miles.
func CalculateHaversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	lat1Rad := lat1 * (math.Pi / 180.0)
	lat2Rad := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2.0)*math.Sin(dLat/2.0) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2.0)*math.Sin(dLon/2.0)
	if a > 1.0 {
		a = 1.0
	} else if a < 0.0 {
		a = 0.0
	}

	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return EarthRadiusNM * c
}

// CalculateBearing calculates the initial navigation course in degrees (0-360).
func CalculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	lat1Rad := lat1 * (math.Pi / 180.0)
	lat2Rad := lat2 * (math.Pi / 180.0)

	y := math.Sin(dLon) * math.Cos(lat2Rad)
	x := math.Cos(lat1Rad)*math.Sin(lat2Rad) - math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(dLon)

	bearing := math.Atan2(y, x) * (180.0 / math.Pi)
	if bearing < 0 {
		bearing += 360.0
	}
	return bearing
}

// FormatPostgresInterval converts a duration into a Postgres interval string (e.g. "HH:MM:SS").
func FormatPostgresInterval(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// sustainedMaxSpeed returns the highest speed (kts) held over any span of at
// least windowSec, measured as straight-line displacement over elapsed time.
// Displacement (not path length) is used so per-sample jitter does not
// accumulate. Returns false when no pair of timed points spans the window.
func sustainedMaxSpeed(points []Point, windowSec float64) (float64, bool) {
	var timed []Point
	for _, p := range points {
		if p.Time != nil {
			timed = append(timed, p)
		}
	}
	maxSpd := 0.0
	found := false
	j := 0
	for i := 1; i < len(timed); i++ {
		// Advance j to the latest point that is still at least windowSec before i.
		for j+1 < i && timed[i].Time.Sub(*timed[j+1].Time).Seconds() >= windowSec {
			j++
		}
		dtSec := timed[i].Time.Sub(*timed[j].Time).Seconds()
		if dtSec < windowSec {
			continue
		}
		spd := CalculateHaversineNM(timed[j].Lat, timed[j].Lng, timed[i].Lat, timed[i].Lng) / (dtSec / 3600.0)
		if spd > MaxPlausibleSpeedKts {
			continue
		}
		found = true
		if spd > maxSpd {
			maxSpd = spd
		}
	}
	return maxSpd, found
}
