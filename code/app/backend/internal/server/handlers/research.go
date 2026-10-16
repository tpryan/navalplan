package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	PilotNotes     json.RawMessage `json:"pilot_notes"`
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

// facilityRingsNM is the escalating "search area" ladder in nautical miles.
// Facilities display within the tightest ring holding at least
// minRingFacilities of them, so the walkable cluster around the stop wins over
// a denser one farther away. Starts at 1.0 NM (~1852 m) so a stop anchored or
// moored in a harbor (e.g. Annapolis) keeps the full walkable waterfront and
// downtown cluster rather than clipping at 0.5 NM. The radius never widens past
// the top rung (20 nm) or the stop's configured radius, whichever is smaller.
var facilityRingsNM = []float64{1, 2.5, 5, 10, 20}

// minRingFacilities is how many facilities a ring needs before it is used, so
// a single nearby place does not hide everything else.
const minRingFacilities = 8

// nearbyFacilities keeps only the facilities within the tightest non-empty ring
// of facilityRingsNM around (lat,lng), capped at maxNM. Facilities without
// coordinates are always retained (they cannot be distance-tested). The result
// is never nil.
func nearbyFacilities(facs []Facility, lat, lng, maxNM float64) []Facility {
	coordless := []Facility{}
	withCoords := []Facility{}
	for _, f := range facs {
		if f.Latitude == 0 && f.Longitude == 0 {
			coordless = append(coordless, f)
		} else {
			withCoords = append(withCoords, f)
		}
	}

	rings := []float64{}
	for _, r := range facilityRingsNM {
		if r <= maxNM {
			rings = append(rings, r)
		}
	}
	if len(rings) == 0 {
		rings = []float64{maxNM}
	}

	var widest []Facility
	for _, ring := range rings {
		kept := []Facility{}
		for _, f := range withCoords {
			if haversine(lat, lng, f.Latitude, f.Longitude, "nm") <= ring {
				kept = append(kept, f)
			}
		}
		if len(kept) >= minRingFacilities {
			return append(kept, coordless...)
		}
		widest = kept
	}
	// No ring reached the minimum: show everything within the widest ring.
	return append(widest, coordless...)
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

func hasReusableFacilities(raw model.RawJSON) bool {
	if len(raw) == 0 {
		return false
	}
	var facs []Facility
	if err := json.Unmarshal(raw, &facs); err != nil {
		return false
	}
	return len(facs) >= minRingFacilities
}

// nearbyStopRadiusNM is the distance threshold (in nautical miles) within which
// two stops share a single search.
const nearbyStopRadiusNM = 2.0

// stopsWithin2NM reports whether two stops have valid coordinates within 2.0 NM of each other.
func stopsWithin2NM(a, b model.Stop) bool {
	if (a.Latitude == 0 && a.Longitude == 0) || (b.Latitude == 0 && b.Longitude == 0) {
		return false
	}
	return haversine(a.Latitude, a.Longitude, b.Latitude, b.Longitude, "nm") <= nearbyStopRadiusNM
}

func sameCalendarDay(a, b time.Time) bool {
	if a.IsZero() && b.IsZero() {
		return true
	}
	if a.IsZero() || b.IsZero() {
		return false
	}
	return a.UTC().Format("2006-01-02") == b.UTC().Format("2006-01-02")
}

func (h *Handler) performStopResearchLogic(stop *model.Stop, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[researcher-agent] Starting research for stop %d", stop.ID))
	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Starting research for %s", stop.LocationName))

	isPassage := stop.StopType == model.StopTypePassagePoint

	var earlierNearbyBriefing *model.Briefing
	stops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
	if err == nil && len(stops) > 1 && !isPassage {
		// Check if any earlier stop in the voyage is within 2 NM (e.g. Stop 1 and
		// Stop 2 in the same harbor, or the final return stop returning to the
		// departure harbor). If on the same date, clone the full briefing; if on a
		// different date, reuse its facilities while still fetching date-specific
		// weather, tides, and sun phase.
		for _, earlier := range stops {
			if earlier.ID == stop.ID {
				break
			}
			if earlier.StopType == model.StopTypePassagePoint {
				continue
			}
			if stopsWithin2NM(earlier, *stop) {
				earlierBriefing, err := h.DB.GetBriefing(ctx, earlier.ID)
				if err == nil && earlierBriefing != nil && hasValidFacilities(earlierBriefing.Facilities) {
					if sameCalendarDay(earlier.TargetDate, stop.TargetDate) {
						slog.InfoContext(ctx, "Stop within 2 NM on same date detected, cloning briefing", "stop_id", stop.ID, "earlier_stop_id", earlier.ID)
						h.broadcastProgress(sessionID, "clone", fmt.Sprintf("Cloning research from earlier stop for %s", stop.LocationName))
						newBriefing := &model.Briefing{
							StopID:         stop.ID,
							WeatherSummary: earlierBriefing.WeatherSummary,
							SunPhase:       earlierBriefing.SunPhase,
							Tides:          earlierBriefing.Tides,
							Facilities:     earlierBriefing.Facilities,
							PilotNotes:     earlierBriefing.PilotNotes,
						}
						if err := h.DB.CreateBriefing(ctx, newBriefing); err == nil {
							allStops := make([]model.Stop, len(stops))
							copy(allStops, stops)
							sort.Slice(allStops, func(i, j int) bool { return allStops[i].TargetDate.Before(allStops[j].TargetDate) })
							h.performLookoutAuditLogic(stop, newBriefing, allStops, sessionID)
							h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete (reused) for %s", stop.LocationName))
							return
						}
					} else if earlierNearbyBriefing == nil {
						earlierNearbyBriefing = earlierBriefing
					}
				}
			}
		}
	}

	const appName = "harbourmaster"
	const userID = "system"
	agentSessionID := fmt.Sprintf("stop_%d_%d", stop.ID, time.Now().Unix())

	var nearbyBriefing *model.Briefing
	var nearbyErr error
	var reusableFacilities json.RawMessage
	if !isPassage {
		if earlierNearbyBriefing != nil {
			nearbyBriefing = earlierNearbyBriefing
		} else {
			nearbyBriefing, nearbyErr = h.DB.GetNearbyBriefing(ctx, stop.Latitude, stop.Longitude)
		}
	}

	// Stop coordinates in session state let the researcher's place search
	// anchor on the stop instead of trusting coordinates relayed by the LLM
	// (which tends to substitute the named city's centre).
	sessionState := map[string]any{
		"stop_latitude":  stop.Latitude,
		"stop_longitude": stop.Longitude,
	}
	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, sessionState); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyBriefing(ctx, stop)
		return
	}

	h.broadcastProgress(sessionID, "agent", fmt.Sprintf("Consulting the Harbourmaster for %s", stop.LocationName))

	var prompt string
	latDir := "N"
	absLat := stop.Latitude
	if absLat < 0 {
		latDir = "S"
		absLat = -absLat
	}
	lngDir := "E"
	absLng := stop.Longitude
	if absLng < 0 {
		lngDir = "W"
		absLng = -absLng
	}
	coordStr := fmt.Sprintf("%.4f %s, %.4f %s (Lat: %f, Lng: %f)", absLat, latDir, absLng, lngDir, stop.Latitude, stop.Longitude)

	if isPassage {
		prompt = fmt.Sprintf("This is an open-ocean passage position with no landfall. Do NOT research anchorages, marinas, moorings, or any shore facilities. "+
			"Report the underway conditions for %s on %s: weather (wind speed and direction, wave height, swell period), "+
			"tides and tidal currents, sun phase (sunrise/sunset), and any safety or navigational alerts. "+
			"Populate weather_summary, tides, and sun_phase, and return an empty array for facilities.",
			coordStr, stop.TargetDate.Format("January 2, 2006"))
	} else {
		locInfo := fmt.Sprintf("%s (%s)", stop.LocationName, coordStr)

		prompt = fmt.Sprintf("Research anchorages and weather for %s for %s. Radius %d %s.",
			locInfo, stop.TargetDate.Format("January 2, 2006"), stop.SearchRadius, stop.SearchRadiusUnit)

		if earlierNearbyBriefing != nil && hasValidFacilities(earlierNearbyBriefing.Facilities) {
			slog.InfoContext(ctx, "Reusing facilities from earlier voyage stop within 2 NM", "stop_id", stop.ID, "earlier_briefing_id", earlierNearbyBriefing.ID)
			reusableFacilities = json.RawMessage(earlierNearbyBriefing.Facilities)
			prompt += " Do not research facilities; I will provide those separately."
		} else if nearbyErr == nil && nearbyBriefing != nil && nearbyBriefing.StopID != stop.ID && hasReusableFacilities(nearbyBriefing.Facilities) {
			slog.InfoContext(ctx, fmt.Sprintf("Found nearby existing briefing %d, reusing facilities", nearbyBriefing.ID))
			reusableFacilities = json.RawMessage(nearbyBriefing.Facilities)
			prompt += " Do not research facilities; I will provide those separately."
		}
	}

	progressStop := make(chan struct{})
	go func() {
		t := time.NewTicker(8 * time.Second)
		defer t.Stop()
		messages := []string{
			fmt.Sprintf("Fetching weather & marine forecast for %s", stop.LocationName),
			fmt.Sprintf("Checking NOAA tide tables & currents for %s", stop.LocationName),
			fmt.Sprintf("Discovering anchorages, marinas, and shore facilities for %s", stop.LocationName),
			fmt.Sprintf("Compiling pilot notes & navigational briefing for %s", stop.LocationName),
		}
		msgIdx := 0
		for {
			select {
			case <-progressStop:
				return
			case <-t.C:
				msg := messages[msgIdx%len(messages)]
				msgIdx++
				h.broadcastProgress(sessionID, "agent", msg)
			}
		}
	}()
	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	close(progressStop)
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

	h.broadcastProgress(sessionID, "parsing", fmt.Sprintf("Mapping facilities & saving briefing for %s", stop.LocationName))
	responseText = cleanJSON(responseText)

	var output AgentOutput
	if err := json.Unmarshal([]byte(responseText), &output); err != nil {
		if !strings.HasSuffix(strings.TrimSpace(responseText), "}") {
			slog.ErrorContext(ctx, "Agent JSON output is incomplete; likely truncated at the model output token limit",
				"stop_id", stop.ID, "location", stop.LocationName, "response_chars", len(responseText))
		}
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

		facilities = nearbyFacilities(facilities, stop.Latitude, stop.Longitude, radiusToNM(stop.SearchRadius, stop.SearchRadiusUnit))

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
		PilotNotes:     model.RawJSON(output.PilotNotes),
	}

	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save briefing", "error", err)
	} else {
		allStops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
		if err == nil && len(allStops) > 0 {
			sort.Slice(allStops, func(i, j int) bool { return allStops[i].TargetDate.Before(allStops[j].TargetDate) })
			h.performLookoutAuditLogic(stop, briefing, allStops, sessionID)
		}
	}
	slog.InfoContext(ctx, "Briefing saved for stop", "stop_id", stop.ID)
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

	// Clip facilities to the stop's search area on the way out, so existing
	// briefings honor the current radius without needing to be re-researched.
	if len(briefing.Facilities) > 0 {
		var facs []Facility
		if err := json.Unmarshal(briefing.Facilities, &facs); err == nil {
			facs = nearbyFacilities(facs, stop.Latitude, stop.Longitude, radiusToNM(stop.SearchRadius, stop.SearchRadiusUnit))
			if b, err := json.Marshal(facs); err == nil {
				briefing.Facilities = model.RawJSON(b)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}

func (h *Handler) TriggerFullVoyageResearch(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

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

		// Check proximity BEFORE making any agent/Places calls:
		// Any stop within 2 NM of an earlier non-passage stop in the voyage
		// (e.g. Stop 1 and Stop 2 in the same harbor, or the final return stop)
		// is held back until the primary stops complete, then clones the primary
		// stop's briefing so only 1 search is performed.
		type redundantStopPair struct {
			stop          model.Stop
			primaryStopID int64
		}
		var primaryStops []model.Stop
		var redundantStops []redundantStopPair
		for _, stop := range stops {
			var matchedPrimaryID int64
			if stop.StopType != model.StopTypePassagePoint {
				for _, p := range primaryStops {
					if p.StopType != model.StopTypePassagePoint && stopsWithin2NM(p, stop) {
						matchedPrimaryID = p.ID
						break
					}
				}
			}
			if matchedPrimaryID != 0 {
				redundantStops = append(redundantStops, redundantStopPair{stop: stop, primaryStopID: matchedPrimaryID})
			} else {
				primaryStops = append(primaryStops, stop)
			}
		}

		for _, stop := range primaryStops {
			wg.Add(1)
			h.ResearchSem <- struct{}{}
			go func(s model.Stop) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }()
				slog.InfoContext(ctx, "Starting stop research for stop", "stop_id", s.ID)
				h.performStopResearchLogic(&s, sessionID)
			}(stop)
		}

		wg.Wait()

		for _, pair := range redundantStops {
			wg.Add(1)
			h.ResearchSem <- struct{}{}
			go func(rStop model.Stop, primaryStopID int64) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }()
				slog.InfoContext(ctx, "Running nearby stop research after primary stop completed", "voyage_id", voyageID, "stop_id", rStop.ID, "primary_stop_id", primaryStopID)
				h.performStopResearchLogic(&rStop, sessionID)
			}(pair.stop, pair.primaryStopID)
		}

		wg.Wait()

		slog.InfoContext(ctx, "Full research complete for voyage", "voyage_id", voyageID)
	}()
}

func (h *Handler) saveEmptyBriefing(ctx context.Context, stop *model.Stop) {
	briefing := &model.Briefing{
		StopID:         stop.ID,
		WeatherSummary: model.RawJSON([]byte(`{"summary":"Error: Agent failed to respond"}`)),
		SunPhase:       model.RawJSON([]byte(`{}`)),
		Tides:          model.RawJSON([]byte(`{}`)),
		Facilities:     model.RawJSON([]byte(`[]`)),
		PilotNotes:     model.RawJSON([]byte(`{}`)),
	}
	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save empty briefing", "stop_id", stop.ID, "error", err)
	}
}
