package gpx

import (
	"fmt"
	"time"

	"app/internal/model"
)

// LegResult represents a track segment associated with a specific voyage day.
type LegResult struct {
	DayID *int64
	Track ParsedTrack
}

// SplitTrackByDays splits a master track into discrete legs based on voyage day start/end locations or stationary periods.
func SplitTrackByDays(master ParsedTrack, days []model.VoyageDay) []LegResult {
	if len(days) == 0 || len(master.Points) < 4 {
		return []LegResult{{Track: master}}
	}

	type dayMatch struct {
		dayIndex int
		startIdx int
		endIdx   int
	}

	var matches []dayMatch
	lastPointIdx := 0

	for dIdx, day := range days {
		if (day.StartLatitude == 0 && day.StartLongitude == 0) || (day.EndLatitude == 0 && day.EndLongitude == 0) {
			continue
		}
		startBest := -1
		startMinDist := 1e9
		for pIdx := lastPointIdx; pIdx < len(master.Points); pIdx++ {
			d := CalculateHaversineNM(day.StartLatitude, day.StartLongitude, master.Points[pIdx].Lat, master.Points[pIdx].Lng)
			if d < startMinDist {
				startMinDist = d
				startBest = pIdx
			}
		}
		if startBest == -1 || startMinDist > 5.0 {
			continue
		}

		endBest := -1
		endMinDist := 1e9
		for pIdx := startBest + 1; pIdx < len(master.Points); pIdx++ {
			d := CalculateHaversineNM(day.EndLatitude, day.EndLongitude, master.Points[pIdx].Lat, master.Points[pIdx].Lng)
			if d < endMinDist {
				endMinDist = d
				endBest = pIdx
			}
		}
		if endBest != -1 && endMinDist <= 5.0 && endBest > startBest {
			matches = append(matches, dayMatch{
				dayIndex: dIdx,
				startIdx: startBest,
				endIdx:   endBest,
			})
			lastPointIdx = endBest
		}
	}

	if len(matches) == 0 {
		return splitByStationaryPeriods(master)
	}

	var legs []LegResult
	for i, m := range matches {
		legPoints := master.Points[m.startIdx : m.endIdx+1]
		day := days[m.dayIndex]
		fromName := day.StartLocationName
		toName := day.EndLocationName
		if fromName == "" {
			fromName = "Start"
		}
		if toName == "" {
			toName = "End"
		}
		dayID := day.ID
		legName := fmt.Sprintf("%s - Leg %d: %s to %s", master.Name, i+1, fromName, toName)
		legTrack := buildParsedTrack(legName, master.Kind, legPoints)
		legs = append(legs, LegResult{
			DayID: &dayID,
			Track: legTrack,
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
