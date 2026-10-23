// Package gpx provides deterministic geometric and kinematic analysis of
// recorded GPX / GeoJSON tracks against planned routes for post-voyage debriefs.
package gpx

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"time"
)

// TrackAnomalyWindow represents a detected speed-drop, tack-cluster, or
// cross-track deviation window along a recorded track.
type TrackAnomalyWindow struct {
	Type        string  `json:"type"` // "tack_cluster", "speed_drop", "course_deviation"
	StartTime   string  `json:"start_time,omitempty"`
	EndTime     string  `json:"end_time,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	MetricValue float64 `json:"metric_value"`
	Description string  `json:"description"`
}

// TelemetryAnalysis summarizes deterministic geometric metrics comparing a
// recorded track against its paired planned route.
type TelemetryAnalysis struct {
	PointCount         int                  `json:"point_count"`
	TackAndGybeCount   int                  `json:"tack_and_gybe_count"`
	MeanTackAngleDeg   float64              `json:"mean_tack_angle_deg,omitempty"`
	MaxCrossTrackNM    float64              `json:"max_cross_track_nm"`
	MeanCrossTrackNM   float64              `json:"mean_cross_track_nm"`
	EstimatedVMGKts    float64              `json:"estimated_vmg_kts,omitempty"`
	SlowestQuarterKts  float64              `json:"slowest_quarter_kts,omitempty"`
	FastestQuarterKts  float64              `json:"fastest_quarter_kts,omitempty"`
	Anomalies          []TrackAnomalyWindow `json:"anomalies,omitempty"`
	FormattedSummary   string               `json:"formatted_summary"`
}

type trackPoint struct {
	Lat  float64
	Lon  float64
	Time time.Time
}

type gpxDoc struct {
	Tracks []struct {
		Segments []struct {
			Points []struct {
				Lat  float64 `xml:"lat,attr"`
				Lon  float64 `xml:"lon,attr"`
				Time string  `xml:"time"`
			} `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

// AnalyzeGPXTelemetry computes Cross-Track Error (XTE), tack/gybe count,
// mean tack angle, VMG, quarter-passage SOG variance, and timestamped anomaly
// windows from a recorded track (GeoJSON and/or raw GPX XML) against a planned GeoJSON track.
func AnalyzeGPXTelemetry(plannedGeoJSON, actualGeoJSON []byte, rawGPX *string, avgSpeedKts float64) TelemetryAnalysis {
	var pts []trackPoint
	if rawGPX != nil && strings.TrimSpace(*rawGPX) != "" {
		pts = parseGPXPoints(*rawGPX)
	}
	if len(pts) == 0 && len(actualGeoJSON) > 0 {
		pts = parseGeoJSONPoints(actualGeoJSON)
	}
	plannedPts := parseGeoJSONPoints(plannedGeoJSON)

	res := TelemetryAnalysis{
		PointCount: len(pts),
	}
	if len(pts) < 2 {
		return res
	}

	// 1. Cross-Track Error (XTE) against planned route segments.
	if len(plannedPts) >= 2 {
		var sumXTE, maxXTE float64
		var maxXTEPt trackPoint
		for _, p := range pts {
			d := minDistanceToPolylineNM(p, plannedPts)
			sumXTE += d
			if d > maxXTE {
				maxXTE = d
				maxXTEPt = p
			}
		}
		res.MaxCrossTrackNM = math.Round(maxXTE*100) / 100
		res.MeanCrossTrackNM = math.Round((sumXTE/float64(len(pts)))*100) / 100
		if res.MaxCrossTrackNM >= 0.35 {
			tStr := ""
			if !maxXTEPt.Time.IsZero() {
				tStr = maxXTEPt.Time.UTC().Format(time.RFC3339)
			}
			res.Anomalies = append(res.Anomalies, TrackAnomalyWindow{
				Type:        "course_deviation",
				StartTime:   tStr,
				Latitude:    math.Round(maxXTEPt.Lat*10000) / 10000,
				Longitude:   math.Round(maxXTEPt.Lon*10000) / 10000,
				MetricValue: res.MaxCrossTrackNM,
				Description: fmt.Sprintf("Peak cross-track deviation of %.2f NM from planned course near (%.4f, %.4f)", res.MaxCrossTrackNM, maxXTEPt.Lat, maxXTEPt.Lon),
			})
		}
	}

	// 2. Maneuver / Tack & Gybe Detection (sustained heading changes >= 50 deg).
	var bearings []float64
	var segPts []trackPoint
	for i := 0; i < len(pts)-1; i++ {
		d := haversineNM(pts[i].Lat, pts[i].Lon, pts[i+1].Lat, pts[i+1].Lon)
		if d < 0.01 {
			continue
		}
		bearings = append(bearings, bearingDeg(pts[i].Lat, pts[i].Lon, pts[i+1].Lat, pts[i+1].Lon))
		segPts = append(segPts, pts[i+1])
	}

	var tackAngleSum float64
	var tackPts []trackPoint
	for i := 1; i < len(bearings); i++ {
		diff := angularDiffDeg(bearings[i-1], bearings[i])
		if diff >= 50.0 && diff <= 145.0 {
			res.TackAndGybeCount++
			tackAngleSum += diff
			tackPts = append(tackPts, segPts[i])
		}
	}
	if res.TackAndGybeCount > 0 {
		res.MeanTackAngleDeg = math.Round((tackAngleSum/float64(res.TackAndGybeCount))*10) / 10
		if len(tackPts) >= 2 {
			first := tackPts[0]
			last := tackPts[len(tackPts)-1]
			var startStr, endStr string
			if !first.Time.IsZero() {
				startStr = first.Time.UTC().Format(time.RFC3339)
			}
			if !last.Time.IsZero() {
				endStr = last.Time.UTC().Format(time.RFC3339)
			}
			res.Anomalies = append(res.Anomalies, TrackAnomalyWindow{
				Type:        "tack_cluster",
				StartTime:   startStr,
				EndTime:     endStr,
				Latitude:    math.Round(first.Lat*10000) / 10000,
				Longitude:   math.Round(first.Lon*10000) / 10000,
				MetricValue: float64(res.TackAndGybeCount),
				Description: fmt.Sprintf("%d tacks/gybes detected (mean maneuver angle %.0f°) starting near (%.4f, %.4f)", res.TackAndGybeCount, res.MeanTackAngleDeg, first.Lat, first.Lon),
			})
		}
	}

	// 3. VMG & Quarter-Passage SOG Variance (when timestamps are available).
	startPt := pts[0]
	endPt := pts[len(pts)-1]
	directEndToEndNM := haversineNM(startPt.Lat, startPt.Lon, endPt.Lat, endPt.Lon)
	if !startPt.Time.IsZero() && !endPt.Time.IsZero() && endPt.Time.After(startPt.Time) {
		totalHours := endPt.Time.Sub(startPt.Time).Hours()
		if totalHours > 0 {
			res.EstimatedVMGKts = math.Round((directEndToEndNM/totalHours)*100) / 100
		}
		if len(pts) >= 8 {
			qSize := len(pts) / 4
			minQ := 999.0
			maxQ := 0.0
			var slowPt trackPoint
			for q := 0; q < 4; q++ {
				sIdx := q * qSize
				eIdx := (q + 1) * qSize
				if q == 3 || eIdx >= len(pts) {
					eIdx = len(pts) - 1
				}
				dt := pts[eIdx].Time.Sub(pts[sIdx].Time).Hours()
				if dt <= 0 {
					continue
				}
				var dist float64
				for k := sIdx; k < eIdx; k++ {
					dist += haversineNM(pts[k].Lat, pts[k].Lon, pts[k+1].Lat, pts[k+1].Lon)
				}
				spd := dist / dt
				if spd < minQ {
					minQ = spd
					slowPt = pts[sIdx]
				}
				if spd > maxQ {
					maxQ = spd
				}
			}
			if minQ < 999.0 && maxQ > 0 {
				res.SlowestQuarterKts = math.Round(minQ*10) / 10
				res.FastestQuarterKts = math.Round(maxQ*10) / 10
				if maxQ >= minQ*1.4 && !slowPt.Time.IsZero() {
					res.Anomalies = append(res.Anomalies, TrackAnomalyWindow{
						Type:        "speed_drop",
						StartTime:   slowPt.Time.UTC().Format(time.RFC3339),
						Latitude:    math.Round(slowPt.Lat*10000) / 10000,
						Longitude:   math.Round(slowPt.Lon*10000) / 10000,
						MetricValue: res.SlowestQuarterKts,
						Description: fmt.Sprintf("SOG dropped to %.1f kts (vs %.1f kts peak quarter) around %s near (%.4f, %.4f)", res.SlowestQuarterKts, res.FastestQuarterKts, slowPt.Time.UTC().Format("15:04 UTC"), slowPt.Lat, slowPt.Lon),
					})
				}
			}
		}
	} else if avgSpeedKts > 0 {
		var totalRecNM float64
		for i := 0; i < len(pts)-1; i++ {
			totalRecNM += haversineNM(pts[i].Lat, pts[i].Lon, pts[i+1].Lat, pts[i+1].Lon)
		}
		if totalRecNM > 0 {
			res.EstimatedVMGKts = math.Round(avgSpeedKts*(directEndToEndNM/totalRecNM)*100) / 100
		}
	}

	var b strings.Builder
	b.WriteString("Deterministic GPX Telemetry Analysis:\n")
	fmt.Fprintf(&b, "- Cross-Track Error (XTE vs Planned Route): Mean %.2f NM, Peak %.2f NM\n", res.MeanCrossTrackNM, res.MaxCrossTrackNM)
	fmt.Fprintf(&b, "- Maneuvers (Tacks/Gybes >= 50°): %d", res.TackAndGybeCount)
	if res.MeanTackAngleDeg > 0 {
		fmt.Fprintf(&b, " (Mean maneuver angle: %.0f°)", res.MeanTackAngleDeg)
	}
	b.WriteString("\n")
	if res.EstimatedVMGKts > 0 {
		fmt.Fprintf(&b, "- Net Destination VMG: %.2f kts\n", res.EstimatedVMGKts)
	}
	if res.SlowestQuarterKts > 0 && res.FastestQuarterKts > 0 {
		fmt.Fprintf(&b, "- Speed Variance by Quarter: Slowest %.1f kts vs Fastest %.1f kts\n", res.SlowestQuarterKts, res.FastestQuarterKts)
	}
	if len(res.Anomalies) > 0 {
		b.WriteString("- Detected Track Anomalies (use GetWeather / GetTides if needed to correlate with wind/tidal streams):\n")
		for _, a := range res.Anomalies {
			fmt.Fprintf(&b, "  * [%s] %s\n", a.Type, a.Description)
		}
	}
	res.FormattedSummary = b.String()
	return res
}

func parseGPXPoints(rawXML string) []trackPoint {
	var doc gpxDoc
	if err := xml.Unmarshal([]byte(rawXML), &doc); err != nil {
		return nil
	}
	var pts []trackPoint
	for _, trk := range doc.Tracks {
		for _, seg := range trk.Segments {
			for _, pt := range seg.Points {
				var ts time.Time
				if pt.Time != "" {
					if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(pt.Time)); err == nil {
						ts = parsed
					}
				}
				pts = append(pts, trackPoint{Lat: pt.Lat, Lon: pt.Lon, Time: ts})
			}
		}
	}
	return pts
}

func parseGeoJSONPoints(raw []byte) []trackPoint {
	if len(raw) == 0 {
		return nil
	}
	var wrapper struct {
		Type     string `json:"type"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil
	}
	coordsRaw := wrapper.Geometry.Coordinates
	if len(coordsRaw) == 0 {
		coordsRaw = wrapper.Coordinates
	}
	var line [][]float64
	if err := json.Unmarshal(coordsRaw, &line); err == nil && len(line) > 0 {
		pts := make([]trackPoint, 0, len(line))
		for _, c := range line {
			if len(c) >= 2 {
				pts = append(pts, trackPoint{Lon: c[0], Lat: c[1]})
			}
		}
		return pts
	}
	return nil
}

func minDistanceToPolylineNM(p trackPoint, poly []trackPoint) float64 {
	best := math.MaxFloat64
	for i := 0; i < len(poly)-1; i++ {
		d := pointToSegmentDistanceNM(p, poly[i], poly[i+1])
		if d < best {
			best = d
		}
	}
	if best == math.MaxFloat64 {
		return 0
	}
	return best
}

func pointToSegmentDistanceNM(p, a, b trackPoint) float64 {
	latScale := 60.0
	lonScale := 60.0 * math.Cos(((a.Lat+b.Lat)/2.0)*math.Pi/180.0)
	px := (p.Lon - a.Lon) * lonScale
	py := (p.Lat - a.Lat) * latScale
	bx := (b.Lon - a.Lon) * lonScale
	by := (b.Lat - a.Lat) * latScale

	segLenSq := bx*bx + by*by
	if segLenSq == 0 {
		return math.Hypot(px, py)
	}
	t := (px*bx + py*by) / segLenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	projX := t * bx
	projY := t * by
	return math.Hypot(px-projX, py-projY)
}

func haversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 3440.065
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLam := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLam/2)*math.Sin(dLam/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dLam := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(dLam) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(dLam)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

func angularDiffDeg(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360.0)
	if d > 180 {
		d = 360 - d
	}
	return d
}
