package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

type AgentRunRequest struct {
	AppName    string `json:"appName"`
	UserID     string `json:"userId"`
	SessionID  string `json:"sessionId"`
	Stream     bool   `json:"stream"`
	NewMessage struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"newMessage"`
}

type AgentEvent struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
}

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

func GeocodeFacility(name, vicinity string, centerLat, centerLng float64) (float64, float64, error) {
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	stop, err := h.DB.GetStop(r.Context(), stopID)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusNotFound)
		return
	}

	// Check ownership via Voyage
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": idStr})

	// Async processing
	go h.performStopResearch(stop)
}

func (h *Handler) performStopResearch(stop *models.Stop) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performStopResearchLogic(stop)
}

func (h *Handler) performStopResearchLogic(stop *models.Stop) {
	ctx := context.Background()
	slog.InfoContext(ctx, fmt.Sprintf("[researcher-agent] Starting research for stop %d", stop.ID))

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "harbourmaster"
	userID := "system"
	sessionID := fmt.Sprintf("stop_%d", stop.ID)

	client := h.AgentClient

	// Check for nearby existing research to reuse facilities
	nearbyBriefing, nearbyErr := h.DB.GetNearbyBriefing(ctx, stop.Latitude, stop.Longitude)
	var reusableFacilities json.RawMessage

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyBriefing(ctx, stop)
		return
	}
	respSession.Body.Close()
	if respSession.StatusCode >= http.StatusInternalServerError {
		slog.ErrorContext(ctx, "Agent session creation returned server error", "status", respSession.StatusCode)
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	// 2. Run Agent
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

	reqBody := AgentRunRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	}
	reqBody.NewMessage.Role = "user"
	reqBody.NewMessage.Parts = []struct {
		Text string `json:"text"`
	}{{Text: prompt}}

	jsonData, _ := json.Marshal(reqBody)
	resp, err := client.Post(agentURL+"/api/run", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to call agent", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "Agent returned error", "body", string(body))
		return
	}

	var events []AgentEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		slog.ErrorContext(ctx, "Failed to decode agent response", "error", err)
		return
	}

	// Find the model response
	var responseText string
	for _, e := range events {
		if e.Content.Role == "model" && len(e.Content.Parts) > 0 {
			responseText = e.Content.Parts[0].Text
		}
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
				lat, lng, err := GeocodeFacility(f.Name, stop.LocationName, stop.Latitude, stop.Longitude)
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
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), stopID)
	if err != nil {
		http.Error(w, "Briefing not found", http.StatusNotFound)
		return
	}

	// Check ownership via Stop -> Voyage
	stop, err := h.DB.GetStop(r.Context(), briefing.StopID)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusInternalServerError)
		return
	}
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusInternalServerError)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}

func (h *Handler) TriggerFullVoyageResearch(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	stops, err := h.DB.ListStops(r.Context(), voyageID, 0, 0)
	if err != nil {
		http.Error(w, "Failed to list stops", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"msg":        "Full research started",
		"voyage_id":  voyageID,
		"stop_count": len(stops),
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
			h.performGuideResearch(voyage)
		}()

		// 2. Research each stop (Parallel)
		for _, stop := range stops {
			wg.Add(1)
			h.ResearchSem <- struct{}{} // Block until a slot is available
			go func(s models.Stop) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }() // Release slot
				slog.InfoContext(ctx, fmt.Sprintf("Starting stop research for stop %d", s.ID))
				h.performStopResearchLogic(&s)
			}(stop)
		}

		wg.Wait()
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
