package gpx

import (
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"time"
)

// filterGPXDoc represents the GPX XML hierarchy for filtering and smoothing.
type filterGPXDoc struct {
	XMLName   xml.Name        `xml:"gpx"`
	Version   string          `xml:"version,attr,omitempty"`
	Creator   string          `xml:"creator,attr,omitempty"`
	Xmlns     string          `xml:"xmlns,attr,omitempty"`
	Metadata  *filterMetadata `xml:"metadata,omitempty"`
	Routes    []filterRoute   `xml:"rte,omitempty"`
	Tracks    []filterTrack   `xml:"trk,omitempty"`
	Waypoints []filterPoint   `xml:"wpt,omitempty"`
}

type filterMetadata struct {
	Name string `xml:"name,omitempty"`
	Desc string `xml:"desc,omitempty"`
	Time string `xml:"time,omitempty"`
}

type filterRoute struct {
	Name        string        `xml:"name,omitempty"`
	Desc        string        `xml:"desc,omitempty"`
	RoutePoints []filterPoint `xml:"rtept,omitempty"`
}

type filterTrack struct {
	Name     string          `xml:"name,omitempty"`
	Desc     string          `xml:"desc,omitempty"`
	Segments []filterSegment `xml:"trkseg,omitempty"`
}

type filterSegment struct {
	TrackPoints []filterPoint `xml:"trkpt,omitempty"`
}

type filterPoint struct {
	Lat    float64  `xml:"lat,attr"`
	Lon    float64  `xml:"lon,attr"`
	Ele    *float64 `xml:"ele,omitempty"`
	Time   string   `xml:"time,omitempty"`
	Speed  *float64 `xml:"speed,omitempty"`  // meters/sec in GPX 1.1
	Course *float64 `xml:"course,omitempty"` // degrees 0-360
	Name   string   `xml:"name,omitempty"`
	Desc   string   `xml:"desc,omitempty"`
}

// FilterGPX parses GPX XML, detects GPS glitches and speed spikes, removes them, and returns clean XML.
// If no glitches were detected, the original data is returned verbatim.
func FilterGPX(data []byte) ([]byte, int, error) {
	var doc filterGPXDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return data, 0, fmt.Errorf("failed to parse GPX XML: %w", err)
	}

	totalGlitches := 0

	// 1. Filter track segments
	for tIdx := range doc.Tracks {
		trk := &doc.Tracks[tIdx]
		var cleanSegments []filterSegment

		for sIdx := range trk.Segments {
			seg := &trk.Segments[sIdx]
			if len(seg.TrackPoints) == 0 {
				continue
			}

			cleanPts, glitches := filterPointSlice(seg.TrackPoints)
			totalGlitches += glitches

			if len(cleanPts) > 0 {
				cleanSegments = append(cleanSegments, filterSegment{TrackPoints: cleanPts})
			} else {
				// If all points were glitches, retain the first point so the segment is not empty
				cleanSegments = append(cleanSegments, filterSegment{TrackPoints: []filterPoint{seg.TrackPoints[0]}})
			}
		}

		trk.Segments = cleanSegments
	}

	// 2. Filter route points if no tracks are present
	if len(doc.Tracks) == 0 && len(doc.Routes) > 0 {
		for rIdx := range doc.Routes {
			rte := &doc.Routes[rIdx]
			if len(rte.RoutePoints) == 0 {
				continue
			}
			cleanPts, glitches := filterPointSlice(rte.RoutePoints)
			totalGlitches += glitches
			if len(cleanPts) > 0 {
				rte.RoutePoints = cleanPts
			}
		}
	}

	if totalGlitches == 0 {
		return data, 0, nil
	}

	if doc.Xmlns == "" {
		doc.Xmlns = "http://www.topografix.com/GPX/1/1"
	}
	if doc.Version == "" {
		doc.Version = "1.1"
	}
	if doc.Creator == "" {
		doc.Creator = "NavalPlan"
	}

	outBytes, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return data, 0, fmt.Errorf("failed to marshal clean GPX XML: %w", err)
	}

	return append([]byte(xml.Header), outBytes...), totalGlitches, nil
}

func filterPointSlice(pts []filterPoint) ([]filterPoint, int) {
	if len(pts) <= 2 {
		return pts, 0
	}

	parsedTimes := make([]*time.Time, len(pts))
	speedsKts := make([]*float64, len(pts))

	for i, pt := range pts {
		if pt.Time != "" {
			if t, err := parseGPXTime(pt.Time); err == nil {
				parsedTimes[i] = t
			}
		}
		if pt.Speed != nil {
			// GPX speed is m/s; 1 m/s = 1.943844 knots
			spd := *pt.Speed * 1.943844
			speedsKts[i] = &spd
		}
	}

	glitchMap := make(map[int]bool)
	glitchCount := 0
	lastValidIdx := -1

	for i := 0; i < len(pts); i++ {
		isGlitch := false

		var spd float64
		hasSpeed := false

		if speedsKts[i] != nil {
			spd = *speedsKts[i]
			hasSpeed = true
		} else if lastValidIdx >= 0 && parsedTimes[lastValidIdx] != nil && parsedTimes[i] != nil {
			dtHours := parsedTimes[i].Sub(*parsedTimes[lastValidIdx]).Hours()
			if dtHours > 0 {
				dist := CalculateHaversineNM(pts[lastValidIdx].Lat, pts[lastValidIdx].Lon, pts[i].Lat, pts[i].Lon)
				spd = dist / dtHours
				hasSpeed = true
			}
		}

		if hasSpeed {
			if spd > MaxPlausibleSpeedKts {
				isGlitch = true
			} else if lastValidIdx >= 0 && parsedTimes[lastValidIdx] != nil && parsedTimes[i] != nil {
				var prevSpd float64
				hasPrevSpd := false
				if speedsKts[lastValidIdx] != nil {
					prevSpd = *speedsKts[lastValidIdx]
					hasPrevSpd = true
				}

				if hasPrevSpd {
					dtSec := parsedTimes[i].Sub(*parsedTimes[lastValidIdx]).Seconds()
					deltaV := math.Abs(spd - prevSpd)

					if dtSec > 0 && dtSec <= MaxGlitchWindowSec {
						accel := deltaV / dtSec
						if accel > MaxAccelerationKtsPerSec {
							isGlitch = true
						}
					} else if dtSec <= 0 && deltaV > 0.5 {
						isGlitch = true
					}
				}
			}

			// 3-point peak spike check
			if !isGlitch && i > 0 && i < len(pts)-1 && speedsKts[i-1] != nil && speedsKts[i+1] != nil && parsedTimes[i-1] != nil && parsedTimes[i+1] != nil && parsedTimes[i] != nil {
				dtPrev := parsedTimes[i].Sub(*parsedTimes[i-1]).Seconds()
				dtNext := parsedTimes[i+1].Sub(*parsedTimes[i]).Seconds()
				if dtPrev > 0 && dtPrev <= 15.0 && dtNext > 0 && dtNext <= 15.0 {
					sPrev := *speedsKts[i-1]
					sNext := *speedsKts[i+1]
					if (spd-sPrev) > 5.0 && (spd-sNext) > 5.0 {
						isGlitch = true
					}
				}
			}
		}

		if isGlitch {
			glitchMap[i] = true
			glitchCount++
		} else {
			lastValidIdx = i
		}
	}

	if glitchCount == 0 {
		return pts, 0
	}

	cleanPts := make([]filterPoint, 0, len(pts)-glitchCount)
	for i, pt := range pts {
		if !glitchMap[i] {
			cleanPts = append(cleanPts, pt)
		}
	}

	return cleanPts, glitchCount
}

// FilterTrackPoints evaluates a slice of Point and removes points representing GPS speed glitches or impossible jumps.
func FilterTrackPoints(points []Point) ([]Point, int) {
	if len(points) <= 2 {
		return points, 0
	}

	speeds := make([]*float64, len(points))
	for i := range points {
		if points[i].SpeedKts != nil {
			speeds[i] = points[i].SpeedKts
		}
	}

	glitchMap := make(map[int]bool)
	glitchCount := 0
	lastValidIdx := -1

	for i := 0; i < len(points); i++ {
		isGlitch := false

		var spd float64
		hasSpeed := false

		if speeds[i] != nil {
			spd = *speeds[i]
			hasSpeed = true
		} else if lastValidIdx >= 0 && points[lastValidIdx].Time != nil && points[i].Time != nil {
			dtHours := points[i].Time.Sub(*points[lastValidIdx].Time).Hours()
			if dtHours > 0 {
				dist := CalculateHaversineNM(points[lastValidIdx].Lat, points[lastValidIdx].Lng, points[i].Lat, points[i].Lng)
				spd = dist / dtHours
				hasSpeed = true
			}
		}

		if hasSpeed {
			if spd > MaxPlausibleSpeedKts {
				isGlitch = true
			} else if lastValidIdx >= 0 && points[lastValidIdx].Time != nil && points[i].Time != nil {
				var prevSpd float64
				hasPrevSpd := false
				if speeds[lastValidIdx] != nil {
					prevSpd = *speeds[lastValidIdx]
					hasPrevSpd = true
				}

				if hasPrevSpd {
					dtSec := points[i].Time.Sub(*points[lastValidIdx].Time).Seconds()
					deltaV := math.Abs(spd - prevSpd)

					if dtSec > 0 && dtSec <= MaxGlitchWindowSec {
						accel := deltaV / dtSec
						if accel > MaxAccelerationKtsPerSec {
							isGlitch = true
						}
					} else if dtSec <= 0 && deltaV > 0.5 {
						isGlitch = true
					}
				}
			}

			// 3-point peak spike check
			if !isGlitch && i > 0 && i < len(points)-1 && speeds[i-1] != nil && speeds[i+1] != nil && points[i-1].Time != nil && points[i+1].Time != nil && points[i].Time != nil {
				dtPrev := points[i].Time.Sub(*points[i-1].Time).Seconds()
				dtNext := points[i+1].Time.Sub(*points[i].Time).Seconds()
				if dtPrev > 0 && dtPrev <= 15.0 && dtNext > 0 && dtNext <= 15.0 {
					sPrev := *speeds[i-1]
					sNext := *speeds[i+1]
					if (spd-sPrev) > 5.0 && (spd-sNext) > 5.0 {
						isGlitch = true
					}
				}
			}
		}

		if isGlitch {
			glitchMap[i] = true
			glitchCount++
		} else {
			lastValidIdx = i
		}
	}

	if glitchCount == 0 {
		return points, 0
	}

	cleanPoints := make([]Point, 0, len(points)-glitchCount)
	for i, pt := range points {
		if !glitchMap[i] {
			cleanPoints = append(cleanPoints, pt)
		}
	}

	if len(cleanPoints) == 0 {
		return points, 0
	}

	return cleanPoints, glitchCount
}

func parseGPXTime(timeStr string) (*time.Time, error) {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return nil, fmt.Errorf("empty time string")
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, timeStr); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("unable to parse time string: %s", timeStr)
}
