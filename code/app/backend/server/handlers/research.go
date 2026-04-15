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
	"sync"
	"time"

	appcontext "app/context"
	"app/models"
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

func (h *Handler) getStaticMap(lat, lng float64) ([]byte, error) {
	apiKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY not set")
	}

	endpoint := fmt.Sprintf("https://maps.googleapis.com/maps/api/staticmap?center=%f,%f&zoom=12&size=600x400&maptype=roadmap&markers=color:red%%7C%f,%f&key=%s",
		lat, lng, lat, lng, apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("static map request failed with status: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func geocodeFacility(name, vicinity string, centerLat, centerLng float64) (float64, float64, error) {
	apiKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if apiKey == "" {
		return 0, 0, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY not set")
	}

	query := fmt.Sprintf("%s, %s", name, vicinity)

	// Create a bounding box roughly +/- 0.5 degrees around the stop (approx 30 miles)
	bounds := fmt.Sprintf("%f,%f|%f,%f", centerLat-0.5, centerLng-0.5, centerLat+0.5, centerLng+0.5)

	endpoint := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&bounds=%s&key=%s",
		url.QueryEscape(query), url.QueryEscape(bounds), apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
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
		return 0, 0, err
	}

	if result.Status != "OK" || len(result.Results) == 0 {
		return 0, 0, fmt.Errorf("geocoding failed: %s", result.Status)
	}

	return result.Results[0].Geometry.Location.Lat, result.Results[0].Geometry.Location.Lng, nil
}

func (h *Handler) TriggerResearch(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
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

	// Check ownership via Voyage
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	sessionID := fmt.Sprintf("stop_%d_%d", stop.ID, time.Now().Unix())

	// Pre-register progress channel before spawning goroutine so early events are buffered.
	h.ensureProgressChannel(sessionID, 25*time.Minute)

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": idStr, "session_id": sessionID})

	// Async processing
	go h.performStopResearch(stop, sessionID)
}

func (h *Handler) performStopResearch(stop *models.Stop, sessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performStopResearchLogic(stop, sessionID)
}

func (h *Handler) performStopResearchLogic(stop *models.Stop, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[researcher-agent] Starting research for stop %d", stop.ID))
	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Starting research for %s", stop.LocationName))

	// Item 16: Check if this is the last stop and identical to the first stop
	stops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
	if err == nil && len(stops) > 1 {
		first := stops[0]
		last := stops[len(stops)-1]

		if stop.ID == last.ID {
			// Compare coordinates
			const threshold = 0.001 // Approx 111 meters
			latDiff := math.Abs(first.Latitude - last.Latitude)
			lngDiff := math.Abs(first.Longitude - last.Longitude)

			if latDiff < threshold && lngDiff < threshold {
				slog.InfoContext(ctx, "Redundant last stop detected, attempting to clone briefing from first stop", "stop_id", stop.ID, "first_stop_id", first.ID)

				firstBriefing, err := h.DB.GetBriefing(ctx, first.ID)
				if err == nil && firstBriefing != nil {
					h.broadcastProgress(sessionID, "clone", fmt.Sprintf("Cloning research from first stop for %s", stop.LocationName))

					newBriefing := &models.Briefing{
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
	// agentSessionID was stable per stop for ADK session management, but we now use a timestamp
	// to ensure a fresh session and avoid 500 errors if the session already exists.
	agentSessionID := fmt.Sprintf("stop_%d_%d", stop.ID, time.Now().Unix())

	// Check for nearby existing research to reuse facilities
	nearbyBriefing, nearbyErr := h.DB.GetNearbyBriefing(ctx, stop.Latitude, stop.Longitude)
	var reusableFacilities json.RawMessage

	// 1. Create Session
	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	h.broadcastProgress(sessionID, "agent", fmt.Sprintf("Consulting the Harbourmaster for %s", stop.LocationName))

	// 2. Build prompt
	var locInfo string
	if stop.PreciseLocation != "" {
		locInfo = fmt.Sprintf("%s (Lat: %f, Lng: %f)", stop.LocationName, stop.Latitude, stop.Longitude)
	} else {
		locInfo = stop.LocationName
	}

	prompt := fmt.Sprintf("Research anchorages and weather for %f N, %f W (%s) for %s. Radius %d %s.",
		stop.Latitude, stop.Longitude, locInfo, stop.TargetDate.Format("January 2, 2006"), stop.SearchRadius, stop.SearchRadiusUnit)

	if nearbyErr == nil && nearbyBriefing != nil && len(nearbyBriefing.Facilities) > 0 {
		slog.InfoContext(ctx, fmt.Sprintf("Found nearby existing briefing %d, reusing facilities", nearbyBriefing.ID))
		reusableFacilities = json.RawMessage(nearbyBriefing.Facilities)
		prompt += " Do not research facilities; I will provide those separately."
	}

	// 3. Run Agent
	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
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

	// Post-process facilities to fix missing or imprecise coordinates
	var facilities []Facility
	if err := json.Unmarshal(output.Facilities, &facilities); err == nil {
		var wg sync.WaitGroup
		// var mu sync.Mutex // Removed as we always re-marshal now for sorting

		for i := range facilities {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				f := facilities[i]
				slog.InfoContext(ctx, fmt.Sprintf("Geocoding facility: %s near %s", f.Name, stop.LocationName))
				lat, lng, err := geocodeFacility(f.Name, stop.LocationName, stop.Latitude, stop.Longitude)
				if err == nil {
					facilities[i].Latitude = lat
					facilities[i].Longitude = lng
				} else {
					slog.WarnContext(ctx, fmt.Sprintf("Failed to geocode facility %s", f.Name), "error", err)
				}
			}(i)
		}
		wg.Wait()

		// Sort facilities by Type, then Name
		sort.Slice(facilities, func(i, j int) bool {
			if facilities[i].Type != facilities[j].Type {
				return facilities[i].Type < facilities[j].Type
			}
			return facilities[i].Name < facilities[j].Name
		})

		newBytes, _ := json.Marshal(facilities)
		output.Facilities = json.RawMessage(newBytes)
	}

	briefing := &models.Briefing{
		StopID:         stop.ID,
		WeatherSummary: models.RawJSON(output.WeatherSummary),
		SunPhase:       models.RawJSON(output.SunPhase),
		Tides:          models.RawJSON(output.Tides),
		Facilities:     models.RawJSON(output.Facilities),
	}

	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save briefing", "error", err)
	}
	slog.InfoContext(ctx, fmt.Sprintf("Briefing saved for stop %d", stop.ID))
	h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete for %s", stop.LocationName))
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
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

	// Check ownership via Stop -> Voyage
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
	person := appcontext.GetPersonFromContext(r.Context())
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

	sessionID := fmt.Sprintf("voyage_%d_%d", voyageID, time.Now().Unix())

	// Pre-register progress channel before spawning goroutines so early events are buffered.
	h.ensureProgressChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"msg":        "Full research started",
		"voyage_id":  voyageID,
		"stop_count": len(stops),
		"session_id": sessionID,
	})

	go func() {
		ctx := context.Background()
		slog.InfoContext(ctx, fmt.Sprintf("[research-coordinator] Starting full research for voyage %d", voyageID))

		var wg sync.WaitGroup

		// 1. Research Voyage Guide (Parallel)
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.InfoContext(ctx, fmt.Sprintf("Starting guide research for voyage %d", voyageID))
			h.performGuideResearch(voyage, sessionID)
		}()

		// Identify redundant last stop to avoid double AI calls in parallel runs
		var redundantLastStop *models.Stop
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

		// 2. Research each stop (Parallel)
		for _, stop := range stops {
			if redundantLastStop != nil && stop.ID == redundantLastStop.ID {
				continue // Skip the redundant last stop for now
			}
			wg.Add(1)
			h.ResearchSem <- struct{}{} // Block until a slot is available
			go func(s models.Stop) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }() // Release slot
				slog.InfoContext(ctx, fmt.Sprintf("Starting stop research for stop %d", s.ID))
				h.performStopResearchLogic(&s, sessionID)
			}(stop)
		}

		wg.Wait()

		// 3. Handle redundant last stop by cloning the first stop's briefing
		if redundantLastStop != nil {
			slog.InfoContext(ctx, "Cloning first stop briefing to redundant last stop", "voyage_id", voyageID, "last_stop_id", redundantLastStop.ID)
			firstBriefing, err := h.DB.GetBriefing(ctx, firstStopID)
			if err == nil && firstBriefing != nil {
				newBriefing := &models.Briefing{
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
				// Fallback: If for some reason the first one failed or is missing, try researching it normally
				slog.WarnContext(ctx, "First stop briefing missing for cloning, falling back to full research for last stop")
				h.performStopResearchLogic(redundantLastStop, sessionID)
			}
		}

		slog.InfoContext(ctx, fmt.Sprintf("Full research complete for voyage %d", voyageID))
	}()
}

func (h *Handler) saveEmptyBriefing(ctx context.Context, stop *models.Stop) {
	briefing := &models.Briefing{
		StopID:         stop.ID,
		WeatherSummary: models.RawJSON([]byte(`{"summary":"Error: Agent failed to respond"}`)),
		SunPhase:       models.RawJSON([]byte(`{}`)),
		Tides:          models.RawJSON([]byte(`{}`)),
		Facilities:     models.RawJSON([]byte(`[]`)),
	}
	h.DB.CreateBriefing(ctx, briefing)
}
