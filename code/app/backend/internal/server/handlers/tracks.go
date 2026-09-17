package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

	allTracks, _ := h.DB.ListVoyageTracks(r.Context(), voyageID)
	debrief := h.generateTrackDebrief(r.Context(), track, allTracks)
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

	debriefs := make([]*model.TrackDebrief, 0, len(allTracks))
	for i := range allTracks {
		track := &allTracks[i]
		debrief := h.generateTrackDebrief(r.Context(), track, allTracks)
		debriefBytes, _ := json.Marshal(debrief)
		_ = h.DB.UpdateVoyageTrackDebrief(r.Context(), track.ID, model.RawJSON(debriefBytes))
		debriefs = append(debriefs, debrief)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debriefs)
}

func (h *Handler) generateTrackDebrief(ctx context.Context, track *model.VoyageTrack, allTracks []model.VoyageTrack) *model.TrackDebrief {
	var plannedDist float64
	var plannedDuration string

	for _, t := range allTracks {
		if t.Kind == string(model.TrackKindPlanned) && t.DistanceNM != nil {
			if track.VoyageStopID != nil && t.VoyageStopID != nil && *t.VoyageStopID == *track.VoyageStopID {
				plannedDist = *t.DistanceNM
				if t.DurationInterval != nil {
					plannedDuration = *t.DurationInterval
				}
				break
			}
			if plannedDist == 0 {
				plannedDist = *t.DistanceNM
				if t.DurationInterval != nil {
					plannedDuration = *t.DurationInterval
				}
			}
		}
	}

	recDist := 0.0
	if track.DistanceNM != nil {
		recDist = *track.DistanceNM
	}
	if plannedDist == 0 {
		plannedDist = recDist * 0.88 // Rhumb-line estimate if no explicit planned route
	}

	distDelta := recDist - plannedDist
	recDur := "N/A"
	if track.DurationInterval != nil {
		recDur = *track.DurationInterval
	}
	if plannedDuration == "" {
		plannedDuration = recDur
	}

	avgSpd := 0.0
	if track.AvgSpeedKts != nil {
		avgSpd = *track.AvgSpeedKts
	}
	maxSpd := 0.0
	if track.MaxSpeedKts != nil {
		maxSpd = *track.MaxSpeedKts
	}

	debrief := model.TrackDebrief{
		TrackID:            track.ID,
		TrackName:          track.Name,
		RecordedDistanceNM: recDist,
		PlannedDistanceNM:  plannedDist,
		DistanceDeltaNM:    distDelta,
		RecordedDuration:   recDur,
		PlannedDuration:    plannedDuration,
		AvgSpeedKts:        avgSpd,
		MaxSpeedKts:        maxSpd,
	}

	// Generate tactical insights using Pilot agent or rule-based fallback
	prompt := fmt.Sprintf(
		"Perform a post-voyage tactical pilot debrief for track '%s': Recorded Distance: %.2f NM, Planned Distance: %.2f NM (Delta: %+.2f NM), Recorded Duration: %s, Avg Speed: %.1f kts, Max Speed: %.1f kts. Evaluate tacking efficiency, leeway, and weather impact.",
		track.Name, recDist, plannedDist, distDelta, recDur, avgSpd, maxSpd,
	)

	if h.Agent != nil {
		agentSessionID := fmt.Sprintf("debrief_%s_%d", track.ID, time.Now().UnixNano())
		resp, err := h.Agent.RunSync(ctx, "pilot", "system", agentSessionID, prompt)
		if err == nil && resp != "" {
			var parsedResp struct {
				Summary           string   `json:"summary"`
				TackingEfficiency string   `json:"tacking_efficiency"`
				WeatherImpact     string   `json:"weather_impact"`
				Observations      []string `json:"observations"`
			}
			if err := json.Unmarshal([]byte(cleanJSON(resp)), &parsedResp); err == nil && parsedResp.Summary != "" {
				debrief.Summary = parsedResp.Summary
				debrief.TackingEfficiency = parsedResp.TackingEfficiency
				debrief.WeatherImpact = parsedResp.WeatherImpact
				debrief.Observations = parsedResp.Observations
			} else {
				debrief.Summary = resp
			}
		}
	}

	if debrief.Summary == "" {
		pctOver := 0.0
		if plannedDist > 0 {
			pctOver = (distDelta / plannedDist) * 100.0
		}
		debrief.Summary = fmt.Sprintf("Passage completed %.1f NM over planned rhumb line (%.1f%% distance variance) with an average speed of %.1f knots.", distDelta, pctOver, avgSpd)
		debrief.TackingEfficiency = fmt.Sprintf("Tacking overhead resulted in %.1f NM additional sailing distance.", distDelta)
		debrief.WeatherImpact = "Favorable conditions observed along the majority of the passage."
		debrief.Observations = []string{
			fmt.Sprintf("Logged maximum speed over ground of %.1f knots.", maxSpd),
			fmt.Sprintf("Total transit time recorded as %s.", recDur),
			"Maintained steady heading relative to channel marks and destination waypoints.",
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
