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
	days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)
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

// DebriefAllVoyageTracks triggers an AI after-action review for all tracks of a voyage.
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

	hasPlanned := false
	hasActual := false
	for _, t := range allTracks {
		if t.Kind == string(model.TrackKindPlanned) {
			hasPlanned = true
		} else {
			hasActual = true
		}
	}
	if !hasPlanned || !hasActual {
		writeError(w, http.StatusBadRequest, "Debrief requires both a planned route and an actual recorded track.")
		return
	}

	days, _ := h.DB.ListDays(r.Context(), voyageID, 100, 0)
	debriefs := make([]*model.TrackDebrief, 0, len(allTracks))
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

	if len(debriefs) == 0 {
		writeError(w, http.StatusBadRequest, "Debrief requires both a planned route and an actual recorded track.")
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
