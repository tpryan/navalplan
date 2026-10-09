package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
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

var (
	durColonRegex = regexp.MustCompile(`^(\d+):(\d{2})(?::(\d{2}))?$`)
	durHoursRegex = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:h|hr|hrs|hour|hours)\b`)
	durMinsRegex  = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:m|min|mins|minute|minutes)\b`)
	durSecsRegex  = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds)\b`)
)

func ParseDurationMinutes(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0, false
	}
	if m := durColonRegex.FindStringSubmatch(s); len(m) > 0 {
		hrs, _ := strconv.ParseFloat(m[1], 64)
		mins, _ := strconv.ParseFloat(m[2], 64)
		secs := 0.0
		if len(m) > 3 && m[3] != "" {
			secs, _ = strconv.ParseFloat(m[3], 64)
		}
		return hrs*60.0 + mins + secs/60.0, true
	}
	totalMins := 0.0
	found := false
	if m := durHoursRegex.FindStringSubmatch(s); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			totalMins += v * 60.0
			found = true
		}
	}
	if m := durMinsRegex.FindStringSubmatch(s); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			totalMins += v
			found = true
		}
	}
	if m := durSecsRegex.FindStringSubmatch(s); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			totalMins += v / 60.0
			found = true
		}
	}
	return totalMins, found
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
	// 2. Check track endpoints if track coordinates exist (GPS leg pair matching)
	if !foundStop && t != nil {
		if startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(t); ok {
			bestLeg := -1
			bestScore := 1e9
			for i := 0; i < len(stops)-1; i++ {
				dStart := gpx.CalculateHaversineNM(startLat, startLng, stops[i].Latitude, stops[i].Longitude)
				dEnd := gpx.CalculateHaversineNM(endLat, endLng, stops[i+1].Latitude, stops[i+1].Longitude)
				if dStart <= 15.0 && dEnd <= 15.0 {
					score := dStart + dEnd
					if t.StartTime != nil && !stops[i].TargetDate.IsZero() {
						if stops[i].TargetDate.Format("2006-01-02") == t.StartTime.Format("2006-01-02") {
							score -= 5.0
						}
					}
					if score < bestScore {
						bestScore = score
						bestLeg = i
					}
				}
			}
			if bestLeg >= 0 {
				d.StartStopID = &stops[bestLeg].ID
				foundStop = true
			}
		}
	}
	// 3. Check if track name mentions starting stop location name (e.g. "Cowes to Newtown")
	if !foundStop {
		nameForLocationMatch := trackName
		if plannedName != "" {
			nameForLocationMatch = trackName + " " + plannedName
		}
		depPart := nameForLocationMatch
		if parts := strings.Split(nameForLocationMatch, " to "); len(parts) > 1 {
			depPart = parts[0]
		} else if parts := strings.Split(nameForLocationMatch, " - "); len(parts) > 1 {
			depPart = parts[0]
		}
		for i := 0; i < len(stops)-1; i++ {
			loc := stops[i].LocationName
			if loc != "" {
				if strings.Contains(depPart, loc) || (depPart == nameForLocationMatch && strings.Contains(nameForLocationMatch, loc)) {
					d.StartStopID = &stops[i].ID
					foundStop = true
					break
				}
			}
		}
	}
	// 4. Fallback to closest single stop by coordinates
	if !foundStop && t != nil {
		if startLat, startLng, _, _, ok := extractTrackEndpoints(t); ok {
			sIdx := findClosestStop(startLat, startLng, stops, 10.0)
			if sIdx >= 0 {
				if sIdx == len(stops)-1 && len(stops) >= 2 {
					dFirst := gpx.CalculateHaversineNM(startLat, startLng, stops[0].Latitude, stops[0].Longitude)
					dLast := gpx.CalculateHaversineNM(startLat, startLng, stops[len(stops)-1].Latitude, stops[len(stops)-1].Longitude)
					if math.Abs(dFirst-dLast) < 2.0 {
						if t.StartTime != nil && !stops[0].TargetDate.IsZero() && stops[0].TargetDate.Format("2006-01-02") == t.StartTime.Format("2006-01-02") {
							sIdx = 0
						} else {
							sIdx = len(stops) - 2
						}
					} else {
						sIdx = len(stops) - 2
					}
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
					// unless start time matches the first stop in a circuit
					if i == len(stops)-1 && i > 0 {
						if t != nil && t.StartTime != nil && !stops[0].TargetDate.IsZero() && stops[0].TargetDate.Format("2006-01-02") == t.StartTime.Format("2006-01-02") {
							d.StartStopID = &stops[0].ID
						} else {
							d.StartStopID = &stops[i-1].ID
						}
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

	var durVarPct *float64
	var durDelta string
	if recMins, okRec := ParseDurationMinutes(recDur); okRec {
		if planMins, okPlan := ParseDurationMinutes(plannedDuration); okPlan && planMins > 0 {
			diffMins := recMins - planMins
			pct := (diffMins / planMins) * 100.0
			durVarPct = &pct

			absDiff := math.Abs(diffMins)
			hrs := int(absDiff) / 60
			mins := int(math.Round(absDiff)) % 60
			deltaBody := ""
			if hrs > 0 && mins > 0 {
				deltaBody = fmt.Sprintf("%dh %dm", hrs, mins)
			} else if hrs > 0 {
				deltaBody = fmt.Sprintf("%dh", hrs)
			} else {
				deltaBody = fmt.Sprintf("%dm", mins)
			}
			if diffMins >= 0 {
				durDelta = "+" + deltaBody
			} else {
				durDelta = "-" + deltaBody
			}
		}
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
		DurationDelta:       durDelta,
		DurationVariancePct: durVarPct,
		AvgSpeedKts:         avgSpd,
		MaxSpeedKts:         maxSpd,
	}

	durComparison := fmt.Sprintf("- Recorded Duration: %s vs Planned Duration: %s", recDur, plannedDuration)
	if durVarPct != nil {
		durComparison += fmt.Sprintf(" (Delta: %s, %+.1f%% variance)", durDelta, *durVarPct)
	}

	// Generate tactical insights using Pilot agent or rule-based fallback
	prompt := fmt.Sprintf(
		"Perform a post-voyage tactical pilot debrief comparing the ACTUAL recorded passage against the PLANNED route:\n"+
			"- Actual Track: '%s'\n"+
			"- Paired Planned Route: '%s'\n"+
			"- Recorded Distance: %.2f NM vs Planned Distance: %.2f NM (Delta: %+.2f NM, %+.1f%% variance)\n"+
			"%s\n"+
			"- Vessel Speed: Average SOG %.1f kts, Maximum SOG %.1f kts\n\n"+
			"Guidelines:\n"+
			"- Be concise, direct, and tactical. Keep each section to 1-2 brief sentences.\n"+
			"- DO NOT repeat raw numbers, percentages, or statistics already displayed in the metric cards (e.g. do not restate exact distance, duration, or speed values).\n"+
			"- Focus on actionable tactical reasons: weather, leeway, tacking choices, and sea state.\n\n"+
			"Output JSON matching:\n"+
			"1. summary: Brief 1-2 sentence tactical overview of passage execution.\n"+
			"2. conclusions: 1-2 crisp sentences explaining why actual differed from plan.\n"+
			"3. tacking_efficiency: 1 sentence on maneuvers, tacking angles, or leeway.\n"+
			"4. weather_impact: 1 sentence on wind, sea state, or current effects.\n"+
			"5. observations: 2-3 short, bulleted tactical takeaways for the skipper.",
		track.Name, match.plannedTrackName, recDist, plannedDist, distDelta, pctOver, durComparison, avgSpd, maxSpd,
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
		if distDelta > 1.0 {
			debrief.Summary = fmt.Sprintf("Passage completed with extra distance sailed due to windward maneuvering and navigational course adjustments compared to '%s'.", match.plannedTrackName)
		} else if distDelta < -0.5 {
			debrief.Summary = fmt.Sprintf("Direct, efficient passage maintained on favorable angles compared to planned '%s'.", match.plannedTrackName)
		} else {
			debrief.Summary = fmt.Sprintf("Passage executed closely aligned with planned route '%s'.", match.plannedTrackName)
		}
	}

	if debrief.Conclusions == "" {
		if distDelta > 1.0 {
			debrief.Conclusions = "Upwind tacking angles and navigational leeway accounted for the extended passage time and distance."
		} else if distDelta < -0.5 {
			debrief.Conclusions = "Favorable wind direction and efficient helming cut down passage distance and transit time."
		} else {
			debrief.Conclusions = "High navigational discipline maintained throughout the leg with minimal course deviation."
		}
	}

	if debrief.TackingEfficiency == "" {
		if distDelta > 0.5 {
			debrief.TackingEfficiency = "Tacking overhead and course corrections added extra distance over the planned rhumb line."
		} else {
			debrief.TackingEfficiency = "Direct course steered with minimal tacking overhead."
		}
	}

	if debrief.WeatherImpact == "" {
		debrief.WeatherImpact = "Conditions allowed steady progress along the leg."
	}

	if len(debrief.Observations) == 0 {
		debrief.Observations = []string{
			"Review upwind VMG angles to optimize tacking overhead on similar legs.",
			"Verify current and tidal stream timing against planned departure windows.",
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
