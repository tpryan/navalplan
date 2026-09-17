package gpx

import (
	"fmt"
	"time"

	"app/internal/model"
)

// LegResult represents a track segment associated with a specific voyage stop.
type LegResult struct {
	StopID *int64
	Track  ParsedTrack
}

// SplitTrackByStops splits a master track into discrete legs based on stop locations or stationary periods.
func SplitTrackByStops(master ParsedTrack, stops []model.Stop) []LegResult {
	if len(stops) < 2 || len(master.Points) < 4 {
		return []LegResult{{Track: master}}
	}

	// Find the closest point index in the track for each stop
	type stopMatch struct {
		stopIndex  int
		pointIndex int
		distNM     float64
	}

	var matches []stopMatch
	lastPointIdx := 0

	for sIdx, stop := range stops {
		if stop.Latitude == 0 && stop.Longitude == 0 {
			continue
		}
		bestIdx := -1
		minDist := 1e9

		// Search forward from lastPointIdx to preserve chronological progression
		for pIdx := lastPointIdx; pIdx < len(master.Points); pIdx++ {
			d := CalculateHaversineNM(stop.Latitude, stop.Longitude, master.Points[pIdx].Lat, master.Points[pIdx].Lng)
			if d < minDist {
				minDist = d
				bestIdx = pIdx
			}
		}

		// Consider a match if within 5.0 NM of the stop or closest approach
		if bestIdx != -1 && minDist <= 5.0 {
			matches = append(matches, stopMatch{
				stopIndex:  sIdx,
				pointIndex: bestIdx,
				distNM:     minDist,
			})
			lastPointIdx = bestIdx
		}
	}

	// If we couldn't match at least 2 distinct stop locations with forward progress, check for stationary periods
	if len(matches) < 2 {
		return splitByStationaryPeriods(master)
	}

	var legs []LegResult
	for i := 0; i < len(matches)-1; i++ {
		startPt := matches[i].pointIndex
		endPt := matches[i+1].pointIndex
		if endPt <= startPt {
			continue
		}

		legPoints := master.Points[startPt : endPt+1]
		fromName := stops[matches[i].stopIndex].LocationName
		toName := stops[matches[i+1].stopIndex].LocationName
		destStopID := stops[matches[i+1].stopIndex].ID

		legName := fmt.Sprintf("%s - Leg %d: %s to %s", master.Name, i+1, fromName, toName)
		legTrack := buildParsedTrack(legName, master.Kind, legPoints)
		legs = append(legs, LegResult{
			StopID: &destStopID,
			Track:  legTrack,
		})
	}

	if len(legs) == 0 {
		return []LegResult{{Track: master}}
	}

	return legs
}

func splitByStationaryPeriods(master ParsedTrack) []LegResult {
	var splitIndices []int
	const minStopGap = 2 * time.Hour

	for i := 1; i < len(master.Points); i++ {
		t1 := master.Points[i-1].Time
		t2 := master.Points[i].Time
		if t1 != nil && t2 != nil {
			gap := t2.Sub(*t1)
			dist := CalculateHaversineNM(master.Points[i-1].Lat, master.Points[i-1].Lng, master.Points[i].Lat, master.Points[i].Lng)
			// Stationary break: time gap > 2 hours and negligible movement (< 0.5 NM), or gap > 6 hours (e.g., GPS powered down overnight)
			if (gap >= minStopGap && dist < 0.5) || gap >= 6*time.Hour {
				splitIndices = append(splitIndices, i)
			}
		}
	}

	if len(splitIndices) == 0 {
		return []LegResult{{Track: master}}
	}

	var legs []LegResult
	start := 0
	for legNum, splitIdx := range splitIndices {
		if splitIdx-start >= 2 {
			pts := master.Points[start:splitIdx]
			name := fmt.Sprintf("%s - Leg %d", master.Name, legNum+1)
			legs = append(legs, LegResult{
				Track: buildParsedTrack(name, master.Kind, pts),
			})
		}
		start = splitIdx
	}
	if len(master.Points)-start >= 2 {
		pts := master.Points[start:]
		name := fmt.Sprintf("%s - Leg %d", master.Name, len(legs)+1)
		legs = append(legs, LegResult{
			Track: buildParsedTrack(name, master.Kind, pts),
		})
	}

	if len(legs) == 0 {
		return []LegResult{{Track: master}}
	}

	return legs
}
