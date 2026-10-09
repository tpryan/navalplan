package gpx

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	EarthRadiusNM            = 3440.065
	MaxPlausibleSpeedKts     = 45.0 // filter out GPS jitter/glitches
	MaxAccelerationKtsPerSec = 2.0  // max plausible boat speed change rate
	MaxGlitchWindowSec       = 30.0 // time window for acceleration check
)

// ParseGPX parses raw GPX XML and produces one or more processed ParsedTrack models.
func ParseGPX(data []byte, preferredKind string) ([]ParsedTrack, error) {
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
		var pts []Point
		for _, pt := range r.RoutePoints {
			pts = append(pts, Point{
				Lat:  pt.Lat,
				Lng:  pt.Lon,
				Ele:  pt.Ele,
				Time: pt.Time,
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
					// GPX 1.1 speed is in m/s; 1 m/s = 1.94384 knots
					k := *pt.Speed * 1.94384
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

func buildParsedTrack(name, kind string, points []Point) ParsedTrack {
	var totalDist float64
	var maxSpeed float64
	var speedSum float64
	var speedPointsCount int

	var startTime *time.Time
	var endTime *time.Time

	lastValidIdx := -1
	for i := range points {
		if points[i].Time != nil {
			if startTime == nil || points[i].Time.Before(*startTime) {
				startTime = points[i].Time
			}
			if endTime == nil || points[i].Time.After(*endTime) {
				endTime = points[i].Time
			}
		}

		if i > 0 {
			prev := points[i-1]
			dist := CalculateHaversineNM(prev.Lat, prev.Lng, points[i].Lat, points[i].Lng)
			totalDist += dist

			// Compute course if not provided
			if points[i].CourseDeg == nil {
				bearing := CalculateBearing(prev.Lat, prev.Lng, points[i].Lat, points[i].Lng)
				points[i].CourseDeg = &bearing
			}

			// Compute speed if not provided and timestamps exist
			if points[i].SpeedKts == nil && prev.Time != nil && points[i].Time != nil {
				dtHours := points[i].Time.Sub(*prev.Time).Hours()
				if dtHours > 0 {
					calcSpeed := dist / dtHours
					points[i].SpeedKts = &calcSpeed
				}
			}

			if points[i].SpeedKts != nil {
				spd := *points[i].SpeedKts
				isGlitch := false
				if spd > MaxPlausibleSpeedKts {
					isGlitch = true
				} else if lastValidIdx >= 0 && points[lastValidIdx].SpeedKts != nil && points[lastValidIdx].Time != nil && points[i].Time != nil {
					dtSec := points[i].Time.Sub(*points[lastValidIdx].Time).Seconds()
					if dtSec > 0 && dtSec <= MaxGlitchWindowSec {
						deltaV := math.Abs(spd - *points[lastValidIdx].SpeedKts)
						if deltaV/dtSec > MaxAccelerationKtsPerSec {
							isGlitch = true
						}
					}
				}

				if !isGlitch {
					lastValidIdx = i
					if spd > maxSpeed {
						maxSpeed = spd
					}
					speedSum += spd
					speedPointsCount++
				}
			}
		} else if points[0].SpeedKts != nil && *points[0].SpeedKts <= MaxPlausibleSpeedKts {
			lastValidIdx = 0
			if *points[0].SpeedKts > maxSpeed {
				maxSpeed = *points[0].SpeedKts
			}
			speedSum += *points[0].SpeedKts
			speedPointsCount++
		}
	}

	var durationInterval string
	var avgSpeed float64
	if startTime != nil && endTime != nil {
		dur := endTime.Sub(*startTime)
		durationInterval = FormatPostgresInterval(dur)
		durHours := dur.Hours()
		if durHours > 0 && totalDist > 0 {
			calcAvg := totalDist / durHours
			if calcAvg <= MaxPlausibleSpeedKts {
				avgSpeed = calcAvg
			}
		}
	}
	if avgSpeed == 0 && speedPointsCount > 0 {
		calcAvg := speedSum / float64(speedPointsCount)
		if calcAvg <= MaxPlausibleSpeedKts {
			avgSpeed = calcAvg
		}
	}
	if kind == "planned" {
		maxSpeed = 0
		avgSpeed = 0
	}
	if avgSpeed < 0 || avgSpeed > MaxPlausibleSpeedKts || math.IsNaN(avgSpeed) || math.IsInf(avgSpeed, 0) {
		avgSpeed = 0
	}
	if maxSpeed < 0 || maxSpeed > MaxPlausibleSpeedKts || math.IsNaN(maxSpeed) || math.IsInf(maxSpeed, 0) {
		maxSpeed = 0
	}
	if totalDist < 0 || math.IsNaN(totalDist) || math.IsInf(totalDist, 0) {
		totalDist = 0
	} else if totalDist > 999999.99 {
		totalDist = 999999.99
	}

	// Generate GeoJSON and Simplified GeoJSON
	geoJSONBytes := BuildGeoJSON(name, kind, points, totalDist, startTime, endTime, maxSpeed, avgSpeed)
	simplifiedPoints := SimplifyRDP(points, DefaultEpsilonNM)
	simplifiedGeoJSONBytes := BuildGeoJSON(name, kind, simplifiedPoints, totalDist, startTime, endTime, maxSpeed, avgSpeed)

	return ParsedTrack{
		Name:              name,
		Kind:              kind,
		Points:            points,
		StartTime:         startTime,
		EndTime:           endTime,
		DistanceNM:        math.Round(totalDist*100) / 100,
		DurationInterval:  durationInterval,
		MaxSpeedKts:       math.Round(maxSpeed*10) / 10,
		AvgSpeedKts:       math.Round(avgSpeed*10) / 10,
		GeoJSON:           geoJSONBytes,
		SimplifiedGeoJSON: simplifiedGeoJSONBytes,
	}
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
