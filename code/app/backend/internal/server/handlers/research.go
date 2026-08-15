package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"app/internal/model"
)

type AgentOutput struct {
	LocationName   string          `json:"location_name"`
	WeatherSummary json.RawMessage `json:"weather_summary"`
	SunPhase       json.RawMessage `json:"sun_phase"`
	Tides          json.RawMessage `json:"tides"`
	Facilities     json.RawMessage `json:"facilities"`
}

type Facility struct {
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Website         string          `json:"website,omitempty"`
	Address         string          `json:"address,omitempty"`
	Latitude        float64         `json:"latitude"`
	Longitude       float64         `json:"longitude"`
	Rating          float64         `json:"rating,omitempty"`
	UserRatingCount int             `json:"user_rating_count,omitempty"`
	BusinessStatus  string          `json:"business_status,omitempty"`
	Details         json.RawMessage `json:"details"`
	References      []string        `json:"references"`
}

func (h *Handler) getStaticMap(ctx context.Context, lat, lng float64) ([]byte, error) {
	apiKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY not set")
	}

	endpoint := fmt.Sprintf("https://maps.googleapis.com/maps/api/staticmap?center=%f,%f&zoom=12&size=600x400&maptype=roadmap&markers=color:red%%7C%f,%f&key=%s",
		lat, lng, lat, lng, apiKey)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create static map request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute static map request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("static map request failed with status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read static map body: %w", err)
	}
	return data, nil
}

func geocodeFacility(ctx context.Context, name, vicinity string, centerLat, centerLng float64) (float64, float64, error) {
	apiKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if apiKey == "" {
		return 0, 0, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY not set")
	}

	query := fmt.Sprintf("%s, %s", name, vicinity)
	bounds := fmt.Sprintf("%f,%f|%f,%f", centerLat-0.5, centerLng-0.5, centerLat+0.5, centerLng+0.5)

	endpoint := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&bounds=%s&key=%s",
		url.QueryEscape(query), url.QueryEscape(bounds), apiKey)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create geocode request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("execute geocode request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Results []struct {
			Geometry struct {
				Location struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, fmt.Errorf("decode geocode response: %w", err)
	}

	if result.Status != "OK" || len(result.Results) == 0 {
		return 0, 0, fmt.Errorf("geocoding failed with status: %s", result.Status)
	}

	return result.Results[0].Geometry.Location.Lat, result.Results[0].Geometry.Location.Lng, nil
}

func (h *Handler) TriggerResearch(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Stop ID")
		return
	}

	stop, err := h.DB.GetStop(r.Context(), stopID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Stop not found")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	jobKey := fmt.Sprintf("stop:%d", stop.ID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Research is already in progress for this stop")
		return
	}

	sessionID := fmt.Sprintf("stop_%d_%d", stop.ID, time.Now().Unix())

	h.ensureProgressChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": idStr, "session_id": sessionID})

	go h.performStopResearch(stop, sessionID, jobKey)
}

func (h *Handler) performStopResearch(stop *model.Stop, sessionID, jobKey string) {
	defer h.releaseJob(jobKey)
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performStopResearchLogic(stop, sessionID)
}

func hasValidFacilities(raw model.RawJSON) bool {
	if len(raw) == 0 {
		return false
	}
	var facs []Facility
	if err := json.Unmarshal(raw, &facs); err != nil {
		return false
	}
	return len(facs) > 0
}

func (h *Handler) performStopResearchLogic(stop *model.Stop, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[researcher-agent] Starting research for stop %d", stop.ID))
	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Starting research for %s", stop.LocationName))

	stops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
	if err == nil && len(stops) > 1 {
		first := stops[0]
		last := stops[len(stops)-1]

		if stop.ID == last.ID {
			const threshold = 0.001
			latDiff := math.Abs(first.Latitude - last.Latitude)
			lngDiff := math.Abs(first.Longitude - last.Longitude)

			if latDiff < threshold && lngDiff < threshold {
				slog.InfoContext(ctx, "Redundant last stop detected, attempting to clone briefing from first stop", "stop_id", stop.ID, "first_stop_id", first.ID)

				firstBriefing, err := h.DB.GetBriefing(ctx, first.ID)
				if err == nil && firstBriefing != nil && hasValidFacilities(firstBriefing.Facilities) {
					h.broadcastProgress(sessionID, "clone", fmt.Sprintf("Cloning research from first stop for %s", stop.LocationName))

					newBriefing := &model.Briefing{
						StopID:         stop.ID,
						WeatherSummary: firstBriefing.WeatherSummary,
						SunPhase:       firstBriefing.SunPhase,
						Tides:          firstBriefing.Tides,
						Facilities:     firstBriefing.Facilities,
					}
					if err := h.DB.CreateBriefing(ctx, newBriefing); err == nil {
						h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete (reused) for %s", stop.LocationName))
						return
					}
				}
			}
		}
	}

	const appName = "harbourmaster"
	const userID = "system"
	agentSessionID := fmt.Sprintf("stop_%d_%d", stop.ID, time.Now().Unix())

	isPassage := stop.StopType == model.StopTypePassagePoint

	var nearbyBriefing *model.Briefing
	var nearbyErr error
	var reusableFacilities json.RawMessage
	if !isPassage {
		nearbyBriefing, nearbyErr = h.DB.GetNearbyBriefing(ctx, stop.Latitude, stop.Longitude)
	}

	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	h.broadcastProgress(sessionID, "agent", fmt.Sprintf("Consulting the Harbourmaster for %s", stop.LocationName))

	var prompt string
	if isPassage {
		prompt = fmt.Sprintf("This is an open-ocean passage position with no landfall. Do NOT research anchorages, marinas, moorings, or any shore facilities. "+
			"Report the underway conditions for %f N, %f W on %s: weather (wind speed and direction, wave height, swell period), "+
			"tides and tidal currents, sun phase (sunrise/sunset), and any safety or navigational alerts. "+
			"Populate weather_summary, tides, and sun_phase, and return an empty array for facilities.",
			stop.Latitude, stop.Longitude, stop.TargetDate.Format("January 2, 2006"))
	} else {
		var locInfo string
		if stop.PreciseLocation != "" {
			locInfo = fmt.Sprintf("%s (Lat: %f, Lng: %f)", stop.LocationName, stop.Latitude, stop.Longitude)
		} else {
			locInfo = stop.LocationName
		}

		prompt = fmt.Sprintf("Research anchorages and weather for %f N, %f W (%s) for %s. Radius %d %s.",
			stop.Latitude, stop.Longitude, locInfo, stop.TargetDate.Format("January 2, 2006"), stop.SearchRadius, stop.SearchRadiusUnit)

		if nearbyErr == nil && nearbyBriefing != nil && hasValidFacilities(nearbyBriefing.Facilities) {
			slog.InfoContext(ctx, fmt.Sprintf("Found nearby existing briefing %d, reusing facilities", nearbyBriefing.ID))
			reusableFacilities = json.RawMessage(nearbyBriefing.Facilities)
			prompt += " Do not research facilities; I will provide those separately."
		}
	}

	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "high demand") {
			h.broadcastProgress(sessionID, "error_503", "Model is busy due to high demand. Please try again in a few minutes.")
		}
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	if responseText == "" {
		slog.ErrorContext(ctx, "No response from agent")
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	responseText = cleanJSON(responseText)

	var output AgentOutput
	if err := json.Unmarshal([]byte(responseText), &output); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", responseText)
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	if reusableFacilities != nil {
		output.Facilities = reusableFacilities
	}

	if isPassage {
		output.Facilities = json.RawMessage(`[]`)
	}

	var facilities []Facility
	if err := json.Unmarshal(output.Facilities, &facilities); err == nil && !isPassage {
		var wg sync.WaitGroup

		for i := range facilities {
			if facilities[i].Latitude != 0 && facilities[i].Longitude != 0 {
				continue
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				f := facilities[i]
				slog.InfoContext(ctx, fmt.Sprintf("Geocoding facility missing coordinates: %s near %s", f.Name, stop.LocationName))
				lat, lng, err := geocodeFacility(ctx, f.Name, stop.LocationName, stop.Latitude, stop.Longitude)
				if err == nil {
					facilities[i].Latitude = lat
					facilities[i].Longitude = lng
				} else {
					slog.WarnContext(ctx, fmt.Sprintf("Failed to geocode facility %s", f.Name), "error", err)
				}
			}(i)
		}
		wg.Wait()

		sort.Slice(facilities, func(i, j int) bool {
			if facilities[i].Type != facilities[j].Type {
				return facilities[i].Type < facilities[j].Type
			}
			return facilities[i].Name < facilities[j].Name
		})

		newBytes, _ := json.Marshal(facilities)
		output.Facilities = json.RawMessage(newBytes)
	}

	briefing := &model.Briefing{
		StopID:         stop.ID,
		WeatherSummary: model.RawJSON(output.WeatherSummary),
		SunPhase:       model.RawJSON(output.SunPhase),
		Tides:          model.RawJSON(output.Tides),
		Facilities:     model.RawJSON(output.Facilities),
	}

	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save briefing", "error", err)
	}
	slog.InfoContext(ctx, fmt.Sprintf("Briefing saved for stop %d", stop.ID))
	h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete for %s", stop.LocationName))
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Stop ID")
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), stopID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Briefing not found")
		return
	}

	stop, err := h.DB.GetStop(r.Context(), briefing.StopID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Stop not found")
		return
	}
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}

func (h *Handler) TriggerFullVoyageResearch(w http.ResponseWriter, r *http.Request) {
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

	stops, err := h.DB.ListStops(r.Context(), voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list stops")
		return
	}

	fullJobKey := fmt.Sprintf("full:%d", voyageID)
	if !h.tryClaimJob(fullJobKey) {
		writeError(w, http.StatusConflict, "Full voyage research is already in progress")
		return
	}

	sessionID := fmt.Sprintf("voyage_%d_%d", voyageID, time.Now().Unix())

	h.ensureProgressChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"msg":        "Full research started",
		"voyage_id":  voyageID,
		"stop_count": len(stops),
		"session_id": sessionID,
	})

	go func() {
		defer h.releaseJob(fullJobKey)
		ctx := context.Background()
		slog.InfoContext(ctx, fmt.Sprintf("[research-coordinator] Starting full research for voyage %d", voyageID))

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.InfoContext(ctx, fmt.Sprintf("Starting guide research for voyage %d", voyageID))
			h.ResearchSem <- struct{}{}
			defer func() { <-h.ResearchSem }()
			h.performGuideResearchLogic(voyage, sessionID)
		}()

		var redundantLastStop *model.Stop
		var firstStopID int64
		if len(stops) > 1 {
			first := stops[0]
			last := stops[len(stops)-1]
			firstStopID = first.ID
			const threshold = 0.001
			if math.Abs(first.Latitude-last.Latitude) < threshold && math.Abs(first.Longitude-last.Longitude) < threshold {
				redundantLastStop = &last
			}
		}

		for _, stop := range stops {
			if redundantLastStop != nil && stop.ID == redundantLastStop.ID {
				continue
			}
			wg.Add(1)
			h.ResearchSem <- struct{}{}
			go func(s model.Stop) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }()
				slog.InfoContext(ctx, fmt.Sprintf("Starting stop research for stop %d", s.ID))
				h.performStopResearchLogic(&s, sessionID)
			}(stop)
		}

		wg.Wait()

		if redundantLastStop != nil {
			slog.InfoContext(ctx, "Cloning first stop briefing to redundant last stop", "voyage_id", voyageID, "last_stop_id", redundantLastStop.ID)
			firstBriefing, err := h.DB.GetBriefing(ctx, firstStopID)
			if err == nil && firstBriefing != nil && hasValidFacilities(firstBriefing.Facilities) {
				newBriefing := &model.Briefing{
					StopID:         redundantLastStop.ID,
					WeatherSummary: firstBriefing.WeatherSummary,
					SunPhase:       firstBriefing.SunPhase,
					Tides:          firstBriefing.Tides,
					Facilities:     firstBriefing.Facilities,
				}
				if err := h.DB.CreateBriefing(ctx, newBriefing); err != nil {
					slog.ErrorContext(ctx, "Failed to clone briefing for redundant last stop", "error", err)
				} else {
					h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete (reused) for %s", redundantLastStop.LocationName))
				}
			} else {
				slog.WarnContext(ctx, "First stop briefing missing or invalid for cloning, falling back to full research for last stop")
				h.performStopResearchLogic(redundantLastStop, sessionID)
			}
		}

		slog.InfoContext(ctx, fmt.Sprintf("Full research complete for voyage %d", voyageID))
	}()
}

func (h *Handler) saveEmptyBriefing(ctx context.Context, stop *model.Stop) {
	briefing := &model.Briefing{
		StopID:         stop.ID,
		WeatherSummary: model.RawJSON([]byte(`{"summary":"Error: Agent failed to respond"}`)),
		SunPhase:       model.RawJSON([]byte(`{}`)),
		Tides:          model.RawJSON([]byte(`{}`)),
		Facilities:     model.RawJSON([]byte(`[]`)),
	}
	h.DB.CreateBriefing(ctx, briefing)
}
