package tool

import (
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	DefaultMaxAccelerationKtsPerSec = 2.0
	DefaultMaxPlausibleSpeedKts     = 45.0
	MPS2Knots                       = 1.943844
	EarthRadiusNM                   = 3440.065
)

// FilterGPXRequest represents the input parameters for the FilterGPXData tool.
type FilterGPXRequest struct {
	GPXData                  string            `json:"gpx_data,omitempty"`
	TrackPoints              []TrackPointInput `json:"track_points,omitempty"`
	MaxAccelerationKtsPerSec *float64          `json:"max_acceleration_kts_per_sec,omitempty"`
	MaxPlausibleSpeedKts     *float64          `json:"max_plausible_speed_kts,omitempty"`
}

// TrackPointInput represents an individual coordinate point with timestamp and speed.
type TrackPointInput struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Time      string   `json:"time,omitempty"`
	SpeedKts  *float64 `json:"speed_kts,omitempty"`
}

// GlitchRecord documents an individual GPS speed anomaly filtered out by the tool.
type GlitchRecord struct {
	Index                 int     `json:"index"`
	Time                  string  `json:"time,omitempty"`
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	ReportedSpeedKts      float64 `json:"reported_speed_kts"`
	PreviousSpeedKts      float64 `json:"previous_speed_kts"`
	DeltaSpeedKts         float64 `json:"delta_speed_kts"`
	DeltaTimeSec          float64 `json:"delta_time_sec"`
	AccelerationKtsPerSec float64 `json:"acceleration_kts_per_sec"`
	Reason                string  `json:"reason"`
}

// FilterGPXResponse contains the filtered metrics, glitch statistics, and clean GPX data.
type FilterGPXResponse struct {
	CleanMaxSpeedKts float64        `json:"clean_max_speed_kts"`
	CleanAvgSpeedKts float64        `json:"clean_avg_speed_kts"`
	RawMaxSpeedKts   float64        `json:"raw_max_speed_kts"`
	RawAvgSpeedKts   float64        `json:"raw_avg_speed_kts"`
	TotalPoints      int            `json:"total_points"`
	ValidPoints      int            `json:"valid_points"`
	GlitchesCount    int            `json:"glitches_count"`
	Glitches         []GlitchRecord `json:"glitches,omitempty"`
	FilteredGPX      string         `json:"filtered_gpx,omitempty"`
	Summary          string         `json:"summary"`
}

type gpxDoc struct {
	XMLName   xml.Name     `xml:"gpx"`
	Version   string       `xml:"version,attr,omitempty"`
	Creator   string       `xml:"creator,attr,omitempty"`
	Xmlns     string       `xml:"xmlns,attr,omitempty"`
	Metadata  *gpxMetadata `xml:"metadata,omitempty"`
	Routes    []gpxRoute   `xml:"rte,omitempty"`
	Tracks    []gpxTrack   `xml:"trk,omitempty"`
	Waypoints []gpxPoint   `xml:"wpt,omitempty"`
}

type gpxMetadata struct {
	Name string `xml:"name,omitempty"`
	Time string `xml:"time,omitempty"`
}

type gpxRoute struct {
	Name        string     `xml:"name,omitempty"`
	RoutePoints []gpxPoint `xml:"rtept,omitempty"`
}

type gpxTrack struct {
	Name     string       `xml:"name,omitempty"`
	Segments []gpxSegment `xml:"trkseg,omitempty"`
}

type gpxSegment struct {
	TrackPoints []gpxPoint `xml:"trkpt,omitempty"`
}

type gpxPoint struct {
	Lat   float64  `xml:"lat,attr"`
	Lon   float64  `xml:"lon,attr"`
	Ele   *float64 `xml:"ele,omitempty"`
	Time  string   `xml:"time,omitempty"`
	Speed *float64 `xml:"speed,omitempty"`
	Name  string   `xml:"name,omitempty"`
}

type internalPoint struct {
	Lat       float64
	Lon       float64
	Ele       *float64
	TimeStr   string
	Time      *time.Time
	SpeedKts  *float64
	Name      string
	TrkIdx    int
	SegIdx    int
	PtIdx     int
	SourceDoc string // "trk", "rte", "wpt", or "input"
}

// FilterGPXDataResult evaluates GPX XML or track points and filters out GPS speed glitches.
func FilterGPXDataResult(req FilterGPXRequest) (*FilterGPXResponse, error) {
	if strings.TrimSpace(req.GPXData) == "" && len(req.TrackPoints) == 0 {
		return nil, fmt.Errorf("either gpx_data or track_points must be provided")
	}

	maxAccel := DefaultMaxAccelerationKtsPerSec
	if req.MaxAccelerationKtsPerSec != nil && *req.MaxAccelerationKtsPerSec > 0 {
		maxAccel = *req.MaxAccelerationKtsPerSec
	}

	maxPlausibleSpeed := DefaultMaxPlausibleSpeedKts
	if req.MaxPlausibleSpeedKts != nil && *req.MaxPlausibleSpeedKts > 0 {
		maxPlausibleSpeed = *req.MaxPlausibleSpeedKts
	}

	var parsedDoc *gpxDoc
	var pts []internalPoint

	if strings.TrimSpace(req.GPXData) != "" {
		var doc gpxDoc
		if err := xml.Unmarshal([]byte(req.GPXData), &doc); err != nil {
			return nil, fmt.Errorf("failed to parse GPX XML: %w", err)
		}
		parsedDoc = &doc

		for tIdx, trk := range doc.Tracks {
			for sIdx, seg := range trk.Segments {
				for pIdx, pt := range seg.TrackPoints {
					ip := internalPoint{
						Lat:       pt.Lat,
						Lon:       pt.Lon,
						Ele:       pt.Ele,
						TimeStr:   pt.Time,
						Name:      pt.Name,
						TrkIdx:    tIdx,
						SegIdx:    sIdx,
						PtIdx:     pIdx,
						SourceDoc: "trk",
					}
					if pt.Time != "" {
						if t, err := parseGPXTime(pt.Time); err == nil {
							ip.Time = &t
						}
					}
					if pt.Speed != nil {
						spd := *pt.Speed * MPS2Knots
						ip.SpeedKts = &spd
					}
					pts = append(pts, ip)
				}
			}
		}

		if len(pts) == 0 {
			for rIdx, rte := range doc.Routes {
				for pIdx, pt := range rte.RoutePoints {
					ip := internalPoint{
						Lat:       pt.Lat,
						Lon:       pt.Lon,
						Ele:       pt.Ele,
						TimeStr:   pt.Time,
						Name:      pt.Name,
						TrkIdx:    rIdx,
						PtIdx:     pIdx,
						SourceDoc: "rte",
					}
					if pt.Time != "" {
						if t, err := parseGPXTime(pt.Time); err == nil {
							ip.Time = &t
						}
					}
					if pt.Speed != nil {
						spd := *pt.Speed * MPS2Knots
						ip.SpeedKts = &spd
					}
					pts = append(pts, ip)
				}
			}
		}

		if len(pts) == 0 {
			for pIdx, pt := range doc.Waypoints {
				ip := internalPoint{
					Lat:       pt.Lat,
					Lon:       pt.Lon,
					Ele:       pt.Ele,
					TimeStr:   pt.Time,
					Name:      pt.Name,
					PtIdx:     pIdx,
					SourceDoc: "wpt",
				}
				if pt.Time != "" {
					if t, err := parseGPXTime(pt.Time); err == nil {
						ip.Time = &t
					}
				}
				pts = append(pts, ip)
			}
		}
	} else {
		for i, tp := range req.TrackPoints {
			ip := internalPoint{
				Lat:       tp.Latitude,
				Lon:       tp.Longitude,
				TimeStr:   tp.Time,
				SpeedKts:  tp.SpeedKts,
				PtIdx:     i,
				SourceDoc: "input",
			}
			if tp.Time != "" {
				if t, err := parseGPXTime(tp.Time); err == nil {
					ip.Time = &t
				}
			}
			pts = append(pts, ip)
		}
	}

	if len(pts) == 0 {
		return nil, fmt.Errorf("no track points or waypoints found in GPX data")
	}

	// Calculate point-to-point speed if missing
	for i := 1; i < len(pts); i++ {
		if pts[i].SpeedKts == nil && pts[i-1].Time != nil && pts[i].Time != nil {
			dtHours := pts[i].Time.Sub(*pts[i-1].Time).Hours()
			if dtHours > 0 {
				dist := haversineDistanceNM(pts[i-1].Lat, pts[i-1].Lon, pts[i].Lat, pts[i].Lon)
				calcSpeed := dist / dtHours
				pts[i].SpeedKts = &calcSpeed
			}
		}
	}

	// Compute raw statistics across all points
	var rawMaxSpeed float64
	var rawSpeedSum float64
	var rawSpeedCount int
	for _, pt := range pts {
		if pt.SpeedKts != nil {
			spd := *pt.SpeedKts
			if spd > rawMaxSpeed {
				rawMaxSpeed = spd
			}
			rawSpeedSum += spd
			rawSpeedCount++
		}
	}
	var rawAvgSpeed float64
	if rawSpeedCount > 0 {
		rawAvgSpeed = rawSpeedSum / float64(rawSpeedCount)
	}

	var glitches []GlitchRecord
	glitchSet := make(map[int]bool)
	lastValidIdx := -1

	for i := 0; i < len(pts); i++ {
		if pts[i].SpeedKts == nil {
			continue
		}
		spd := *pts[i].SpeedKts
		isGlitch := false
		var reason string
		var prevSpd, deltaV, dtSec, accel float64

		if spd > maxPlausibleSpeed {
			isGlitch = true
			reason = fmt.Sprintf("Speed %.1f kts exceeds plausible speed threshold of %.1f kts", spd, maxPlausibleSpeed)
		} else if lastValidIdx >= 0 && pts[lastValidIdx].SpeedKts != nil && pts[lastValidIdx].Time != nil && pts[i].Time != nil {
			prevSpd = *pts[lastValidIdx].SpeedKts
			dt := pts[i].Time.Sub(*pts[lastValidIdx].Time)
			dtSec = dt.Seconds()
			deltaV = math.Abs(spd - prevSpd)

			if dtSec > 0 && dtSec <= 60.0 {
				accel = deltaV / dtSec
				if accel > maxAccel {
					isGlitch = true
					reason = fmt.Sprintf("Speed changed by %.1f kts in %.1fs (%.2f kts/s exceeds threshold %.2f kts/s)", deltaV, dtSec, accel, maxAccel)
				}
			} else if dtSec <= 0 && deltaV > 0.5 {
				isGlitch = true
				reason = "Instantaneous jump with zero elapsed time"
			}
		}

		// 3-point peak spike check (isolated spike that returns to normal)
		if !isGlitch && i > 0 && i < len(pts)-1 && pts[i-1].SpeedKts != nil && pts[i+1].SpeedKts != nil && pts[i-1].Time != nil && pts[i+1].Time != nil {
			sPrev := *pts[i-1].SpeedKts
			sNext := *pts[i+1].SpeedKts
			dtPrev := pts[i].Time.Sub(*pts[i-1].Time).Seconds()
			dtNext := pts[i+1].Time.Sub(*pts[i].Time).Seconds()
			if dtPrev > 0 && dtPrev <= 15.0 && dtNext > 0 && dtNext <= 15.0 {
				if (spd-sPrev) > 5.0 && (spd-sNext) > 5.0 {
					isGlitch = true
					reason = fmt.Sprintf("Isolated speed spike: jumped from %.1f to %.1f kts and returned to %.1f kts", sPrev, spd, sNext)
					prevSpd = sPrev
					deltaV = spd - sPrev
					dtSec = dtPrev
					accel = deltaV / dtSec
				}
			}
		}

		if isGlitch {
			glitchSet[i] = true
			glitches = append(glitches, GlitchRecord{
				Index:                 i,
				Time:                  pts[i].TimeStr,
				Latitude:              pts[i].Lat,
				Longitude:             pts[i].Lon,
				ReportedSpeedKts:      math.Round(spd*10) / 10,
				PreviousSpeedKts:      math.Round(prevSpd*10) / 10,
				DeltaSpeedKts:         math.Round(deltaV*10) / 10,
				DeltaTimeSec:          math.Round(dtSec*10) / 10,
				AccelerationKtsPerSec: math.Round(accel*100) / 100,
				Reason:                reason,
			})
		} else {
			lastValidIdx = i
		}
	}

	// Calculate clean metrics
	var cleanMaxSpeed float64
	var cleanSpeedSum float64
	var cleanSpeedCount int
	for i, pt := range pts {
		if glitchSet[i] || pt.SpeedKts == nil {
			continue
		}
		s := *pt.SpeedKts
		if s > cleanMaxSpeed {
			cleanMaxSpeed = s
		}
		cleanSpeedSum += s
		cleanSpeedCount++
	}

	var cleanAvgSpeed float64
	if cleanSpeedCount > 0 {
		cleanAvgSpeed = cleanSpeedSum / float64(cleanSpeedCount)
	}

	// Generate sanitized GPX XML if input was XML
	var filteredGPX string
	if parsedDoc != nil {
		cleanDoc := *parsedDoc
		// Filter tracks
		var newTracks []gpxTrack
		for tIdx, trk := range cleanDoc.Tracks {
			newTrk := gpxTrack{Name: trk.Name}
			for sIdx, seg := range trk.Segments {
				var newPts []gpxPoint
				for pIdx, pt := range seg.TrackPoints {
					ptIdx := -1
					for idx, ip := range pts {
						if ip.SourceDoc == "trk" && ip.TrkIdx == tIdx && ip.SegIdx == sIdx && ip.PtIdx == pIdx {
							ptIdx = idx
							break
						}
					}
					if ptIdx >= 0 && glitchSet[ptIdx] {
						continue
					}
					newPts = append(newPts, pt)
				}
				if len(newPts) > 0 {
					newTrk.Segments = append(newTrk.Segments, gpxSegment{TrackPoints: newPts})
				}
			}
			if len(newTrk.Segments) > 0 {
				newTracks = append(newTracks, newTrk)
			}
		}
		cleanDoc.Tracks = newTracks

		if bytesOut, err := xml.MarshalIndent(cleanDoc, "", "  "); err == nil {
			filteredGPX = xml.Header + string(bytesOut)
		}
	}

	validCount := len(pts) - len(glitches)
	cleanMaxRounded := math.Round(cleanMaxSpeed*10) / 10
	cleanAvgRounded := math.Round(cleanAvgSpeed*10) / 10
	rawMaxRounded := math.Round(rawMaxSpeed*10) / 10
	rawAvgRounded := math.Round(rawAvgSpeed*10) / 10

	var summary string
	if len(glitches) == 0 {
		summary = fmt.Sprintf("GPX analysis complete: %d points verified with no speed glitches. Maximum SOG: %.1f kts, Average SOG: %.1f kts.", len(pts), cleanMaxRounded, cleanAvgRounded)
	} else {
		summary = fmt.Sprintf("GPX filtering complete: %d points analyzed, %d GPS speed glitches filtered out (raw peak %.1f kts rejected). Clean Maximum SOG: %.1f kts, Clean Average SOG: %.1f kts.", len(pts), len(glitches), rawMaxRounded, cleanMaxRounded, cleanAvgRounded)
	}

	return &FilterGPXResponse{
		CleanMaxSpeedKts: cleanMaxRounded,
		CleanAvgSpeedKts: cleanAvgRounded,
		RawMaxSpeedKts:   rawMaxRounded,
		RawAvgSpeedKts:   rawAvgRounded,
		TotalPoints:      len(pts),
		ValidPoints:      validCount,
		GlitchesCount:    len(glitches),
		Glitches:         glitches,
		FilteredGPX:      filteredGPX,
		Summary:          summary,
	}, nil
}

func haversineDistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusNM * c
}

func parseGPXTime(timeStr string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, timeStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time string: %s", timeStr)
}
