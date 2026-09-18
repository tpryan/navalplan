package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"app/internal/gpx"
	"app/internal/model"
)

// UploadVoyageTrack handles uploading and parsing a GPX file for a whole voyage.
func (h *Handler) UploadVoyageTrack(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	data, fileName, kindOverride, autoSplit, err := extractGPXPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	parsedTracks, err := gpx.ParseGPX(data, kindOverride)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse GPX: %v", err))
		return
	}

	stops, _ := h.DB.ListStops(r.Context(), voyageID, 100, 0)
	rawGPXStr := string(data)

	var savedTracks []model.VoyageTrack

	for _, pt := range parsedTracks {
		var legsToSave []gpx.LegResult
		if autoSplit && len(stops) >= 2 {
			legsToSave = gpx.SplitTrackByStops(pt, stops)
		}

		if len(legsToSave) > 1 {
			// Save the master track as well
			masterTrack := buildModelTrack(voyageID, nil, fileName, rawGPXStr, pt)
			if err := h.DB.CreateVoyageTrack(r.Context(), &masterTrack); err != nil {
				slog.ErrorContext(r.Context(), "Failed to save master track", "error", err)
			} else {
				savedTracks = append(savedTracks, masterTrack)
			}

			// Save individual legs
			for _, leg := range legsToSave {
				mTrack := buildModelTrack(voyageID, leg.StopID, fileName, "", leg.Track)
				if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
					slog.ErrorContext(r.Context(), "Failed to save leg track", "error", err)
					continue
				}
				savedTracks = append(savedTracks, mTrack)
			}
		} else {
			mTrack := buildModelTrack(voyageID, nil, fileName, rawGPXStr, pt)
			if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
				slog.ErrorContext(r.Context(), "Failed to save track", "error", err)
				writeError(w, http.StatusInternalServerError, "Failed to save track")
				return
			}
			savedTracks = append(savedTracks, mTrack)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(savedTracks)
}

// UploadStopTrack handles uploading and parsing a GPX file for a specific stop/leg.
func (h *Handler) UploadStopTrack(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	stopID, err := strconv.ParseInt(r.PathValue("stopId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Stop ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	stop, err := h.DB.GetStop(r.Context(), stopID)
	if err != nil || stop.VoyageID != voyageID {
		writeError(w, http.StatusNotFound, "Stop not found for this voyage")
		return
	}

	data, fileName, kindOverride, _, err := extractGPXPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	parsedTracks, err := gpx.ParseGPX(data, kindOverride)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse GPX: %v", err))
		return
	}

	rawGPXStr := string(data)
	var savedTracks []model.VoyageTrack

	for _, pt := range parsedTracks {
		mTrack := buildModelTrack(voyageID, &stopID, fileName, rawGPXStr, pt)
		if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
			slog.ErrorContext(r.Context(), "Failed to save stop track", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to save stop track")
			return
		}
		savedTracks = append(savedTracks, mTrack)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(savedTracks)
}

// ListVoyageTracks returns all tracks associated with a voyage.
func (h *Handler) ListVoyageTracks(w http.ResponseWriter, r *http.Request) {
	voyageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	person := GetPersonFromContext(r.Context())
	if !voyage.IsPublic && (person == nil || voyage.PersonID != person.ID) {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	tracks, err := h.DB.ListVoyageTracks(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list voyage tracks", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list tracks")
		return
	}

	if tracks == nil {
		tracks = []model.VoyageTrack{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tracks)
}

// GetPublicVoyageTracks retrieves tracks for a public voyage using the share token.
func (h *Handler) GetPublicVoyageTracks(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "Missing token")
		return
	}

	voyage, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil || voyage == nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	if !voyage.IsPublic {
		writeError(w, http.StatusForbidden, "Voyage is not public")
		return
	}

	tracks, err := h.DB.ListVoyageTracks(r.Context(), voyage.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list public tracks", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list tracks")
		return
	}

	if tracks == nil {
		tracks = []model.VoyageTrack{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tracks)
}

// DeleteVoyageTrack removes a track record.
func (h *Handler) DeleteVoyageTrack(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	trackID := r.PathValue("trackId")
	if trackID == "" {
		writeError(w, http.StatusBadRequest, "Missing Track ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	track, err := h.DB.GetVoyageTrack(r.Context(), trackID)
	if err != nil || track.VoyageID != voyageID {
		writeError(w, http.StatusNotFound, "Track not found")
		return
	}

	if err := h.DB.DeleteVoyageTrack(r.Context(), trackID); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete voyage track", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete track")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DebriefVoyageTrack triggers an AI after-action review comparing planned vs recorded tracks.
func (h *Handler) DebriefVoyageTrack(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	trackID := r.PathValue("trackId")
	if trackID == "" {
		writeError(w, http.StatusBadRequest, "Missing Track ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	track, err := h.DB.GetVoyageTrack(r.Context(), trackID)
	if err != nil || track.VoyageID != voyageID {
		writeError(w, http.StatusNotFound, "Track not found")
		return
	}
	if track.Kind == string(model.TrackKindPlanned) {
		writeError(w, http.StatusBadRequest, "Cannot debrief a planned route directly; debrief compares an actual recorded track against the planned route.")
		return
	}

	allTracks, _ := h.DB.ListVoyageTracks(r.Context(), voyageID)
	stops, _ := h.DB.ListStops(r.Context(), voyageID, 100, 0)
	debrief := h.generateTrackDebrief(r.Context(), track, allTracks, stops)
	debriefBytes, _ := json.Marshal(debrief)
	_ = h.DB.UpdateVoyageTrackDebrief(r.Context(), track.ID, model.RawJSON(debriefBytes))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debrief)
}

// DebriefAllVoyageTracks triggers an AI after-action review for all tracks of a voyage.
func (h *Handler) DebriefAllVoyageTracks(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	allTracks, err := h.DB.ListVoyageTracks(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list tracks for debrief", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list tracks")
		return
	}

	stops, _ := h.DB.ListStops(r.Context(), voyageID, 100, 0)
	debriefs := make([]*model.TrackDebrief, 0, len(allTracks))
	for i := range allTracks {
		track := &allTracks[i]
		if track.Kind == string(model.TrackKindPlanned) {
			continue
		}
		debrief := h.generateTrackDebrief(r.Context(), track, allTracks, stops)
		debriefBytes, _ := json.Marshal(debrief)
		_ = h.DB.UpdateVoyageTrackDebrief(r.Context(), track.ID, model.RawJSON(debriefBytes))
		debriefs = append(debriefs, debrief)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debriefs)
}

type plannedRouteMatch struct {
	plannedTrackID   *string
	plannedTrackName string
	plannedDist      float64
	plannedDuration  string
	voyageStopID     *int64
	startStopID      *int64
}

func extractTrackEndpoints(t *model.VoyageTrack) (startLat, startLng, endLat, endLng float64, ok bool) {
	if t == nil {
		return 0, 0, 0, 0, false
	}
	raw := t.SimplifiedGeoJSON
	if len(raw) == 0 {
		raw = t.GeoJSON
	}
	coords := extractCoordinates(raw)
	if len(coords) < 2 {
		if len(t.GeoJSON) > 0 && len(t.SimplifiedGeoJSON) > 0 {
			coords = extractCoordinates(t.GeoJSON)
		}
		if len(coords) < 2 {
			return 0, 0, 0, 0, false
		}
	}
	startPt := coords[0]
	endPt := coords[len(coords)-1]
	if len(startPt) < 2 || len(endPt) < 2 {
		return 0, 0, 0, 0, false
	}
	return startPt[1], startPt[0], endPt[1], endPt[0], true
}

func extractLegNumber(name string) int {
	re := regexp.MustCompile(`(?i)Leg\s*#?\s*(\d+)`)
	matches := re.FindStringSubmatch(name)
	if len(matches) > 1 {
		num, err := strconv.Atoi(matches[1])
		if err == nil {
			return num
		}
	}
	return 0
}

func findClosestStop(lat, lon float64, stops []model.Stop, maxDistNM float64) int {
	bestIdx := -1
	minDist := maxDistNM
	for i, s := range stops {
		d := gpx.CalculateHaversineNM(lat, lon, s.Latitude, s.Longitude)
		if d < minDist {
			minDist = d
			bestIdx = i
		}
	}
	return bestIdx
}

func formatHoursMins(hours float64) string {
	hrs := int(hours)
	mins := int((hours - float64(hrs)) * 60)
	return fmt.Sprintf("%02dh %02dm", hrs, mins)
}

// ResolveDebriefStartStop ensures that a debrief's StartStopID points to the starting stop of its passage leg,
// never the arrival/ending stop, and populates StopTitle matching the voyage stops.
func ResolveDebriefStartStop(d *model.TrackDebrief, t *model.VoyageTrack, stops []model.Stop) {
	if len(stops) == 0 {
		if d.StartStopID == nil && d.VoyageStopID != nil {
			d.StartStopID = d.VoyageStopID
		} else if d.StartStopID == nil && t != nil && t.VoyageStopID != nil {
			d.StartStopID = t.VoyageStopID
		}
		return
	}
	trackName := d.TrackName
	if trackName == "" && t != nil {
		trackName = t.Name
	}
	plannedName := d.PlannedTrackName
	foundStop := false
	// 1. Check for explicit leg number (e.g., "Leg 1", "Leg 2")
	legNum := extractLegNumber(trackName)
	if legNum == 0 && plannedName != "" {
		legNum = extractLegNumber(plannedName)
	}
	if legNum >= 1 && legNum <= len(stops)-1 {
		d.StartStopID = &stops[legNum-1].ID
		foundStop = true
	}
	// 2. Check if track name mentions starting stop location name (e.g. "Cowes to Newtown")
	if !foundStop {
		for i := 0; i < len(stops)-1; i++ {
			loc := stops[i].LocationName
			if loc != "" && (strings.Contains(trackName, loc) || (plannedName != "" && strings.Contains(plannedName, loc))) {
				d.StartStopID = &stops[i].ID
				foundStop = true
				break
			}
		}
	}
	// 3. Check track endpoints if track coordinates exist
	if !foundStop && t != nil {
		if startLat, startLng, _, _, ok := extractTrackEndpoints(t); ok {
			sIdx := findClosestStop(startLat, startLng, stops, 10.0)
			if sIdx >= 0 {
				if sIdx == len(stops)-1 && len(stops) >= 2 {
					sIdx = len(stops) - 2
				}
				d.StartStopID = &stops[sIdx].ID
				foundStop = true
			}
		}
	}
	// 4. Check existing StartStopID or VoyageStopID reference
	if !foundStop {
		refID := d.StartStopID
		if refID == nil {
			refID = d.VoyageStopID
		}
		if refID == nil && t != nil {
			refID = t.VoyageStopID
		}
		if refID != nil {
			for i, s := range stops {
				if s.ID == *refID {
					// If associated with the final stop, it was associated with arrival/destination; map to preceding starting stop
					if i == len(stops)-1 && i > 0 {
						d.StartStopID = &stops[i-1].ID
					} else {
						d.StartStopID = &stops[i].ID
					}
					foundStop = true
					break
				}
			}
		}
	}
	// 5. Default to the first stop
	if !foundStop && d.StartStopID == nil {
		d.StartStopID = &stops[0].ID
	}
	// Populate StopTitle based on resolved StartStopID
	if d.StartStopID != nil {
		for i, s := range stops {
			if s.ID == *d.StartStopID {
				if i < len(stops)-1 {
					d.StopTitle = fmt.Sprintf("%s to %s", s.LocationName, stops[i+1].LocationName)
				} else {
					d.StopTitle = s.LocationName
				}
				break
			}
		}
	}
	if d.StopTitle == "" {
		if len(stops) >= 2 {
			d.StopTitle = fmt.Sprintf("%s to %s", stops[0].LocationName, stops[1].LocationName)
		} else {
			d.StopTitle = stops[0].LocationName
		}
	}
}

func pairActualWithPlanned(actual *model.VoyageTrack, allTracks []model.VoyageTrack, stops []model.Stop) plannedRouteMatch {
	var plannedTracks []*model.VoyageTrack
	for i := range allTracks {
		if allTracks[i].Kind == string(model.TrackKindPlanned) {
			plannedTracks = append(plannedTracks, &allTracks[i])
		}
	}

	var matched *model.VoyageTrack
	aStartLat, aStartLng, aEndLat, aEndLng, aOk := extractTrackEndpoints(actual)

	// Strategy A: Match by VoyageStopID
	if actual.VoyageStopID != nil {
		for _, p := range plannedTracks {
			if p.VoyageStopID != nil && *p.VoyageStopID == *actual.VoyageStopID {
				matched = p
				break
			}
		}
	}

	// Strategy B: Match by Leg Number in track name
	if matched == nil {
		actLeg := extractLegNumber(actual.Name)
		if actLeg > 0 {
			for _, p := range plannedTracks {
				if extractLegNumber(p.Name) == actLeg {
					matched = p
					break
				}
			}
		}
	}

	// Strategy C: Match by Geographic Proximity of endpoints
	if matched == nil && len(plannedTracks) > 0 {
		aStartLat, aStartLng, aEndLat, aEndLng, aOk := extractTrackEndpoints(actual)
		if aOk {
			bestScore := 1e9
			var bestP *model.VoyageTrack
			for _, p := range plannedTracks {
				pStartLat, pStartLng, pEndLat, pEndLng, pOk := extractTrackEndpoints(p)
				if pOk {
					dStart := gpx.CalculateHaversineNM(aStartLat, aStartLng, pStartLat, pStartLng)
					dEnd := gpx.CalculateHaversineNM(aEndLat, aEndLng, pEndLat, pEndLng)
					if dStart <= 8.0 && dEnd <= 8.0 && (dStart+dEnd) < bestScore {
						bestScore = dStart + dEnd
						bestP = p
					}
				}
			}
			if bestP != nil {
				matched = bestP
			}
		}
	}

	// Strategy D: Index alignment if multiple planned tracks
	if matched == nil && len(plannedTracks) > 0 {
		if len(plannedTracks) == 1 {
			matched = plannedTracks[0]
		} else {
			var actualTracks []*model.VoyageTrack
			for i := range allTracks {
				if allTracks[i].Kind != string(model.TrackKindPlanned) {
					actualTracks = append(actualTracks, &allTracks[i])
				}
			}
			actIdx := -1
			for i, a := range actualTracks {
				if a.ID == actual.ID {
					actIdx = i
					break
				}
			}
			if actIdx >= 0 && actIdx < len(plannedTracks) {
				matched = plannedTracks[actIdx]
			}
		}
	}

	// Determine starting stop ID for this leg
	tempDebrief := model.TrackDebrief{
		TrackName:    actual.Name,
		VoyageStopID: actual.VoyageStopID,
	}
	if matched != nil {
		tempDebrief.PlannedTrackName = matched.Name
	}
	ResolveDebriefStartStop(&tempDebrief, actual, stops)
	startStopID := tempDebrief.StartStopID
	if startStopID == nil && len(stops) >= 2 {
		var actualTracks []*model.VoyageTrack
		for i := range allTracks {
			if allTracks[i].Kind != string(model.TrackKindPlanned) {
				actualTracks = append(actualTracks, &allTracks[i])
			}
		}
		actIdx := -1
		for i, a := range actualTracks {
			if a.ID == actual.ID {
				actIdx = i
				break
			}
		}
		if actIdx >= 0 && actIdx < len(stops)-1 {
			startStopID = &stops[actIdx].ID
		}
	}
	if startStopID == nil && len(stops) > 0 {
		startStopID = &stops[0].ID
	}

	if matched != nil {
		dist := 0.0
		if matched.DistanceNM != nil {
			dist = *matched.DistanceNM
		}
		dur := ""
		if matched.DurationInterval != nil {
			dur = *matched.DurationInterval
		}
		return plannedRouteMatch{
			plannedTrackID:   &matched.ID,
			plannedTrackName: matched.Name,
			plannedDist:      dist,
			plannedDuration:  dur,
			voyageStopID:     startStopID,
			startStopID:      startStopID,
		}
	}

	// Strategy E: Fallback to voyage stops
	if len(stops) >= 2 {
		if startStopID != nil {
			for i, s := range stops {
				if s.ID == *startStopID && i < len(stops)-1 {
					nextStop := stops[i+1]
					d := gpx.CalculateHaversineNM(s.Latitude, s.Longitude, nextStop.Latitude, nextStop.Longitude)
					name := fmt.Sprintf("Planned Route: %s to %s", s.LocationName, nextStop.LocationName)
					return plannedRouteMatch{
						plannedTrackName: name,
						plannedDist:      d,
						voyageStopID:     startStopID,
						startStopID:      startStopID,
					}
				}
			}
		}
	}

	// Final Fallback: Direct rhumb line between actual track endpoints
	if aOk {
		d := gpx.CalculateHaversineNM(aStartLat, aStartLng, aEndLat, aEndLng)
		if d > 0.1 {
			return plannedRouteMatch{
				plannedTrackName: "Direct Rhumb Line Course",
				plannedDist:      d,
				voyageStopID:     startStopID,
				startStopID:      startStopID,
			}
		}
	}

	// Last resort fallback
	recDist := 0.0
	if actual.DistanceNM != nil {
		recDist = *actual.DistanceNM
	}
	return plannedRouteMatch{
		plannedTrackName: "Direct Rhumb Line Course",
		plannedDist:      recDist * 0.88,
		voyageStopID:     startStopID,
		startStopID:      startStopID,
	}
}

func (h *Handler) generateTrackDebrief(ctx context.Context, track *model.VoyageTrack, allTracks []model.VoyageTrack, stops []model.Stop) *model.TrackDebrief {
	match := pairActualWithPlanned(track, allTracks, stops)

	recDist := 0.0
	if track.DistanceNM != nil {
		recDist = *track.DistanceNM
	}
	plannedDist := match.plannedDist
	distDelta := recDist - plannedDist
	pctOver := 0.0
	if plannedDist > 0 {
		pctOver = (distDelta / plannedDist) * 100.0
	}

	recDur := "N/A"
	if track.DurationInterval != nil {
		recDur = *track.DurationInterval
	}
	plannedDuration := match.plannedDuration
	if plannedDuration == "" {
		if plannedDist > 0 {
			plannedDuration = formatHoursMins(plannedDist / 5.5)
		} else {
			plannedDuration = recDur
		}
	}

	avgSpd := 0.0
	if track.AvgSpeedKts != nil {
		avgSpd = *track.AvgSpeedKts
	}
	maxSpd := 0.0
	if track.MaxSpeedKts != nil {
		maxSpd = *track.MaxSpeedKts
	}

	var stopTitle string
	if match.startStopID != nil && len(stops) > 0 {
		for i, s := range stops {
			if s.ID == *match.startStopID {
				if i < len(stops)-1 {
					stopTitle = fmt.Sprintf("%s to %s", s.LocationName, stops[i+1].LocationName)
				} else {
					stopTitle = s.LocationName
				}
				break
			}
		}
	}
	if stopTitle == "" && len(stops) >= 2 {
		stopTitle = fmt.Sprintf("%s to %s", stops[0].LocationName, stops[1].LocationName)
	} else if stopTitle == "" && len(stops) == 1 {
		stopTitle = stops[0].LocationName
	}

	debrief := model.TrackDebrief{
		TrackID:             track.ID,
		TrackName:           track.Name,
		StopTitle:           stopTitle,
		PlannedTrackID:      match.plannedTrackID,
		PlannedTrackName:    match.plannedTrackName,
		VoyageStopID:        match.startStopID,
		StartStopID:         match.startStopID,
		RecordedDistanceNM:  recDist,
		PlannedDistanceNM:   plannedDist,
		DistanceDeltaNM:     distDelta,
		DistanceVariancePct: pctOver,
		RecordedDuration:    recDur,
		PlannedDuration:     plannedDuration,
		AvgSpeedKts:         avgSpd,
		MaxSpeedKts:         maxSpd,
	}

	// Generate tactical insights using Pilot agent or rule-based fallback
	prompt := fmt.Sprintf(
		"Perform a post-voyage tactical pilot debrief comparing the ACTUAL recorded passage against the PLANNED route:\n"+
			"- Actual Track: '%s'\n"+
			"- Paired Planned Route: '%s'\n"+
			"- Recorded Distance: %.2f NM vs Planned Distance: %.2f NM (Delta: %+.2f NM, %+.1f%% variance)\n"+
			"- Recorded Duration: %s vs Planned Duration: %s\n"+
			"- Vessel Speed: Average SOG %.1f kts, Maximum SOG %.1f kts\n\n"+
			"Compare actual execution directly to the planned route and provide:\n"+
			"1. Summary: 2-3 sentence overview comparing actual vs planned.\n"+
			"2. Conclusions: In-depth conclusions explaining variances in distance, time, and tactical choices.\n"+
			"3. Tacking Efficiency: Analysis of tacking overhead and leeway against the plan.\n"+
			"4. Weather Impact: Atmospheric and sea state factors.\n"+
			"5. Observations: 3-5 concrete tactical takeaways for the skipper.",
		track.Name, match.plannedTrackName, recDist, plannedDist, distDelta, pctOver, recDur, plannedDuration, avgSpd, maxSpd,
	)

	if h.Agent != nil {
		agentSessionID := fmt.Sprintf("debrief_%s_%d", track.ID, time.Now().UnixNano())
		resp, err := h.Agent.RunSync(ctx, "pilot", "system", agentSessionID, prompt)
		if err == nil && resp != "" {
			var parsedResp struct {
				Summary           string   `json:"summary"`
				Conclusions       string   `json:"conclusions"`
				TackingEfficiency string   `json:"tacking_efficiency"`
				WeatherImpact     string   `json:"weather_impact"`
				Observations      []string `json:"observations"`
			}
			if err := json.Unmarshal([]byte(cleanJSON(resp)), &parsedResp); err == nil && parsedResp.Summary != "" {
				debrief.Summary = parsedResp.Summary
				debrief.Conclusions = parsedResp.Conclusions
				debrief.TackingEfficiency = parsedResp.TackingEfficiency
				debrief.WeatherImpact = parsedResp.WeatherImpact
				debrief.Observations = parsedResp.Observations
			} else {
				debrief.Summary = resp
			}
		}
	}

	if debrief.Summary == "" {
		debrief.Summary = fmt.Sprintf(
			"Passage Debrief comparing actual '%s' against planned '%s': Sailed %.1f NM vs %.1f NM planned (%+.1f NM, %+.1f%% variance) in %s (avg speed %.1f kts, max %.1f kts).",
			track.Name, match.plannedTrackName, recDist, plannedDist, distDelta, pctOver, recDur, avgSpd, maxSpd,
		)
	}

	if debrief.Conclusions == "" {
		if distDelta > 1.0 {
			debrief.Conclusions = fmt.Sprintf(
				"Actual passage '%s' required %.1f NM compared to the planned %.1f NM for '%s' (%+.1f%% variance). Slower transit time (%s vs %s planned) was driven by windward tacking angles and navigational leeway along the leg. SOG averaged %.1f kts (peak %.1f kts).",
				track.Name, recDist, plannedDist, match.plannedTrackName, pctOver, recDur, plannedDuration, avgSpd, maxSpd,
			)
		} else if distDelta < -0.5 {
			debrief.Conclusions = fmt.Sprintf(
				"The vessel completed '%s' in %.1f NM, cutting %.1f NM off the planned %.1f NM route for '%s'. A direct course was maintained during favorable wind angles, finishing in %s with an average SOG of %.1f knots.",
				track.Name, recDist, -distDelta, plannedDist, match.plannedTrackName, recDur, avgSpd,
			)
		} else {
			debrief.Conclusions = fmt.Sprintf(
				"The actual track '%s' closely tracked the planned route '%s' (%.1f NM actual vs %.1f NM planned, %+.1f%% variance). The passage was executed with high navigational discipline, finishing in %s at an average SOG of %.1f kts.",
				track.Name, match.plannedTrackName, recDist, plannedDist, pctOver, recDur, avgSpd,
			)
		}
	}

	if debrief.TackingEfficiency == "" {
		if distDelta > 0.5 {
			debrief.TackingEfficiency = fmt.Sprintf(
				"Tacking overhead and course corrections added %.1f NM (%.1f%% extra distance) over the planned course '%s'.",
				distDelta, pctOver, match.plannedTrackName,
			)
		} else {
			debrief.TackingEfficiency = fmt.Sprintf(
				"Direct course steered with minimal tacking overhead (%+.1f NM over plan '%s').",
				distDelta, match.plannedTrackName,
			)
		}
	}

	if debrief.WeatherImpact == "" {
		debrief.WeatherImpact = fmt.Sprintf("Conditions along the leg allowed an average speed of %.1f kts with peak velocity of %.1f kts.", avgSpd, maxSpd)
	}

	if len(debrief.Observations) == 0 {
		debrief.Observations = []string{
			fmt.Sprintf("Recorded %s under way, compared to %s planned.", recDur, plannedDuration),
			fmt.Sprintf("Average speed over ground maintained at %.1f knots with peak velocity of %.1f knots.", avgSpd, maxSpd),
			fmt.Sprintf("Course deviation vs plan: %+.1f NM (%+.1f%% distance variance).", distDelta, pctOver),
			"Tactical takeaway: review upwind VMG angles to optimize tacking efficiency on similar legs.",
		}
	}

	return &debrief
}

func extractGPXPayload(r *http.Request) ([]byte, string, string, bool, error) {
	contentType := r.Header.Get("Content-Type")
	var data []byte
	var fileName string
	var kindOverride string
	autoSplit := true

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, "", "", false, fmt.Errorf("failed to parse multipart form: %w", err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, "", "", false, fmt.Errorf("missing 'file' field in multipart form")
		}
		defer file.Close()
		fileName = header.Filename
		data, err = io.ReadAll(file)
		if err != nil {
			return nil, "", "", false, fmt.Errorf("failed to read file: %w", err)
		}

		if k := r.FormValue("kind"); k != "" {
			kindOverride = k
		}
		if as := r.FormValue("auto_split"); as == "false" || as == "0" {
			autoSplit = false
		}
	} else {
		var err error
		data, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, "", "", false, fmt.Errorf("failed to read request body: %w", err)
		}
		if k := r.URL.Query().Get("kind"); k != "" {
			kindOverride = k
		}
		if as := r.URL.Query().Get("auto_split"); as == "false" || as == "0" {
			autoSplit = false
		}
		fileName = "track.gpx"
	}

	if len(data) == 0 {
		return nil, "", "", false, fmt.Errorf("empty GPX file payload")
	}

	return data, fileName, kindOverride, autoSplit, nil
}

func buildModelTrack(voyageID int64, stopID *int64, fileName, rawGPX string, pt gpx.ParsedTrack) model.VoyageTrack {
	var fn *string
	if fileName != "" {
		fn = &fileName
	}
	var raw *string
	if rawGPX != "" {
		raw = &rawGPX
	}
	var dur *string
	if pt.DurationInterval != "" {
		dur = &pt.DurationInterval
	}

	return model.VoyageTrack{
		VoyageID:          voyageID,
		VoyageStopID:      stopID,
		Kind:              pt.Kind,
		Name:              pt.Name,
		FileName:          fn,
		StartTime:         pt.StartTime,
		EndTime:           pt.EndTime,
		DistanceNM:        &pt.DistanceNM,
		DurationInterval:  dur,
		MaxSpeedKts:       &pt.MaxSpeedKts,
		AvgSpeedKts:       &pt.AvgSpeedKts,
		GeoJSON:           model.RawJSON(pt.GeoJSON),
		SimplifiedGeoJSON: model.RawJSON(pt.SimplifiedGeoJSON),
		RawGPX:            raw,
	}
}
