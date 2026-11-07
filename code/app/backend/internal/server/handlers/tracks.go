package handlers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
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

	if cleanData, glitches, err := gpx.FilterGPX(data); err == nil && glitches > 0 {
		slog.InfoContext(r.Context(), "Filtered GPS glitches from uploaded GPX",
			"voyage_id", voyageID,
			"glitches_removed", glitches,
		)
		data = cleanData
	}

	parsedTracks, err := gpx.ParseGPX(data, kindOverride)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse GPX: %v", err))
		return
	}

	days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)
	rawGPXStr := string(data)

	var savedTracks []model.VoyageTrack

	for _, pt := range parsedTracks {
		var legsToSave []gpx.LegResult
		if autoSplit && len(days) >= 1 {
			legsToSave = gpx.SplitTrackByDays(pt, days)
		}

		if len(legsToSave) > 1 {
			masterTrack := buildModelTrack(voyageID, nil, fileName, rawGPXStr, pt)
			if err := h.DB.CreateVoyageTrack(r.Context(), &masterTrack); err != nil {
				slog.ErrorContext(r.Context(), "Failed to save master track",
					"error", err,
					"name", masterTrack.Name,
					"distance_nm", masterTrack.DistanceNM,
					"max_speed_kts", masterTrack.MaxSpeedKts,
					"avg_speed_kts", masterTrack.AvgSpeedKts,
				)
			} else {
				savedTracks = append(savedTracks, masterTrack)
			}

			for _, leg := range legsToSave {
				mTrack := buildModelTrack(voyageID, leg.DayID, fileName, "", leg.Track)
				if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
					slog.ErrorContext(r.Context(), "Failed to save leg track",
						"error", err,
						"name", mTrack.Name,
						"distance_nm", mTrack.DistanceNM,
						"max_speed_kts", mTrack.MaxSpeedKts,
						"avg_speed_kts", mTrack.AvgSpeedKts,
					)
					continue
				}
				savedTracks = append(savedTracks, mTrack)
			}
		} else {
			var assignedDayID *int64
			if len(legsToSave) == 1 && legsToSave[0].DayID != nil {
				assignedDayID = legsToSave[0].DayID
			}
			mTrack := buildModelTrack(voyageID, assignedDayID, fileName, rawGPXStr, pt)
			if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
				slog.ErrorContext(r.Context(), "Failed to save track",
					"error", err,
					"name", mTrack.Name,
					"distance_nm", mTrack.DistanceNM,
					"max_speed_kts", mTrack.MaxSpeedKts,
					"avg_speed_kts", mTrack.AvgSpeedKts,
					"duration", mTrack.DurationInterval,
				)
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

// UploadDayTrack handles uploading and parsing a GPX file for a specific voyage day.
func (h *Handler) UploadDayTrack(w http.ResponseWriter, r *http.Request) {
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

	dayIDStr := r.PathValue("dayId")
	if dayIDStr == "" {
		dayIDStr = r.PathValue("stopId")
	}
	dayID, err := strconv.ParseInt(dayIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Day ID")
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

	day, err := h.DB.GetDay(r.Context(), dayID)
	if err != nil || day.VoyageID != voyageID {
		writeError(w, http.StatusNotFound, "Day not found for this voyage")
		return
	}

	data, fileName, kindOverride, _, err := extractGPXPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if cleanData, glitches, err := gpx.FilterGPX(data); err == nil && glitches > 0 {
		slog.InfoContext(r.Context(), "Filtered GPS glitches from uploaded day GPX",
			"voyage_id", voyageID,
			"day_id", dayID,
			"glitches_removed", glitches,
		)
		data = cleanData
	}

	parsedTracks, err := gpx.ParseGPX(data, kindOverride)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse GPX: %v", err))
		return
	}

	rawGPXStr := string(data)
	var savedTracks []model.VoyageTrack

	for _, pt := range parsedTracks {
		mTrack := buildModelTrack(voyageID, &dayID, fileName, rawGPXStr, pt)
		if err := h.DB.CreateVoyageTrack(r.Context(), &mTrack); err != nil {
			slog.ErrorContext(r.Context(), "Failed to save day track", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to save day track")
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
	tracks = h.deduplicateVoyageTracks(r.Context(), tracks, true)
	if len(tracks) > 0 {
		days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)
		tracks = h.sanitizeTrackComparisonDebriefs(r.Context(), tracks, days, true)
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
	tracks = h.deduplicateVoyageTracks(r.Context(), tracks, false)
	if len(tracks) > 0 {
		days, _ := h.DB.ListDays(r.Context(), voyage.ID, 100, 0)
		tracks = h.sanitizeTrackComparisonDebriefs(r.Context(), tracks, days, false)
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

// UpdateVoyageTrack updates the name of an existing voyage track.
func (h *Handler) UpdateVoyageTrack(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || track == nil || track.VoyageID != voyageID {
		writeError(w, http.StatusNotFound, "Track not found")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Track name cannot be empty")
		return
	}

	if err := h.DB.UpdateVoyageTrackName(r.Context(), track.ID, name); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update voyage track name", "track_id", track.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update track name")
		return
	}

	track.Name = name
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(track)
}

// DebriefVoyageTrack triggers an AI after-action review comparing planned vs recorded tracks,
// or comparing multiple candidate planned routes when invoked on a planned track.
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

	allTracks, _ := h.DB.ListVoyageTracks(r.Context(), voyageID)
	days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)

	if track.Kind == string(model.TrackKindPlanned) {
		group := findCandidateGroupForTrack(track, allTracks, days)
		if len(group.tracks) < 2 {
			writeError(w, http.StatusBadRequest, "Cannot debrief a single planned route directly; upload at least two planned routes to compare or a recorded track to debrief.")
			return
		}
		debrief := h.generateMultiPlanComparison(r.Context(), group.tracks, group.day, days)
		if debrief == nil {
			writeError(w, http.StatusBadRequest, "Failed to compare planned routes.")
			return
		}
		h.persistComparisonDebrief(r.Context(), debrief, group.tracks)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(debrief)
		return
	}

	debrief := h.generateTrackDebrief(r.Context(), track, allTracks, days)
	if debrief == nil {
		writeError(w, http.StatusBadRequest, "Debrief requires both a planned route and an actual recorded track.")
		return
	}
	debriefBytes, _ := json.Marshal(debrief)
	_ = h.DB.UpdateVoyageTrackDebrief(r.Context(), track.ID, model.RawJSON(debriefBytes))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debrief)
}

// DebriefAllVoyageTracks triggers an AI after-action review for all tracks of a voyage,
// including multi-plan route comparisons when 2 or more candidate planned routes exist.
func (h *Handler) DebriefAllVoyageTracks(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

	allTracks, err := h.DB.ListVoyageTracks(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list tracks for debrief", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list tracks")
		return
	}

	plannedCount := 0
	hasActual := false
	for _, t := range allTracks {
		if t.Kind == string(model.TrackKindPlanned) {
			plannedCount++
		} else {
			hasActual = true
		}
	}
	hasPlanned := plannedCount > 0
	if (!hasPlanned || !hasActual) && plannedCount < 2 {
		writeError(w, http.StatusBadRequest, "Debrief requires both a planned route and an actual recorded track, or at least two planned routes to compare.")
		return
	}

	days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)
	debriefs := make([]*model.TrackDebrief, 0, len(allTracks))

	if hasPlanned && hasActual {
		for i := range allTracks {
			track := &allTracks[i]
			if track.Kind == string(model.TrackKindPlanned) {
				continue
			}
			debrief := h.generateTrackDebrief(r.Context(), track, allTracks, days)
			if debrief == nil {
				continue
			}
			debriefBytes, err := json.Marshal(debrief)
			if err != nil {
				slog.ErrorContext(r.Context(), "Failed to marshal track debrief", "track_id", track.ID, "error", err)
				continue
			}
			if err := h.DB.UpdateVoyageTrackDebrief(r.Context(), track.ID, model.RawJSON(debriefBytes)); err != nil {
				slog.ErrorContext(r.Context(), "Failed to persist track debrief", "track_id", track.ID, "error", err)
				continue
			}
			debriefs = append(debriefs, debrief)
		}
	}

	if plannedCount >= 2 {
		groups := groupCandidatePlannedTracks(allTracks, days)
		for _, grp := range groups {
			if len(grp.tracks) < 2 {
				continue
			}
			compDebrief := h.generateMultiPlanComparison(r.Context(), grp.tracks, grp.day, days)
			if compDebrief == nil {
				continue
			}
			h.persistComparisonDebrief(r.Context(), compDebrief, grp.tracks)
			debriefs = append(debriefs, compDebrief)
		}
	}

	if len(debriefs) == 0 {
		writeError(w, http.StatusBadRequest, "Debrief requires both a planned route and an actual recorded track, or at least two planned routes to compare.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debriefs)
}

type plannedRouteMatch struct {
	plannedTrackID   *string
	plannedTrackName string
	plannedDist      float64
	plannedDuration  string
	plannedGeoJSON   model.RawJSON
	voyageDayID      *int64
	startDayID       *int64
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
	re := regexp.MustCompile(`(?i)\b(?:Leg|Day)\s*#?\s*(\d+)\b`)
	matches := re.FindStringSubmatch(name)
	if len(matches) > 1 {
		num, err := strconv.Atoi(matches[1])
		if err == nil {
			return num
		}
	}
	return 0
}

func findClosestDayStart(lat, lon float64, days []model.VoyageDay, maxDistNM float64) int {
	bestIdx := -1
	minDist := maxDistNM
	for i, d := range days {
		dist := gpx.CalculateHaversineNM(lat, lon, d.StartLatitude, d.StartLongitude)
		if dist < minDist {
			minDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

// formatHMS renders a duration given in minutes as "8h 8m 8s".
func formatHMS(totalMins float64) string {
	secs := int(math.Round(totalMins * 60))
	if secs < 0 {
		secs = 0
	}
	return fmt.Sprintf("%dh %dm %ds", secs/3600, (secs%3600)/60, secs%60)
}

var durDaysRegex = regexp.MustCompile(`(?i)^(\d+)\s*days?\s+`)

func normalizeDuration(s string) string {
	s = strings.TrimSpace(s)
	days := 0.0
	if m := durDaysRegex.FindStringSubmatch(s); len(m) > 1 {
		days, _ = strconv.ParseFloat(m[1], 64)
		s = s[len(m[0]):]
	}
	mins, ok := ParseDurationMinutes(s)
	if !ok {
		return s
	}
	return formatHMS(days*24*60 + mins)
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

const (
	legEndpointMatchNM          = 15.0
	sameDayDepartureBonusNM     = 5.0
	singleDayFallbackNM         = 10.0
	circuitLoopRadiusNM         = 2.0
	plannedRouteEndpointMatchNM = 8.0
	defaultPlanningSpeedKts     = 5.5
	maxDBDistanceColumnNM       = 999999.99
	maxDBSpeedColumnKts         = 999.99
)

func formatDayLegTitle(d model.VoyageDay) string {
	if d.StartLocationName != "" && d.EndLocationName != "" && d.StartLocationName != d.EndLocationName {
		return fmt.Sprintf("%s to %s", d.StartLocationName, d.EndLocationName)
	}
	if d.EndLocationName != "" {
		return d.EndLocationName
	}
	return d.StartLocationName
}

// ResolveDebriefDay resolves the VoyageDayID/StartDayID and DayTitle for a track debrief.
func ResolveDebriefDay(d *model.TrackDebrief, t *model.VoyageTrack, days []model.VoyageDay) {
	if len(days) == 0 {
		if d.StartDayID == nil && d.VoyageDayID != nil {
			d.StartDayID = d.VoyageDayID
		} else if d.StartDayID == nil && t != nil && t.VoyageDayID != nil {
			d.StartDayID = t.VoyageDayID
			d.VoyageDayID = t.VoyageDayID
		}
		return
	}
	trackName := d.TrackName
	if trackName == "" && t != nil {
		trackName = t.Name
	}
	plannedName := d.PlannedTrackName
	foundDay := false

	// 1. Check explicit leg number
	legNum := extractLegNumber(trackName)
	if legNum == 0 && plannedName != "" {
		legNum = extractLegNumber(plannedName)
	}
	if legNum >= 1 && legNum <= len(days) {
		d.StartDayID = &days[legNum-1].ID
		d.VoyageDayID = &days[legNum-1].ID
		foundDay = true
	}

	// 2. Check track endpoints against each day's Start -> End coordinates
	if !foundDay && t != nil {
		if startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(t); ok {
			bestLeg := -1
			bestScore := 1e9
			for i := 0; i < len(days); i++ {
				dStart := gpx.CalculateHaversineNM(startLat, startLng, days[i].StartLatitude, days[i].StartLongitude)
				dEnd := gpx.CalculateHaversineNM(endLat, endLng, days[i].EndLatitude, days[i].EndLongitude)
				if dStart <= legEndpointMatchNM && dEnd <= legEndpointMatchNM {
					score := dStart + dEnd
					if t.StartTime != nil && !days[i].TargetDate.IsZero() {
						if days[i].TargetDate.Format("2006-01-02") == t.StartTime.Format("2006-01-02") {
							score -= sameDayDepartureBonusNM
						}
					}
					if score < bestScore {
						bestScore = score
						bestLeg = i
					}
				}
			}
			if bestLeg >= 0 {
				d.StartDayID = &days[bestLeg].ID
				d.VoyageDayID = &days[bestLeg].ID
				foundDay = true
			}
		}
	}

	// 3. Check if track name mentions starting location name
	if !foundDay {
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
		for i := 0; i < len(days); i++ {
			loc := days[i].StartLocationName
			if loc != "" {
				if strings.Contains(depPart, loc) || (depPart == nameForLocationMatch && strings.Contains(nameForLocationMatch, loc)) {
					d.StartDayID = &days[i].ID
					d.VoyageDayID = &days[i].ID
					foundDay = true
					break
				}
			}
		}
	}

	// 4. Fallback to closest day start by coordinates
	if !foundDay && t != nil {
		if startLat, startLng, _, _, ok := extractTrackEndpoints(t); ok {
			dIdx := findClosestDayStart(startLat, startLng, days, singleDayFallbackNM)
			if dIdx >= 0 {
				d.StartDayID = &days[dIdx].ID
				d.VoyageDayID = &days[dIdx].ID
				foundDay = true
			}
		}
	}

	// 5. Check existing StartDayID or VoyageDayID reference
	if !foundDay {
		refID := d.StartDayID
		if refID == nil {
			refID = d.VoyageDayID
		}
		if refID == nil && t != nil {
			refID = t.VoyageDayID
		}
		if refID != nil {
			for i, dy := range days {
				if dy.ID == *refID {
					d.StartDayID = &days[i].ID
					d.VoyageDayID = &days[i].ID
					foundDay = true
					break
				}
			}
		}
	}

	if !foundDay && d.StartDayID == nil {
		d.StartDayID = &days[0].ID
		d.VoyageDayID = &days[0].ID
	}

	if d.StartDayID != nil {
		for _, dy := range days {
			if dy.ID == *d.StartDayID {
				d.DayTitle = formatDayLegTitle(dy)
				break
			}
		}
	}
	if d.DayTitle == "" && len(days) > 0 {
		d.DayTitle = formatDayLegTitle(days[0])
	}
}

func pairActualWithPlanned(actual *model.VoyageTrack, allTracks []model.VoyageTrack, days []model.VoyageDay) plannedRouteMatch {
	var plannedTracks []*model.VoyageTrack
	for i := range allTracks {
		if allTracks[i].Kind == string(model.TrackKindPlanned) {
			plannedTracks = append(plannedTracks, &allTracks[i])
		}
	}

	var matched *model.VoyageTrack

	// Strategy A: Match by VoyageDayID
	if actual.VoyageDayID != nil {
		for _, p := range plannedTracks {
			if p.VoyageDayID != nil && *p.VoyageDayID == *actual.VoyageDayID {
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

	if matched == nil {
		return plannedRouteMatch{}
	}

	tempDebrief := model.TrackDebrief{
		TrackName:        actual.Name,
		PlannedTrackName: matched.Name,
		VoyageDayID:      actual.VoyageDayID,
	}
	ResolveDebriefDay(&tempDebrief, actual, days)
	startDayID := tempDebrief.StartDayID
	if startDayID == nil && len(days) > 0 {
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
		if actIdx >= 0 && actIdx < len(days) {
			startDayID = &days[actIdx].ID
		}
	}
	if startDayID == nil && len(days) > 0 {
		startDayID = &days[0].ID
	}

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
		plannedGeoJSON:   matched.GeoJSON,
		voyageDayID:      startDayID,
		startDayID:       startDayID,
	}
}

func (h *Handler) generateTrackDebrief(ctx context.Context, track *model.VoyageTrack, allTracks []model.VoyageTrack, days []model.VoyageDay) *model.TrackDebrief {
	match := pairActualWithPlanned(track, allTracks, days)
	if match.plannedTrackID == nil {
		return nil
	}

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
		recDur = normalizeDuration(*track.DurationInterval)
	}
	plannedDuration := normalizeDuration(match.plannedDuration)
	if plannedDuration == "" {
		if plannedDist > 0 {
			plannedDuration = formatHMS(plannedDist / 5.5 * 60)
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

	var dayTitle string
	if match.startDayID != nil && len(days) > 0 {
		for _, dy := range days {
			if dy.ID == *match.startDayID {
				dayTitle = formatDayLegTitle(dy)
				break
			}
		}
	}
	if dayTitle == "" && len(days) > 0 {
		dayTitle = formatDayLegTitle(days[0])
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
		DayTitle:            dayTitle,
		PlannedTrackID:      match.plannedTrackID,
		PlannedTrackName:    match.plannedTrackName,
		VoyageDayID:         match.startDayID,
		StartDayID:          match.startDayID,
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

	var briefing *model.Briefing
	if h.DB != nil && match.startDayID != nil {
		b, err := h.DB.GetBriefing(ctx, *match.startDayID)
		if err == nil {
			briefing = b
		}
	}
	weatherContext, parsedWeather := formatDebriefWeatherAndTides(briefing)

	telemetry := gpx.AnalyzeGPXTelemetry(match.plannedGeoJSON, track.GeoJSON, track.RawGPX, avgSpd)

	prompt := fmt.Sprintf(
		"Perform a post-voyage tactical pilot debrief comparing the ACTUAL recorded passage against the PLANNED route:\n"+
			"- Actual Track: '%s'\n"+
			"- Paired Planned Route: '%s'\n"+
			"- Recorded Distance: %.2f NM vs Planned Distance: %.2f NM (Delta: %+.2f NM, %+.1f%% variance)\n"+
			"%s\n"+
			"- Vessel Speed: Average SOG %.1f kts, Maximum SOG %.1f kts\n"+
			"%s\n"+
			"%s\n"+
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
		track.Name, match.plannedTrackName, recDist, plannedDist, distDelta, pctOver, durComparison, avgSpd, maxSpd, weatherContext, telemetry.FormattedSummary,
	)

	if track.RawGPX != nil && len(*track.RawGPX) > 0 && len(*track.RawGPX) < 100000 {
		prompt += fmt.Sprintf("\n\nGPX Track Data (Pre-filtered and smoothed for GPS glitches upon import):\n```xml\n%s\n```\n", *track.RawGPX)
	}

	if h.Agent != nil {
		agentSessionID := fmt.Sprintf("debrief_%s_%d", track.ID, time.Now().UnixNano())
		resp, err := h.Agent.RunSync(ctx, "debrief", "system", agentSessionID, prompt)
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
			debrief.TackingEfficiency = "Tacking overhead and course corrections added extra distance over the planned route."
		} else {
			debrief.TackingEfficiency = "Direct course steered with minimal tacking overhead."
		}
	}

	if debrief.WeatherImpact == "" {
		debrief.WeatherImpact = buildFallbackWeatherImpact(parsedWeather)
	}

	if len(debrief.Observations) == 0 {
		debrief.Observations = []string{
			"Review upwind VMG angles to optimize tacking overhead on similar legs.",
			"Verify current and tidal stream timing against planned departure windows.",
		}
	}

	return &debrief
}

func formatDebriefWeatherAndTides(briefing *model.Briefing) (string, *model.WeatherSummary) {
	if briefing == nil {
		return "", nil
	}
	var sb strings.Builder
	var parsedWS *model.WeatherSummary
	rawWeather := strings.TrimSpace(string(briefing.WeatherSummary))
	if rawWeather != "" && rawWeather != "null" && rawWeather != "{}" {
		var ws model.WeatherSummary
		if err := json.Unmarshal(briefing.WeatherSummary, &ws); err == nil {
			if ws.WindSpeedKt > 0 || ws.WaveHeightFt > 0 || ws.Condition != "" || ws.Summary != "" {
				parsedWS = &ws
			}
		}
		sb.WriteString("\nWeather Conditions for Leg:\n")
		if parsedWS != nil {
			var parts []string
			if parsedWS.Summary != "" {
				parts = append(parts, fmt.Sprintf("Condition: %s", parsedWS.Summary))
			} else if parsedWS.Condition != "" {
				parts = append(parts, fmt.Sprintf("Condition: %s", parsedWS.Condition))
			}
			if parsedWS.WindSpeedKt > 0 {
				if parsedWS.WindDirection != "" {
					parts = append(parts, fmt.Sprintf("Wind: %.1f kts %s", parsedWS.WindSpeedKt, parsedWS.WindDirection))
				} else {
					parts = append(parts, fmt.Sprintf("Wind: %.1f kts", parsedWS.WindSpeedKt))
				}
			}
			if parsedWS.WaveHeightFt > 0 {
				if parsedWS.WavePeriod > 0 {
					parts = append(parts, fmt.Sprintf("Waves: %.1f ft @ %.0fs", parsedWS.WaveHeightFt, parsedWS.WavePeriod))
				} else {
					parts = append(parts, fmt.Sprintf("Waves: %.1f ft", parsedWS.WaveHeightFt))
				}
			}
			if len(parts) > 0 {
				sb.WriteString("- " + strings.Join(parts, " | ") + "\n")
			}
		}
		sb.WriteString(rawWeather + "\n")
	}
	rawTides := strings.TrimSpace(string(briefing.Tides))
	if rawTides != "" && rawTides != "null" && rawTides != "{}" && rawTides != "[]" {
		sb.WriteString("\nTides (48h forecast):\n")
		sb.WriteString(rawTides + "\n")
	}
	return sb.String(), parsedWS
}

func buildFallbackWeatherImpact(ws *model.WeatherSummary) string {
	if ws != nil && (ws.WindSpeedKt > 0 || ws.WaveHeightFt > 0 || ws.Condition != "") {
		var details []string
		if ws.WindSpeedKt > 0 {
			if ws.WindDirection != "" {
				details = append(details, fmt.Sprintf("%.0f kt %s winds", ws.WindSpeedKt, ws.WindDirection))
			} else {
				details = append(details, fmt.Sprintf("%.0f kt winds", ws.WindSpeedKt))
			}
		}
		if ws.WaveHeightFt > 0 {
			details = append(details, fmt.Sprintf("%.1f ft seas", ws.WaveHeightFt))
		}
		if len(details) > 0 {
			return fmt.Sprintf("Passage sailed in %s influencing vessel speed and leeway along the leg.", strings.Join(details, " and "))
		}
	}
	return "Conditions allowed steady progress along the leg."
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

func buildModelTrack(voyageID int64, dayID *int64, fileName, rawGPX string, pt gpx.ParsedTrack) model.VoyageTrack {
	pt.Name = resolveGPXTrackName(pt.Name, fileName, rawGPX)
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

	distNM := pt.DistanceNM
	if distNM < 0 || math.IsNaN(distNM) || math.IsInf(distNM, 0) {
		distNM = 0
	} else if distNM > maxDBDistanceColumnNM {
		distNM = maxDBDistanceColumnNM
	}

	maxSpd := pt.MaxSpeedKts
	if maxSpd < 0 || math.IsNaN(maxSpd) || math.IsInf(maxSpd, 0) {
		maxSpd = 0
	} else if maxSpd > maxDBSpeedColumnKts {
		maxSpd = maxDBSpeedColumnKts
	}

	avgSpd := pt.AvgSpeedKts
	if avgSpd < 0 || math.IsNaN(avgSpd) || math.IsInf(avgSpd, 0) {
		avgSpd = 0
	} else if avgSpd > maxDBSpeedColumnKts {
		avgSpd = maxDBSpeedColumnKts
	}

	return model.VoyageTrack{
		VoyageID:          voyageID,
		VoyageDayID:       dayID,
		Kind:              pt.Kind,
		Name:              pt.Name,
		FileName:          fn,
		StartTime:         pt.StartTime,
		EndTime:           pt.EndTime,
		DistanceNM:        &distNM,
		DurationInterval:  dur,
		MaxSpeedKts:       &maxSpd,
		AvgSpeedKts:       &avgSpd,
		GeoJSON:           model.RawJSON(pt.GeoJSON),
		SimplifiedGeoJSON: model.RawJSON(pt.SimplifiedGeoJSON),
		RawGPX:            raw,
	}
}

var genericGPXTrackNameRegex = regexp.MustCompile(`(?i)^(track|route|waypoints\s+route|untitled)(\s*#?\s*\d+)?$`)

func isGenericGPXTrackName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	return genericGPXTrackNameRegex.MatchString(trimmed)
}

func resolveGPXTrackName(xmlName, fileName, rawGPX string) string {
	name := strings.TrimSpace(xmlName)
	if !isGenericGPXTrackName(name) {
		return name
	}

	if rawGPX != "" {
		var metaWrapper struct {
			Metadata *struct {
				Name string `xml:"name"`
			} `xml:"metadata"`
			Name string `xml:"name"`
		}
		if err := xml.Unmarshal([]byte(rawGPX), &metaWrapper); err == nil {
			if metaWrapper.Metadata != nil {
				metaName := strings.TrimSpace(metaWrapper.Metadata.Name)
				if !isGenericGPXTrackName(metaName) {
					return metaName
				}
			}
			topName := strings.TrimSpace(metaWrapper.Name)
			if !isGenericGPXTrackName(topName) {
				return topName
			}
		}
	}

	if fileName != "" {
		base := filepath.Base(fileName)
		stem := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
		if stem != "" && (!strings.EqualFold(stem, "track") || name == "") {
			return stem
		}
	}

	if name != "" {
		return name
	}
	return "Track"
}

type plannedTrackGroup struct {
	day    *model.VoyageDay
	tracks []*model.VoyageTrack
}

// plannedTrackGeometrySignature returns a canonical geometry signature for a track
// based on rounded distance and start/mid/end coordinates so duplicate uploads of the
// exact same route are never compared against themselves.
func plannedTrackGeometrySignature(t *model.VoyageTrack) string {
	if t == nil {
		return ""
	}
	dist := 0.0
	if t.DistanceNM != nil {
		dist = *t.DistanceNM
	}
	raw := t.SimplifiedGeoJSON
	if len(raw) == 0 {
		raw = t.GeoJSON
	}
	coords := extractCoordinates(raw)
	if len(coords) >= 2 {
		start := coords[0]
		mid := coords[len(coords)/2]
		end := coords[len(coords)-1]
		if len(start) >= 2 && len(mid) >= 2 && len(end) >= 2 {
			if dist <= 0 {
				for i := 0; i < len(coords)-1; i++ {
					dist += gpx.CalculateHaversineNM(coords[i][1], coords[i][0], coords[i+1][1], coords[i+1][0])
				}
			}
			return fmt.Sprintf("geo:%.1f:%.3f,%.3f:%.3f,%.3f:%.3f,%.3f",
				dist, start[1], start[0], mid[1], mid[0], end[1], end[0])
		}
	}
	return fmt.Sprintf("name:%s:%.1f", strings.ToLower(strings.TrimSpace(t.Name)), dist)
}

// filterCandidatePlannedTracks returns planned tracks excluding redundant unsplit master tracks
// when split leg tracks exist for the same upload/voyage, and deduplicates identical planned routes.
func filterCandidatePlannedTracks(allTracks []model.VoyageTrack) []*model.VoyageTrack {
	var planned []*model.VoyageTrack
	for i := range allTracks {
		if allTracks[i].Kind == string(model.TrackKindPlanned) {
			planned = append(planned, &allTracks[i])
		}
	}
	if len(planned) == 0 {
		return nil
	}

	var nonMaster []*model.VoyageTrack
	for _, t := range planned {
		isMaster := false
		if t.VoyageDayID == nil && extractLegNumber(t.Name) == 0 {
			for _, other := range planned {
				if other.ID == t.ID {
					continue
				}
				otherIsLeg := other.VoyageDayID != nil || extractLegNumber(other.Name) > 0
				if !otherIsLeg {
					continue
				}
				sameFile := t.FileName != nil && other.FileName != nil && *t.FileName != "" && *t.FileName == *other.FileName
				namePrefix := t.Name != "" && (strings.HasPrefix(other.Name, t.Name+" - Leg") || strings.HasPrefix(other.Name, t.Name+" (Leg"))
				if sameFile || namePrefix {
					isMaster = true
					break
				}
			}
		}
		if !isMaster {
			nonMaster = append(nonMaster, t)
		}
	}
	if len(nonMaster) == 0 {
		nonMaster = planned
	}

	seenSig := make(map[string]bool)
	var unique []*model.VoyageTrack
	for _, t := range nonMaster {
		sig := plannedTrackGeometrySignature(t)
		if sig != "" && seenSig[sig] {
			continue
		}
		if sig != "" {
			seenSig[sig] = true
		}
		unique = append(unique, t)
	}
	return unique
}

func trackMatchesDayEndpoints(t *model.VoyageTrack, day *model.VoyageDay) bool {
	if t == nil || day == nil {
		return false
	}
	startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(t)
	if !ok {
		return true
	}
	hasDayCoords := (day.StartLatitude != 0 || day.StartLongitude != 0) && (day.EndLatitude != 0 || day.EndLongitude != 0)
	if !hasDayCoords {
		return true
	}
	dStart := gpx.CalculateHaversineNM(startLat, startLng, day.StartLatitude, day.StartLongitude)
	dEnd := gpx.CalculateHaversineNM(endLat, endLng, day.EndLatitude, day.EndLongitude)
	return dStart <= legEndpointMatchNM && dEnd <= legEndpointMatchNM
}

func tracksEndpointsOverlap(a, b *model.VoyageTrack, maxDistNM float64) bool {
	if a == nil || b == nil {
		return false
	}
	aStartLat, aStartLng, aEndLat, aEndLng, aOk := extractTrackEndpoints(a)
	bStartLat, bStartLng, bEndLat, bEndLng, bOk := extractTrackEndpoints(b)
	if !aOk || !bOk {
		return true
	}
	dStart := gpx.CalculateHaversineNM(aStartLat, aStartLng, bStartLat, bStartLng)
	dEnd := gpx.CalculateHaversineNM(aEndLat, aEndLng, bEndLat, bEndLng)
	return dStart <= maxDistNM && dEnd <= maxDistNM
}

func resolvePlannedTrackDay(t *model.VoyageTrack, days []model.VoyageDay) *model.VoyageDay {
	if t == nil || len(days) == 0 {
		return nil
	}
	if startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(t); ok {
		bestLeg := -1
		bestScore := 1e9
		for i := range days {
			dStart := gpx.CalculateHaversineNM(startLat, startLng, days[i].StartLatitude, days[i].StartLongitude)
			dEnd := gpx.CalculateHaversineNM(endLat, endLng, days[i].EndLatitude, days[i].EndLongitude)
			if dStart <= legEndpointMatchNM && dEnd <= legEndpointMatchNM {
				score := dStart + dEnd
				if score < bestScore {
					bestScore = score
					bestLeg = i
				}
			}
		}
		if bestLeg >= 0 {
			return &days[bestLeg]
		}
		// Track has valid coordinates that do not match both start and end of any voyage day/leg.
		return nil
	}
	if t.VoyageDayID != nil {
		for i := range days {
			if days[i].ID == *t.VoyageDayID && trackMatchesDayEndpoints(t, &days[i]) {
				return &days[i]
			}
		}
	}
	if legNum := extractLegNumber(t.Name); legNum >= 1 && legNum <= len(days) {
		if trackMatchesDayEndpoints(t, &days[legNum-1]) {
			return &days[legNum-1]
		}
	}
	return nil
}

// tracksConflictInTimeOrLeg returns true if two planned tracks belong to different
// days/legs, do not overlap in start/end endpoints, or have non-overlapping calendar dates.
func tracksConflictInTimeOrLeg(a, b *model.VoyageTrack, aDay, bDay *model.VoyageDay) bool {
	if a == nil || b == nil {
		return true
	}
	if !tracksEndpointsOverlap(a, b, legEndpointMatchNM) {
		return true
	}
	aLeg := extractLegNumber(a.Name)
	bLeg := extractLegNumber(b.Name)
	if aLeg > 0 && bLeg > 0 && aLeg != bLeg {
		return true
	}
	if aDay != nil && bDay != nil && aDay.ID != bDay.ID {
		return true
	}
	if (aDay != nil && bDay == nil) || (bDay != nil && aDay == nil) {
		return true
	}
	if a.StartTime != nil && b.StartTime != nil && !a.StartTime.IsZero() && !b.StartTime.IsZero() {
		aEnd := *a.StartTime
		if a.EndTime != nil && a.EndTime.After(aEnd) {
			aEnd = *a.EndTime
		}
		bEnd := *b.StartTime
		if b.EndTime != nil && b.EndTime.After(bEnd) {
			bEnd = *b.EndTime
		}
		// Different calendar days without time-window overlap
		if a.StartTime.UTC().Format("2006-01-02") != b.StartTime.UTC().Format("2006-01-02") {
			if aEnd.Before(*b.StartTime) || bEnd.Before(*a.StartTime) {
				return true
			}
		}
	}
	return false
}

// groupCandidatePlannedTracks groups candidate planned tracks that overlap in both
// time/leg and start/end endpoints (within legEndpointMatchNM).
func groupCandidatePlannedTracks(allTracks []model.VoyageTrack, days []model.VoyageDay) []plannedTrackGroup {
	planned := filterCandidatePlannedTracks(allTracks)
	if len(planned) == 0 {
		return nil
	}

	var groups []plannedTrackGroup
	for _, p := range planned {
		pDay := resolvePlannedTrackDay(p, days)
		pLeg := extractLegNumber(p.Name)

		matchedIdx := -1
		for idx := range groups {
			g := &groups[idx]
			if len(g.tracks) == 0 || tracksConflictInTimeOrLeg(p, g.tracks[0], pDay, g.day) {
				continue
			}
			if pDay != nil && g.day != nil && pDay.ID == g.day.ID {
				matchedIdx = idx
				break
			}
			if pDay == nil && g.day == nil && pLeg > 0 && extractLegNumber(g.tracks[0].Name) == pLeg {
				matchedIdx = idx
				break
			}
			if pDay == nil && g.day == nil && pLeg == 0 && tracksEndpointsOverlap(p, g.tracks[0], 3.0) {
				matchedIdx = idx
				break
			}
		}

		if matchedIdx >= 0 {
			groups[matchedIdx].tracks = append(groups[matchedIdx].tracks, p)
			if groups[matchedIdx].day == nil && pDay != nil {
				groups[matchedIdx].day = pDay
			}
		} else {
			groups = append(groups, plannedTrackGroup{
				day:    pDay,
				tracks: []*model.VoyageTrack{p},
			})
		}
	}

	var multiGroups []plannedTrackGroup
	for _, g := range groups {
		if len(g.tracks) >= 2 {
			multiGroups = append(multiGroups, g)
		}
	}

	return multiGroups
}

func findCandidateGroupForTrack(target *model.VoyageTrack, allTracks []model.VoyageTrack, days []model.VoyageDay) plannedTrackGroup {
	groups := groupCandidatePlannedTracks(allTracks, days)
	for _, g := range groups {
		for _, tr := range g.tracks {
			if tr.ID == target.ID {
				return g
			}
		}
	}
	return plannedTrackGroup{
		day:    resolvePlannedTrackDay(target, days),
		tracks: []*model.VoyageTrack{target},
	}
}

func (h *Handler) persistComparisonDebrief(ctx context.Context, debrief *model.TrackDebrief, groupTracks []*model.VoyageTrack) {
	if h.DB == nil || debrief == nil || len(groupTracks) == 0 {
		return
	}
	primaryID := debrief.TrackID
	if primaryID == "" {
		primaryID = groupTracks[0].ID
		debrief.TrackID = primaryID
	}
	debriefBytes, err := json.Marshal(debrief)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to marshal comparison debrief", "track_id", primaryID, "error", err)
		return
	}
	if err := h.DB.UpdateVoyageTrackDebrief(ctx, primaryID, model.RawJSON(debriefBytes)); err != nil {
		slog.ErrorContext(ctx, "Failed to persist comparison debrief", "track_id", primaryID, "error", err)
	}
	// Clear stale comparison debriefs on non-primary planned tracks in the same group so the UI doesn't render duplicate cards.
	for _, tr := range groupTracks {
		if tr.ID == primaryID || len(tr.Debrief) == 0 {
			continue
		}
		var prev model.TrackDebrief
		if err := json.Unmarshal(tr.Debrief, &prev); err == nil && prev.Mode == "comparison" {
			_ = h.DB.UpdateVoyageTrackDebrief(ctx, tr.ID, nil)
		}
	}
}

type evaluatedCandidateRoute struct {
	track            *model.VoyageTrack
	candidate        model.PlannedTrackCandidate
	upwindNM         float64
	reachNM          float64
	downwindNM       float64
	upwindPct        float64
	avgTWA           float64
	hasWindEval      bool
	tacticalScoreHrs float64
	routeBlock       string
}

func parseWindDirectionDegrees(dir string) (float64, bool) {
	clean := strings.ToUpper(strings.TrimSpace(dir))
	if clean == "" {
		return 0, false
	}
	fields := strings.Fields(clean)
	if len(fields) > 0 {
		clean = strings.TrimSuffix(fields[0], "°")
	}
	cardinalMap := map[string]float64{
		"N": 0, "NORTH": 0,
		"NNE": 22.5,
		"NE":  45, "NORTHEAST": 45,
		"ENE": 67.5,
		"E":   90, "EAST": 90,
		"ESE": 112.5,
		"SE":  135, "SOUTHEAST": 135,
		"SSE": 157.5,
		"S":   180, "SOUTH": 180,
		"SSW": 202.5,
		"SW":  225, "SOUTHWEST": 225,
		"WSW": 247.5,
		"W":   270, "WEST": 270,
		"WNW": 292.5,
		"NW":  315, "NORTHWEST": 315,
		"NNW": 337.5,
	}
	if deg, ok := cardinalMap[clean]; ok {
		return deg, true
	}
	if v, err := strconv.ParseFloat(clean, 64); err == nil && !math.IsNaN(v) {
		return math.Mod(v+360, 360), true
	}
	return 0, false
}

func smallestAngleDiffDeg(a, b float64) float64 {
	diff := math.Mod(math.Abs(a-b), 360.0)
	if diff > 180.0 {
		diff = 360.0 - diff
	}
	return diff
}

type parsedTideEvent struct {
	Time     time.Time
	TimeStr  string
	Type     string
	HeightFt float64
}

func parseBriefingTideEvents(rawTides []byte) (string, []parsedTideEvent) {
	if len(rawTides) == 0 {
		return "", nil
	}
	var td struct {
		StationName string `json:"station_name"`
		Events      []struct {
			Time     string  `json:"time"`
			Type     string  `json:"type"`
			HeightFt float64 `json:"height_ft"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rawTides, &td); err != nil {
		return "", nil
	}
	var out []parsedTideEvent
	layouts := []string{"2006-01-02 15:04", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "15:04"}
	for _, ev := range td.Events {
		var parsedTime time.Time
		for _, l := range layouts {
			if t, err := time.Parse(l, strings.TrimSpace(ev.Time)); err == nil {
				parsedTime = t
				break
			}
		}
		out = append(out, parsedTideEvent{
			Time:     parsedTime,
			TimeStr:  strings.TrimSpace(ev.Time),
			Type:     strings.TrimSpace(ev.Type),
			HeightFt: ev.HeightFt,
		})
	}
	return strings.TrimSpace(td.StationName), out
}

func describeTideStageAtHour(hour float64, events []parsedTideEvent) (stageLabel string, isActiveStream bool, rangeFt float64) {
	if len(events) == 0 {
		return "", false, 0
	}
	minH, maxH := events[0].HeightFt, events[0].HeightFt
	for _, ev := range events {
		if ev.HeightFt < minH {
			minH = ev.HeightFt
		}
		if ev.HeightFt > maxH {
			maxH = ev.HeightFt
		}
	}
	rangeFt = maxH - minH

	type hourEv struct {
		ev  parsedTideEvent
		hrs float64
	}
	var dayEvs []hourEv
	for _, ev := range events {
		if !ev.Time.IsZero() {
			h := float64(ev.Time.Hour()) + float64(ev.Time.Minute())/60.0
			dayEvs = append(dayEvs, hourEv{ev: ev, hrs: h})
		}
	}
	if len(dayEvs) == 0 {
		first := events[0]
		return fmt.Sprintf("Next %s %.1f ft @ %s (range %.1f ft)", first.Type, first.HeightFt, first.TimeStr, rangeFt), rangeFt >= 4.0, rangeFt
	}

	var prev, next *hourEv
	for i := range dayEvs {
		if dayEvs[i].hrs <= hour {
			prev = &dayEvs[i]
		} else if next == nil && dayEvs[i].hrs > hour {
			next = &dayEvs[i]
			break
		}
	}
	if next == nil {
		next = &dayEvs[0]
	}

	target := next.ev
	dt := math.Abs(next.hrs - hour)
	isHigh := strings.HasPrefix(strings.ToLower(target.Type), "h")
	timeShort := target.TimeStr
	if !target.Time.IsZero() {
		timeShort = target.Time.Format("15:04")
	}

	if dt <= 0.6 {
		if isHigh {
			return fmt.Sprintf("Slack near High Water (%.1f ft @ %s)", target.HeightFt, timeShort), false, rangeFt
		}
		return fmt.Sprintf("Slack near Low Water (%.1f ft @ %s)", target.HeightFt, timeShort), false, rangeFt
	}

	streamStrength := "moderate"
	if dt >= 1.5 && dt <= 4.0 {
		streamStrength = "peak mid-tide"
		isActiveStream = true
	} else {
		isActiveStream = rangeFt >= 3.0
	}
	if prev != nil {
		legRange := math.Abs(target.HeightFt - prev.ev.HeightFt)
		if legRange > 0 {
			rangeFt = legRange
		}
	}
	if isHigh {
		return fmt.Sprintf("Flooding (%s flood stream, rising %.1f ft toward High %.1f ft @ %s)", streamStrength, rangeFt, target.HeightFt, timeShort), isActiveStream, rangeFt
	}
	return fmt.Sprintf("Ebbing (%s ebb stream, falling %.1f ft toward Low %.1f ft @ %s)", streamStrength, rangeFt, target.HeightFt, timeShort), isActiveStream, rangeFt
}

func pointOfSailLabel(twa float64) string {
	switch {
	case twa <= 55.0:
		return "Close-hauled / Beat"
	case twa <= 80.0:
		return "Close Reach"
	case twa <= 110.0:
		return "Beam Reach"
	case twa <= 145.0:
		return "Broad Reach"
	default:
		return "Run / Downwind"
	}
}

func formatAlongRouteEnvForLookout(tracks []model.VoyageTrack, briefing *model.Briefing) string {
	if len(tracks) == 0 || briefing == nil {
		return ""
	}
	_, parsedWS := formatDebriefWeatherAndTides(briefing)
	var rawTides []byte
	if len(briefing.Tides) > 0 {
		rawTides = briefing.Tides
	}
	if parsedWS == nil && len(rawTides) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Along-Route Time-Stepped Wind, Wave, Tide & Current Profile (assuming 08:00 departure @ 6.0 kts base speed):\n")
	for i := range tracks {
		ev := evaluateCandidateRoute(&tracks[i], parsedWS, rawTides)
		sb.WriteString(fmt.Sprintf("  Route %d (%q — %.1f NM, est. %s):\n", i+1, ev.candidate.Name, ev.candidate.DistanceNM, ev.candidate.EstimatedDuration))
		if ev.candidate.WindAngleSummary != "" {
			sb.WriteString(fmt.Sprintf("    - Wind Profile: %s\n", ev.candidate.WindAngleSummary))
		}
		if ev.candidate.TidalSummary != "" {
			sb.WriteString(fmt.Sprintf("    - Tide & Current Profile: %s\n", ev.candidate.TidalSummary))
		}
		for _, segLine := range ev.candidate.AlongRouteProfile {
			sb.WriteString(fmt.Sprintf("    * %s\n", segLine))
		}
	}
	return strings.TrimSpace(sb.String())
}

func evaluateCandidateRoute(tr *model.VoyageTrack, ws *model.WeatherSummary, rawTidesOpt ...[]byte) evaluatedCandidateRoute {
	const cruisingSpeedKts = 6.0

	var rawTides []byte
	if len(rawTidesOpt) > 0 {
		rawTides = rawTidesOpt[0]
	}
	stationName, tideEvents := parseBriefingTideEvents(rawTides)

	distNM := 0.0
	if tr.DistanceNM != nil && *tr.DistanceNM > 0 {
		distNM = *tr.DistanceNM
	}

	raw := tr.GeoJSON
	if len(raw) == 0 {
		raw = tr.SimplifiedGeoJSON
	}
	coords := extractCoordinates(raw)
	if len(coords) < 2 && len(tr.SimplifiedGeoJSON) > 0 {
		coords = extractCoordinates(tr.SimplifiedGeoJSON)
	}

	var sinSum, cosSum, totalSegDist float64
	var upwindNM, reachNM, downwindNM, weightedTWA float64
	baseWindDeg, hasBaseWindDir := 0.0, false
	baseWindSpd := 0.0
	baseWindDirLabel := ""
	if ws != nil {
		baseWindSpd = ws.WindSpeedKt
		baseWindDirLabel = strings.TrimSpace(ws.WindDirection)
		baseWindDeg, hasBaseWindDir = parseWindDirectionDegrees(baseWindDirLabel)
	}

	numPts := len(coords)
	step := 1
	if numPts > 8 {
		step = numPts / 6
		if step < 1 {
			step = 1
		}
	}
	var sampleIdxs []int
	for i := 0; i < numPts; i += step {
		sampleIdxs = append(sampleIdxs, i)
	}
	if len(sampleIdxs) > 0 && sampleIdxs[len(sampleIdxs)-1] != numPts-1 {
		sampleIdxs = append(sampleIdxs, numPts-1)
	}

	depHour := 8.0
	if tr.StartTime != nil && !tr.StartTime.IsZero() {
		depHour = float64(tr.StartTime.Hour()) + float64(tr.StartTime.Minute())/60.0
	}
	elapsedHrs := 0.0
	hasAnyWindEval := false
	var alongRouteProfile []string
	var chopSegments int
	var firstTideStage, lastTideStage string
	var maxRangeFt float64

	for s := 0; s < len(sampleIdxs)-1; s++ {
		idx1, idx2 := sampleIdxs[s], sampleIdxs[s+1]
		if len(coords[idx1]) < 2 || len(coords[idx2]) < 2 {
			continue
		}
		segDist := 0.0
		for k := idx1; k < idx2; k++ {
			if len(coords[k]) >= 2 && len(coords[k+1]) >= 2 {
				segDist += gpx.CalculateHaversineNM(coords[k][1], coords[k][0], coords[k+1][1], coords[k+1][0])
			}
		}
		lat1, lon1 := coords[idx1][1], coords[idx1][0]
		lat2, lon2 := coords[idx2][1], coords[idx2][0]
		if segDist <= 0 {
			segDist = gpx.CalculateHaversineNM(lat1, lon1, lat2, lon2)
		}
		if segDist <= 0 {
			continue
		}

		brg := calculateBearing(lat1, lon1, lat2, lon2)
		cardinal := bearingToCardinal(brg)
		rad := brg * math.Pi / 180.0
		sinSum += math.Sin(rad) * segDist
		cosSum += math.Cos(rad) * segDist
		totalSegDist += segDist

		midHour := depHour + elapsedHrs + (segDist/cruisingSpeedKts)/2.0
		hourIdx := int(math.Floor(midHour)) % 24
		if hourIdx < 0 {
			hourIdx = 0
		}

		segWindSpd := baseWindSpd
		segWindDir := baseWindDirLabel
		segWindDeg := baseWindDeg
		segHasWind := hasBaseWindDir
		segWaveFt := 0.0
		if ws != nil {
			segWaveFt = ws.WaveHeightFt
			if hourIdx < len(ws.HourlyWind) && ws.HourlyWind[hourIdx] > 0 {
				segWindSpd = ws.HourlyWind[hourIdx]
			}
			if hourIdx < len(ws.HourlyWindDir) && strings.TrimSpace(ws.HourlyWindDir[hourIdx]) != "" {
				segWindDir = strings.TrimSpace(ws.HourlyWindDir[hourIdx])
				if d, ok := parseWindDirectionDegrees(segWindDir); ok {
					segWindDeg = d
					segHasWind = true
				}
			}
			if hourIdx < len(ws.HourlyWaveHeight) && ws.HourlyWaveHeight[hourIdx] > 0 {
				segWaveFt = ws.HourlyWaveHeight[hourIdx]
			}
		}

		posLabel := ""
		segSpeedFactor := 1.0
		if segHasWind {
			hasAnyWindEval = true
			twa := smallestAngleDiffDeg(brg, segWindDeg)
			weightedTWA += twa * segDist
			posLabel = pointOfSailLabel(twa)
			if twa <= 55.0 {
				upwindNM += segDist
				segSpeedFactor = 0.75
			} else if twa <= 135.0 {
				reachNM += segDist
			} else {
				downwindNM += segDist
				segSpeedFactor = 0.92
			}
		}

		segDurHrs := segDist / (cruisingSpeedKts * segSpeedFactor)
		startClock := int(math.Floor(depHour+elapsedHrs)) % 24
		startMin := int(math.Round(((depHour+elapsedHrs)-math.Floor(depHour+elapsedHrs))*60.0)) % 60
		elapsedHrs += segDurHrs

		tideStage, isActiveStream, rng := describeTideStageAtHour(midHour, tideEvents)
		if rng > maxRangeFt {
			maxRangeFt = rng
		}
		if tideStage != "" {
			if firstTideStage == "" {
				firstTideStage = tideStage
			}
			lastTideStage = tideStage
		}

		chopNote := ""
		if isActiveStream && segWindSpd >= 14.0 && rng >= 3.5 {
			chopSegments++
			chopNote = " [wind-against-tide chop risk]"
		}

		var profileParts []string
		profileParts = append(profileParts, fmt.Sprintf("Seg %d (%02d:%02d, %.1f NM @ %.0f° %s)", s+1, startClock, startMin, segDist, brg, cardinal))
		if segHasWind {
			twa := smallestAngleDiffDeg(brg, segWindDeg)
			windPiece := fmt.Sprintf("%s %.0f kt (TWA %.0f° — %s)", segWindDir, segWindSpd, twa, posLabel)
			if segWaveFt > 0 {
				windPiece += fmt.Sprintf(", seas %.1f ft", segWaveFt)
			}
			profileParts = append(profileParts, windPiece)
		}
		if tideStage != "" {
			profileParts = append(profileParts, tideStage+chopNote)
		}
		alongRouteProfile = append(alongRouteProfile, strings.Join(profileParts, ": "))
	}

	if distNM <= 0 && totalSegDist > 0 {
		distNM = totalSegDist
	}

	estDur := ""
	if tr.DurationInterval != nil && strings.TrimSpace(*tr.DurationInterval) != "" {
		estDur = normalizeDuration(*tr.DurationInterval)
	}
	if estDur == "" && distNM > 0 {
		estDur = formatHMS((distNM / cruisingSpeedKts) * 60.0)
	}

	var domHeading *float64
	var domCardinal string
	if totalSegDist > 0 {
		meanDeg := math.Mod(math.Atan2(sinSum, cosSum)*180.0/math.Pi+360.0, 360.0)
		rounded := math.Round(meanDeg*10) / 10
		domHeading = &rounded
		domCardinal = bearingToCardinal(rounded)
	}

	windSummary := ""
	avgTWA := 0.0
	upwindPct := 0.0
	if hasAnyWindEval && totalSegDist > 0 {
		avgTWA = weightedTWA / totalSegDist
		upwindPct = (upwindNM / totalSegDist) * 100.0
		reachPct := (reachNM / totalSegDist) * 100.0
		windLabel := baseWindDirLabel
		if baseWindSpd > 0 {
			windLabel = fmt.Sprintf("%s %.0f kt", baseWindDirLabel, baseWindSpd)
		}
		if upwindPct >= 40.0 {
			windSummary = fmt.Sprintf("%.0f%% upwind/close-hauled (avg TWA %.0f° vs %s wind)", upwindPct, avgTWA, windLabel)
		} else if reachPct >= 45.0 {
			windSummary = fmt.Sprintf("Primarily reaching (%.0f%% reach, avg TWA %.0f° vs %s wind)", reachPct, avgTWA, windLabel)
		} else {
			windSummary = fmt.Sprintf("Broad reach / downwind (avg TWA %.0f° vs %s wind)", avgTWA, windLabel)
		}
	} else if domHeading != nil {
		windSummary = fmt.Sprintf("Dominant course %.0f° (%s)", *domHeading, domCardinal)
	}

	tidalSummary := ""
	if firstTideStage != "" {
		if firstTideStage == lastTideStage || lastTideStage == "" {
			tidalSummary = firstTideStage
		} else {
			tidalSummary = fmt.Sprintf("Departs on %s; arrives on %s", firstTideStage, lastTideStage)
		}
		if stationName != "" {
			tidalSummary += fmt.Sprintf(" (%s)", stationName)
		}
		if chopSegments > 0 {
			tidalSummary += fmt.Sprintf(" · %d segment(s) with wind-against-tide chop risk", chopSegments)
		}
	}

	baseHrs := distNM / cruisingSpeedKts
	tacticalPenaltyHrs := 0.0
	if hasAnyWindEval && totalSegDist > 0 {
		upwindPenaltyFactor := 0.35
		if baseWindSpd >= 18.0 || (ws != nil && ws.WaveHeightFt >= 4.0) {
			upwindPenaltyFactor = 0.50
		}
		tacticalPenaltyHrs += (upwindNM * upwindPenaltyFactor) / cruisingSpeedKts
		tacticalPenaltyHrs += (downwindNM * 0.08) / cruisingSpeedKts
	}
	if chopSegments > 0 {
		tacticalPenaltyHrs += float64(chopSegments) * 0.12
	}

	routeBlock, _ := formatPlannedRouteForLookout(tr, distNM)
	if len(alongRouteProfile) > 0 {
		routeBlock = strings.TrimSpace(routeBlock) + "\nAlong-Route Time-Stepped Wind, Wave & Tide Profile:\n  * " + strings.Join(alongRouteProfile, "\n  * ") + "\n"
	}

	name := strings.TrimSpace(tr.Name)
	if name == "" {
		name = "Planned Route"
	}

	return evaluatedCandidateRoute{
		track: tr,
		candidate: model.PlannedTrackCandidate{
			TrackID:           tr.ID,
			Name:              name,
			DistanceNM:        math.Round(distNM*100) / 100,
			EstimatedDuration: estDur,
			DominantHeading:   domHeading,
			DominantCardinal:  domCardinal,
			WindAngleSummary:  windSummary,
			TidalSummary:      tidalSummary,
			AlongRouteProfile: alongRouteProfile,
		},
		upwindNM:         upwindNM,
		reachNM:          reachNM,
		downwindNM:       downwindNM,
		upwindPct:        upwindPct,
		avgTWA:           avgTWA,
		hasWindEval:      hasAnyWindEval && totalSegDist > 0,
		tacticalScoreHrs: baseHrs + tacticalPenaltyHrs,
		routeBlock:       routeBlock,
	}
}

// generateMultiPlanComparison compares 2+ candidate planned routes for a leg/voyage against
// the day's forecast wind, waves, tides, and sun phase.
func (h *Handler) generateMultiPlanComparison(ctx context.Context, candidates []*model.VoyageTrack, day *model.VoyageDay, days []model.VoyageDay) *model.TrackDebrief {
	if len(candidates) < 2 {
		return nil
	}

	targetDay := day
	if targetDay == nil {
		for _, c := range candidates {
			if d := resolvePlannedTrackDay(c, days); d != nil {
				targetDay = d
				break
			}
		}
	}
	if targetDay == nil && len(days) > 0 {
		targetDay = &days[0]
	}

	var startDayID *int64
	var dayTitle string
	if targetDay != nil {
		startDayID = &targetDay.ID
		dayTitle = formatDayLegTitle(*targetDay)
	}

	var briefing *model.Briefing
	if h.DB != nil && startDayID != nil {
		if b, err := h.DB.GetBriefing(ctx, *startDayID); err == nil {
			briefing = b
		}
	}
	weatherContext, parsedWeather := formatDebriefWeatherAndTides(briefing)

	sunContext := ""
	if briefing != nil && len(briefing.SunPhase) > 0 {
		rawSun := strings.TrimSpace(string(briefing.SunPhase))
		if rawSun != "" && rawSun != "null" && rawSun != "{}" {
			sunContext = fmt.Sprintf("\nSun Phase:\n%s\n", rawSun)
		}
	}

	var rawTides []byte
	if briefing != nil && len(briefing.Tides) > 0 {
		rawTides = briefing.Tides
	}

	evals := make([]evaluatedCandidateRoute, 0, len(candidates))
	candidateModels := make([]model.PlannedTrackCandidate, 0, len(candidates))
	bestIdx := 0
	for i, c := range candidates {
		ev := evaluateCandidateRoute(c, parsedWeather, rawTides)
		evals = append(evals, ev)
		candidateModels = append(candidateModels, ev.candidate)
		if ev.tacticalScoreHrs < evals[bestIdx].tacticalScoreHrs {
			bestIdx = i
		}
	}

	var candBuilder strings.Builder
	for i, ev := range evals {
		cand := ev.candidate
		hdgInfo := "N/A"
		if cand.DominantHeading != nil {
			hdgInfo = fmt.Sprintf("%.0f° (%s)", *cand.DominantHeading, cand.DominantCardinal)
		}
		tideLine := ""
		if cand.TidalSummary != "" {
			tideLine = fmt.Sprintf("- Along-Route Tide & Current Profile: %s\n", cand.TidalSummary)
		}
		candBuilder.WriteString(fmt.Sprintf(
			"Candidate %d:\n- Track ID: %s\n- Route Name: %q\n- Distance: %.2f NM\n- Estimated Duration (@ 6.0 kts): %s\n- Dominant Heading: %s\n- Wind Angle Exposure: %s\n%s%s\n",
			i+1, cand.TrackID, cand.Name, cand.DistanceNM, cand.EstimatedDuration, hdgInfo, cand.WindAngleSummary, tideLine, ev.routeBlock,
		))
	}

	legLabel := dayTitle
	if legLabel == "" {
		legLabel = "Voyage Passage"
	}

	prompt := fmt.Sprintf(
		"Perform a pre-departure tactical route comparison across %d candidate planned routes for %s:\n\n"+
			"%s"+
			"%s"+
			"%s\n"+
			"Guidelines:\n"+
			"- Compare the candidate routes on distance, estimated duration, segment headings vs. forecast wind direction/speed (upwind tacking overhead vs reaching/downwind), wave exposure, tidal current timing, and daylight window.\n"+
			"- Select the single best candidate route and explain clearly why it is preferred.\n"+
			"- Keep each field concise, direct, and actionable (1-2 sentences per field).\n\n"+
			"Output JSON matching:\n"+
			"1. summary: Brief 1-2 sentence executive comparison naming the candidate routes and recommended choice.\n"+
			"2. recommended_track_id: Exact Track ID of the recommended candidate route.\n"+
			"3. recommended_track_name: Exact Route Name of the recommended candidate route.\n"+
			"4. conclusions: 1-2 sentences explaining the tactical trade-offs between the routes.\n"+
			"5. tacking_efficiency: 1 sentence comparing point of sail, upwind tacking overhead, and VMG across the candidates.\n"+
			"6. weather_impact: 1 sentence on how forecast wind and sea state favor or penalize each route.\n"+
			"7. tidal_analysis: 1 sentence on tidal currents, slack water windows, or coastal stream effects across the routes.\n"+
			"8. observations: 2-3 short tactical takeaways for the skipper.",
		len(evals), legLabel, candBuilder.String(), weatherContext, sunContext,
	)

	var (
		aiSummary       string
		aiRecID         string
		aiRecName       string
		aiConclusions   string
		aiTacking       string
		aiWeatherImpact string
		aiTidalAnalysis string
		aiObservations  []string
	)

	if h.Agent != nil {
		agentSessionID := fmt.Sprintf("compare_%s_%d", evals[bestIdx].track.ID, time.Now().UnixNano())
		resp, err := h.Agent.RunSync(ctx, "debrief", "system", agentSessionID, prompt)
		if err == nil && resp != "" {
			var parsedResp struct {
				Summary              string   `json:"summary"`
				RecommendedTrackID   string   `json:"recommended_track_id"`
				RecommendedTrackName string   `json:"recommended_track_name"`
				Conclusions          string   `json:"conclusions"`
				TackingEfficiency    string   `json:"tacking_efficiency"`
				WeatherImpact        string   `json:"weather_impact"`
				TidalAnalysis        string   `json:"tidal_analysis"`
				Observations         []string `json:"observations"`
			}
			if err := json.Unmarshal([]byte(cleanJSON(resp)), &parsedResp); err == nil && parsedResp.Summary != "" {
				aiSummary = strings.TrimSpace(parsedResp.Summary)
				aiRecID = strings.TrimSpace(parsedResp.RecommendedTrackID)
				aiRecName = strings.TrimSpace(parsedResp.RecommendedTrackName)
				aiConclusions = strings.TrimSpace(parsedResp.Conclusions)
				aiTacking = strings.TrimSpace(parsedResp.TackingEfficiency)
				aiWeatherImpact = strings.TrimSpace(parsedResp.WeatherImpact)
				aiTidalAnalysis = strings.TrimSpace(parsedResp.TidalAnalysis)
				aiObservations = parsedResp.Observations
			}
		}
	}

	if aiRecID != "" || aiRecName != "" {
		for i, ev := range evals {
			if (aiRecID != "" && ev.candidate.TrackID == aiRecID) ||
				(aiRecName != "" && strings.EqualFold(ev.candidate.Name, aiRecName)) {
				bestIdx = i
				break
			}
		}
	}

	bestEv := evals[bestIdx]
	runnerUpIdx := 0
	if bestIdx == 0 && len(evals) > 1 {
		runnerUpIdx = 1
	}
	runnerUpEv := evals[runnerUpIdx]

	recID := bestEv.candidate.TrackID
	recName := bestEv.candidate.Name
	distDelta := bestEv.candidate.DistanceNM - runnerUpEv.candidate.DistanceNM
	distVarPct := 0.0
	if runnerUpEv.candidate.DistanceNM > 0 {
		distVarPct = (distDelta / runnerUpEv.candidate.DistanceNM) * 100.0
	}

	summary := aiSummary
	if summary == "" {
		if math.Abs(distDelta) >= 0.3 {
			shorterLonger := "shorter"
			if distDelta > 0 {
				shorterLonger = "longer"
			}
			summary = fmt.Sprintf(
				"Recommended '%s' (%.1f NM, est. %s) over '%s' (%.1f NM, est. %s): %.1f NM %s with superior tactical sailing angles.",
				recName, bestEv.candidate.DistanceNM, bestEv.candidate.EstimatedDuration,
				runnerUpEv.candidate.Name, runnerUpEv.candidate.DistanceNM, runnerUpEv.candidate.EstimatedDuration,
				math.Abs(distDelta), shorterLonger,
			)
		} else {
			summary = fmt.Sprintf(
				"Recommended '%s' (%.1f NM, est. %s) over '%s' (%.1f NM, est. %s) for more favorable route geometry and wind exposure.",
				recName, bestEv.candidate.DistanceNM, bestEv.candidate.EstimatedDuration,
				runnerUpEv.candidate.Name, runnerUpEv.candidate.DistanceNM, runnerUpEv.candidate.EstimatedDuration,
			)
		}
	}

	conclusions := aiConclusions
	if conclusions == "" {
		if bestEv.hasWindEval && runnerUpEv.hasWindEval {
			conclusions = fmt.Sprintf(
				"'%s' (%s) reduces upwind tacking penalties compared to '%s' (%s), yielding a faster predicted transit.",
				recName, bestEv.candidate.WindAngleSummary,
				runnerUpEv.candidate.Name, runnerUpEv.candidate.WindAngleSummary,
			)
		} else {
			conclusions = fmt.Sprintf(
				"'%s' provides the most direct line of advance (%.1f NM vs %.1f NM on '%s') with lower maneuvering overhead.",
				recName, bestEv.candidate.DistanceNM, runnerUpEv.candidate.DistanceNM, runnerUpEv.candidate.Name,
			)
		}
	}

	tacking := aiTacking
	if tacking == "" {
		if bestEv.hasWindEval {
			tacking = fmt.Sprintf(
				"'%s' maintains %.0f%% upwind exposure (avg TWA %.0f°) versus %.0f%% upwind on '%s' (avg TWA %.0f°).",
				recName, bestEv.upwindPct, bestEv.avgTWA, runnerUpEv.upwindPct, runnerUpEv.candidate.Name, runnerUpEv.avgTWA,
			)
		} else {
			tacking = fmt.Sprintf(
				"'%s' minimizes waypoint course changes and tacking overhead across %.1f NM.",
				recName, bestEv.candidate.DistanceNM,
			)
		}
	}

	weatherImpact := aiWeatherImpact
	if weatherImpact == "" {
		weatherImpact = buildFallbackWeatherImpact(parsedWeather)
	}

	tidalAnalysis := aiTidalAnalysis
	if tidalAnalysis == "" {
		if briefing != nil && len(briefing.Tides) > 2 && string(briefing.Tides) != "null" && string(briefing.Tides) != "{}" && string(briefing.Tides) != "[]" {
			tidalAnalysis = fmt.Sprintf("Time departure on '%s' with slack water or favorable flood/ebb stream to avoid foul current on exposed headlands.", recName)
		} else {
			tidalAnalysis = fmt.Sprintf("Verify local tidal stream timing before departing on '%s' to maximize VMG.", recName)
		}
	}

	observations := aiObservations
	if len(observations) == 0 {
		observations = []string{
			fmt.Sprintf("Primary recommendation: sail '%s' (%.1f NM, est. %s).", recName, bestEv.candidate.DistanceNM, bestEv.candidate.EstimatedDuration),
			fmt.Sprintf("Alternate route '%s' (%.1f NM, est. %s) remains available if wind shifts.", runnerUpEv.candidate.Name, runnerUpEv.candidate.DistanceNM, runnerUpEv.candidate.EstimatedDuration),
		}
	}

	debrief := model.TrackDebrief{
		Mode:                 "comparison",
		TrackID:              recID,
		TrackName:            recName,
		DayTitle:             dayTitle,
		PlannedTrackID:       &recID,
		PlannedTrackName:     recName,
		RecommendedTrackID:   &recID,
		RecommendedTrackName: recName,
		CandidateTracks:      candidateModels,
		VoyageDayID:          startDayID,
		StartDayID:           startDayID,
		PlannedDistanceNM:    bestEv.candidate.DistanceNM,
		RecordedDistanceNM:   runnerUpEv.candidate.DistanceNM,
		DistanceDeltaNM:      math.Round(distDelta*100) / 100,
		DistanceVariancePct:  math.Round(distVarPct*10) / 10,
		PlannedDuration:      bestEv.candidate.EstimatedDuration,
		RecordedDuration:     runnerUpEv.candidate.EstimatedDuration,
		Summary:              summary,
		Conclusions:          conclusions,
		TackingEfficiency:    tacking,
		WeatherImpact:        weatherImpact,
		TidalAnalysis:        tidalAnalysis,
		Observations:         observations,
	}

	expandRouteComparisonAcronyms(&debrief)
	return &debrief
}

type acronymDef struct {
	acronym   string
	full      string
	wordRe    *regexp.Regexp
	definedRe *regexp.Regexp
}

var routeComparisonAcronyms = []acronymDef{
	{acronym: "VMG", full: "velocity made good (VMG)", wordRe: regexp.MustCompile(`\bVMG\b`), definedRe: regexp.MustCompile(`(?i)velocity\s+made\s+good\s*\(VMG\)`)},
	{acronym: "TWA", full: "true wind angle (TWA)", wordRe: regexp.MustCompile(`\bTWA\b`), definedRe: regexp.MustCompile(`(?i)true\s+wind\s+angle\s*\(TWA\)`)},
	{acronym: "TWS", full: "true wind speed (TWS)", wordRe: regexp.MustCompile(`\bTWS\b`), definedRe: regexp.MustCompile(`(?i)true\s+wind\s+speed\s*\(TWS\)`)},
	{acronym: "AWA", full: "apparent wind angle (AWA)", wordRe: regexp.MustCompile(`\bAWA\b`), definedRe: regexp.MustCompile(`(?i)apparent\s+wind\s+angle\s*\(AWA\)`)},
	{acronym: "AWS", full: "apparent wind speed (AWS)", wordRe: regexp.MustCompile(`\bAWS\b`), definedRe: regexp.MustCompile(`(?i)apparent\s+wind\s+speed\s*\(AWS\)`)},
	{acronym: "SOG", full: "speed over ground (SOG)", wordRe: regexp.MustCompile(`\bSOG\b`), definedRe: regexp.MustCompile(`(?i)speed\s+over\s+ground\s*\(SOG\)`)},
	{acronym: "COG", full: "course over ground (COG)", wordRe: regexp.MustCompile(`\bCOG\b`), definedRe: regexp.MustCompile(`(?i)course\s+over\s+ground\s*\(COG\)`)},
	{acronym: "XTE", full: "cross-track error (XTE)", wordRe: regexp.MustCompile(`\bXTE\b`), definedRe: regexp.MustCompile(`(?i)cross[- ]track\s+error\s*\(XTE\)`)},
	{acronym: "ETA", full: "estimated time of arrival (ETA)", wordRe: regexp.MustCompile(`\bETA\b`), definedRe: regexp.MustCompile(`(?i)estimated\s+time\s+of\s+arrival\s*\(ETA\)`)},
	{acronym: "ETD", full: "estimated time of departure (ETD)", wordRe: regexp.MustCompile(`\bETD\b`), definedRe: regexp.MustCompile(`(?i)estimated\s+time\s+of\s+departure\s*\(ETD\)`)},
	{acronym: "LNM", full: "Local Notice to Mariners (LNM)", wordRe: regexp.MustCompile(`\bLNM\b`), definedRe: regexp.MustCompile(`(?i)local\s+notices?\s+to\s+mariners\s*\(LNM\)`)},
}

func expandAcronymsInText(text string, seen map[string]bool) string {
	if text == "" {
		return text
	}
	out := text
	for _, def := range routeComparisonAcronyms {
		if def.definedRe.MatchString(out) {
			seen[def.acronym] = true
			continue
		}
		if seen[def.acronym] {
			continue
		}
		loc := def.wordRe.FindStringIndex(out)
		if loc != nil {
			out = out[:loc[0]] + def.full + out[loc[1]:]
			seen[def.acronym] = true
		}
	}
	return out
}

func expandRouteComparisonAcronyms(d *model.TrackDebrief) {
	if d == nil {
		return
	}
	seen := make(map[string]bool)
	// First check if any field already defines an acronym
	allTexts := []string{d.Summary, d.Conclusions, d.TackingEfficiency, d.WeatherImpact, d.TidalAnalysis}
	for _, c := range d.CandidateTracks {
		allTexts = append(allTexts, c.WindAngleSummary)
	}
	allTexts = append(allTexts, d.Observations...)
	for _, t := range allTexts {
		for _, def := range routeComparisonAcronyms {
			if def.definedRe.MatchString(t) {
				seen[def.acronym] = true
			}
		}
	}

	d.Summary = expandAcronymsInText(d.Summary, seen)
	for i := range d.CandidateTracks {
		d.CandidateTracks[i].WindAngleSummary = expandAcronymsInText(d.CandidateTracks[i].WindAngleSummary, seen)
		d.CandidateTracks[i].TidalSummary = expandAcronymsInText(d.CandidateTracks[i].TidalSummary, seen)
		for j := range d.CandidateTracks[i].AlongRouteProfile {
			d.CandidateTracks[i].AlongRouteProfile[j] = expandAcronymsInText(d.CandidateTracks[i].AlongRouteProfile[j], seen)
		}
	}
	d.Conclusions = expandAcronymsInText(d.Conclusions, seen)
	d.TackingEfficiency = expandAcronymsInText(d.TackingEfficiency, seen)
	d.WeatherImpact = expandAcronymsInText(d.WeatherImpact, seen)
	d.TidalAnalysis = expandAcronymsInText(d.TidalAnalysis, seen)
	for i := range d.Observations {
		d.Observations[i] = expandAcronymsInText(d.Observations[i], seen)
	}
}

func deduplicateCandidateModels(cands []model.PlannedTrackCandidate) []model.PlannedTrackCandidate {
	seen := make(map[string]bool)
	var unique []model.PlannedTrackCandidate
	for _, c := range cands {
		hdgStr := ""
		if c.DominantHeading != nil {
			hdgStr = fmt.Sprintf("%.0f", *c.DominantHeading)
		}
		sig := fmt.Sprintf("%s|%.1f|%s", strings.ToLower(strings.TrimSpace(c.Name)), c.DistanceNM, hdgStr)
		if seen[sig] {
			continue
		}
		seen[sig] = true
		unique = append(unique, c)
	}
	return unique
}

// deduplicateVoyageTracks collapses duplicate uploads that share the exact same Kind, Name,
// and route geometry signature. When pruneDB is true, redundant duplicate rows are deleted from DB.
func (h *Handler) deduplicateVoyageTracks(ctx context.Context, tracks []model.VoyageTrack, pruneDB bool) []model.VoyageTrack {
	if len(tracks) <= 1 {
		return tracks
	}
	seen := make(map[string]bool)
	var unique []model.VoyageTrack
	for _, t := range tracks {
		geoSig := plannedTrackGeometrySignature(&t)
		key := fmt.Sprintf("%s|%s|%s", strings.ToLower(strings.TrimSpace(t.Kind)), strings.ToLower(strings.TrimSpace(t.Name)), geoSig)
		if geoSig != "" && seen[key] {
			if pruneDB && h.DB != nil && t.ID != "" {
				_ = h.DB.DeleteVoyageTrack(ctx, t.ID)
			}
			continue
		}
		if geoSig != "" {
			seen[key] = true
		}
		unique = append(unique, t)
	}
	return unique
}

// sanitizeTrackComparisonDebriefs ensures persisted comparison debriefs on tracks only
// include candidates that belong to the same overlapping candidate group (same leg/day and endpoints).
func (h *Handler) sanitizeTrackComparisonDebriefs(ctx context.Context, tracks []model.VoyageTrack, days []model.VoyageDay, pruneDB bool) []model.VoyageTrack {
	if len(tracks) == 0 {
		return tracks
	}
	candidateGroups := groupCandidatePlannedTracks(tracks, days)
	trackGroupAllowedIDs := make(map[string]map[string]bool)
	trackGroupAllowedNames := make(map[string]map[string]bool)
	for _, g := range candidateGroups {
		if len(g.tracks) < 2 {
			continue
		}
		ids := make(map[string]bool, len(g.tracks))
		names := make(map[string]bool, len(g.tracks))
		for _, gt := range g.tracks {
			if gt.ID != "" {
				ids[gt.ID] = true
			}
			if gt.Name != "" {
				names[strings.ToLower(strings.TrimSpace(gt.Name))] = true
			}
		}
		for _, gt := range g.tracks {
			if gt.ID != "" {
				trackGroupAllowedIDs[gt.ID] = ids
				trackGroupAllowedNames[gt.ID] = names
			}
		}
	}

	for i := range tracks {
		t := &tracks[i]
		if len(t.Debrief) == 0 || string(t.Debrief) == "null" {
			continue
		}
		var d model.TrackDebrief
		if err := json.Unmarshal(t.Debrief, &d); err != nil {
			continue
		}
		if d.Mode != "comparison" && len(d.CandidateTracks) < 2 {
			continue
		}
		allowedIDs := trackGroupAllowedIDs[t.ID]
		allowedNames := trackGroupAllowedNames[t.ID]
		if len(allowedIDs) < 2 && len(allowedNames) < 2 {
			t.Debrief = nil
			if pruneDB && h.DB != nil && t.ID != "" {
				_ = h.DB.UpdateVoyageTrackDebrief(ctx, t.ID, model.RawJSON("null"))
			}
			continue
		}
		var filtered []model.PlannedTrackCandidate
		for _, c := range deduplicateCandidateModels(d.CandidateTracks) {
			cName := strings.ToLower(strings.TrimSpace(c.Name))
			if (c.TrackID != "" && allowedIDs[c.TrackID]) || (cName != "" && allowedNames[cName]) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) < 2 {
			t.Debrief = nil
			if pruneDB && h.DB != nil && t.ID != "" {
				_ = h.DB.UpdateVoyageTrackDebrief(ctx, t.ID, model.RawJSON("null"))
			}
			continue
		}
		if len(filtered) != len(d.CandidateTracks) {
			d.CandidateTracks = filtered
			if updatedBytes, err := json.Marshal(&d); err == nil {
				t.Debrief = model.RawJSON(updatedBytes)
				if pruneDB && h.DB != nil && t.ID != "" {
					_ = h.DB.UpdateVoyageTrackDebrief(ctx, t.ID, model.RawJSON(updatedBytes))
				}
			}
		}
	}
	return tracks
}

func hasConflictingCandidateDays(cands []model.PlannedTrackCandidate) bool {
	seenLeg := 0
	for _, c := range cands {
		leg := extractLegNumber(c.Name)
		if leg > 0 {
			if seenLeg == 0 {
				seenLeg = leg
			} else if seenLeg != leg {
				return true
			}
		}
	}
	return false
}

// collectVoyageReportDebriefs gathers persisted debriefs for a voyage's tracks and
// automatically generates (and persists) multi-plan route comparisons for any period/leg
// that has 2+ candidate planned routes without an up-to-date comparison debrief.
func (h *Handler) collectVoyageReportDebriefs(ctx context.Context, tracks []model.VoyageTrack, days []model.VoyageDay) []*model.TrackDebrief {
	tracks = h.deduplicateVoyageTracks(ctx, tracks, true)

	candidateGroups := groupCandidatePlannedTracks(tracks, days)

	trackToGroupIdx := make(map[string]int)
	for gIdx, g := range candidateGroups {
		for _, gt := range g.tracks {
			if gt.ID != "" {
				trackToGroupIdx[gt.ID] = gIdx
			}
		}
	}

	var debriefs []*model.TrackDebrief
	coveredComparisonTracks := make(map[string]bool)
	coveredGroups := make(map[int]bool)

	for _, t := range tracks {
		if len(t.Debrief) > 0 && string(t.Debrief) != "null" {
			var d model.TrackDebrief
			if err := json.Unmarshal(t.Debrief, &d); err == nil {
				if d.TrackName == "" && t.Name != "" {
					d.TrackName = t.Name
				}
				if d.TrackID == "" && t.ID != "" {
					d.TrackID = t.ID
				}
				if d.VoyageDayID == nil && t.VoyageDayID != nil {
					d.VoyageDayID = t.VoyageDayID
				}
				ResolveDebriefDay(&d, &t, days)
				if d.Mode == "comparison" || len(d.CandidateTracks) >= 2 {
					d.CandidateTracks = deduplicateCandidateModels(d.CandidateTracks)
					gIdx, inValidGroup := trackToGroupIdx[t.ID]
					if len(d.CandidateTracks) < 2 || hasConflictingCandidateDays(d.CandidateTracks) || !inValidGroup {
						if h.DB != nil && t.ID != "" {
							_ = h.DB.UpdateVoyageTrackDebrief(ctx, t.ID, model.RawJSON("null"))
						}
						continue
					}
					if coveredGroups[gIdx] {
						continue
					}
					grp := candidateGroups[gIdx]
					if len(grp.tracks) < 2 || len(d.CandidateTracks) != len(grp.tracks) {
						if h.DB != nil && t.ID != "" {
							_ = h.DB.UpdateVoyageTrackDebrief(ctx, t.ID, model.RawJSON("null"))
						}
						continue
					}
					coveredGroups[gIdx] = true
					for _, gt := range grp.tracks {
						coveredComparisonTracks[gt.ID] = true
					}
					expandRouteComparisonAcronyms(&d)
					coveredComparisonTracks[t.ID] = true
					for _, c := range d.CandidateTracks {
						if c.TrackID != "" {
							coveredComparisonTracks[c.TrackID] = true
						}
					}
				}
				debriefs = append(debriefs, &d)
			}
		}
	}

	for gIdx, g := range candidateGroups {
		if len(g.tracks) < 2 || coveredGroups[gIdx] {
			continue
		}
		allCovered := true
		for _, gt := range g.tracks {
			if !coveredComparisonTracks[gt.ID] {
				allCovered = false
				break
			}
		}
		if allCovered {
			continue
		}
		compDebrief := h.generateMultiPlanComparison(ctx, g.tracks, g.day, days)
		if compDebrief == nil {
			continue
		}
		h.persistComparisonDebrief(ctx, compDebrief, g.tracks)
		coveredGroups[gIdx] = true
		for _, gt := range g.tracks {
			coveredComparisonTracks[gt.ID] = true
		}
		debriefs = append(debriefs, compDebrief)
	}

	return debriefs
}
