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

// FilterGPX parses GPX XML, detects GPS glitches and speed spikes in track
// segments (<trk>), removes them, and returns cleaned XML. Routes (<rte>) and
// waypoints (<wpt>) are left untouched because their <time> elements represent
// creation timestamps rather than travel telemetry. If no glitches were
// detected, the original data is returned verbatim.
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

	// Routes (<rte>) are intentionally not filtered: route points are planned
	// waypoints, and their <time> (e.g. OpenCPN waypoint creation time) is not
	// a travel timestamp, so speed-based glitch detection would drop real legs.
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
		return data, totalGlitches, fmt.Errorf("failed to marshal clean GPX XML: %w", err)
	}

	return append([]byte(xml.Header), outBytes...), totalGlitches, nil
}

func filterPointSlice(pts []filterPoint) ([]filterPoint, int) {
	if len(pts) <= 2 {
		return pts, 0
	}

	points := make([]Point, len(pts))
	for i, pt := range pts {
		points[i].Lat = pt.Lat
		points[i].Lng = pt.Lon
		if pt.Time != "" {
			if t, err := parseGPXTime(pt.Time); err == nil {
				points[i].Time = t
			}
		}
		if pt.Speed != nil {
			spd := *pt.Speed * MPS2Knots
			points[i].SpeedKts = &spd
		}
	}

	glitchMap, glitchCount := detectPointGlitches(points)
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

	glitchMap, glitchCount := detectPointGlitches(points)
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

// detectPointGlitches identifies indices in points that exceed plausible speed,
// acceleration, or 3-point spike thresholds.
func detectPointGlitches(points []Point) (map[int]bool, int) {
	glitchMap := make(map[int]bool)
	glitchCount := 0
	lastValidIdx := -1

	for i := 0; i < len(points); i++ {
		isGlitch := false

		var spd float64
		hasSpeed := false

		if points[i].SpeedKts != nil {
			spd = *points[i].SpeedKts
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
			} else if lastValidIdx >= 0 && points[lastValidIdx].Time != nil && points[i].Time != nil && points[lastValidIdx].SpeedKts != nil {
				prevSpd := *points[lastValidIdx].SpeedKts
				dtSec := points[i].Time.Sub(*points[lastValidIdx].Time).Seconds()
				deltaV := math.Abs(spd - prevSpd)

				if dtSec > 0 && dtSec <= MaxGlitchWindowSec {
					if deltaV/dtSec > MaxAccelerationKtsPerSec {
						isGlitch = true
					}
				} else if dtSec <= 0 && deltaV > 0.5 {
					isGlitch = true
				}
			}

			// 3-point peak spike check
			if !isGlitch && i > 0 && i < len(points)-1 &&
				points[i-1].SpeedKts != nil && points[i+1].SpeedKts != nil &&
				points[i-1].Time != nil && points[i+1].Time != nil && points[i].Time != nil {
				dtPrev := points[i].Time.Sub(*points[i-1].Time).Seconds()
				dtNext := points[i+1].Time.Sub(*points[i].Time).Seconds()
				if dtPrev > 0 && dtPrev <= 15.0 && dtNext > 0 && dtNext <= 15.0 {
					sPrev := *points[i-1].SpeedKts
					sNext := *points[i+1].SpeedKts
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

	return glitchMap, glitchCount
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
