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

	"app/internal/agent"
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

var facilityRingsNM = []float64{1, 2.5, 5, 10, 20}

const minRingFacilities = 8

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
	dayID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Day ID")
		return
	}

	day, err := h.DB.GetDay(r.Context(), dayID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Day not found")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), day.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	jobKey := fmt.Sprintf("day:%d", day.ID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Research is already in progress for this day")
		return
	}

	sessionID := fmt.Sprintf("day_%d_%d", day.ID, time.Now().Unix())

	h.ensureProgressChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "day_id": idStr, "stop_id": idStr, "session_id": sessionID})

	go h.performDayResearch(day, sessionID, jobKey)
}

func (h *Handler) performDayResearch(day *model.VoyageDay, sessionID, jobKey string) {
	defer h.releaseJob(jobKey)
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performDayResearchLogic(day, sessionID)
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

const nearbyDayRadiusNM = 2.0

func daysWithin2NM(a, b model.VoyageDay) bool {
	if (a.EndLatitude == 0 && a.EndLongitude == 0) || (b.EndLatitude == 0 && b.EndLongitude == 0) {
		return false
	}
	return haversine(a.EndLatitude, a.EndLongitude, b.EndLatitude, b.EndLongitude, "nm") <= nearbyDayRadiusNM
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

func (h *Handler) performDayResearchLogic(day *model.VoyageDay, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	destName := day.PrimaryLocationName()
	destLat := day.EndLatitude
	destLng := day.EndLongitude
	if destLat == 0 && destLng == 0 {
		destLat = day.StartLatitude
		destLng = day.StartLongitude
	}

	slog.InfoContext(ctx, fmt.Sprintf("[researcher-agent] Starting research for day %d", day.ID))
	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Starting research for %s", destName))

	isPassage := day.DayType == model.DayTypePassagePoint

	var earlierNearbyBriefing *model.Briefing
	days, err := h.DB.ListDays(ctx, day.VoyageID, 0, 0)
	allDays := make([]model.VoyageDay, len(days))
	copy(allDays, days)
	sort.Slice(allDays, func(i, j int) bool {
		return allDays[i].TargetDate.Before(allDays[j].TargetDate)
	})
	dayPosition := 1
	for i, d := range allDays {
		if d.ID == day.ID {
			dayPosition = i + 1
			break
		}
	}

	var distNM, course float64
	var hasCourse bool
	if (day.StartLatitude != 0 || day.StartLongitude != 0) && (day.EndLatitude != 0 || day.EndLongitude != 0) {
		distNM = lookoutHaversineNM(day.StartLatitude, day.StartLongitude, day.EndLatitude, day.EndLongitude)
		if distNM > 0.1 {
			course = calculateBearing(day.StartLatitude, day.StartLongitude, day.EndLatitude, day.EndLongitude)
			hasCourse = true
		}
	}
	matchedPlannedTrack := h.findPlannedTrackForDay(ctx, day, dayPosition)
	var plannedRouteInfo string
	if matchedPlannedTrack != nil {
		plannedRouteInfo, distNM = formatPlannedRouteForLookout(matchedPlannedTrack, distNM)
	}

	if err == nil && len(days) > 1 && !isPassage {
		for _, earlier := range days {
			if earlier.ID == day.ID {
				break
			}
			if earlier.DayType == model.DayTypePassagePoint {
				continue
			}
			if daysWithin2NM(earlier, *day) {
				earlierBriefing, err := h.DB.GetBriefing(ctx, earlier.ID)
				if err == nil && earlierBriefing != nil && hasValidFacilities(earlierBriefing.Facilities) {
					if sameCalendarDay(earlier.TargetDate, day.TargetDate) && matchedPlannedTrack == nil {
						slog.InfoContext(ctx, "Day within 2 NM on same date detected, cloning briefing", "day_id", day.ID, "earlier_day_id", earlier.ID)
						h.broadcastProgress(sessionID, "clone", fmt.Sprintf("Cloning research from earlier day for %s", destName))
						newBriefing := &model.Briefing{
							DayID:          day.ID,
							WeatherSummary: earlierBriefing.WeatherSummary,
							SunPhase:       earlierBriefing.SunPhase,
							Tides:          earlierBriefing.Tides,
							Facilities:     earlierBriefing.Facilities,
							PilotNotes:     earlierBriefing.PilotNotes,
						}
						if err := h.DB.CreateBriefing(ctx, newBriefing); err == nil {
							h.performLookoutAuditLogic(day, newBriefing, allDays, sessionID)
							h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete (reused) for %s", destName))
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
	agentSessionID := fmt.Sprintf("day_%d_%d", day.ID, time.Now().Unix())

	var nearbyBriefing *model.Briefing
	var nearbyErr error
	var reusableFacilities json.RawMessage
	if !isPassage {
		if earlierNearbyBriefing != nil {
			nearbyBriefing = earlierNearbyBriefing
		} else {
			nearbyBriefing, nearbyErr = h.DB.GetNearbyBriefing(ctx, destLat, destLng)
		}
	}

	sessionState := map[string]any{
		"stop_latitude":     destLat,
		"stop_longitude":    destLng,
		"day_end_latitude":  destLat,
		"day_end_longitude": destLng,
	}
	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, sessionState); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyBriefing(ctx, day)
		return
	}

	h.broadcastProgress(sessionID, "agent", fmt.Sprintf("Consulting the Harbourmaster for %s", destName))

	var prompt string
	latDir := "N"
	absLat := destLat
	if absLat < 0 {
		latDir = "S"
		absLat = -absLat
	}
	lngDir := "E"
	absLng := destLng
	if absLng < 0 {
		lngDir = "W"
		absLng = -absLng
	}
	coordStr := fmt.Sprintf("%.4f %s, %.4f %s (Lat: %f, Lng: %f)", absLat, latDir, absLng, lngDir, destLat, destLng)

	if isPassage {
		prompt = fmt.Sprintf("This is an open-ocean passage position with no landfall. Do NOT research anchorages, marinas, moorings, or any shore facilities. "+
			"Report the underway conditions for %s on %s: weather (wind speed and direction, wave height, swell period), "+
			"tides and tidal currents, sun phase (sunrise/sunset), and any safety or navigational alerts. "+
			"Populate weather_summary, tides, and sun_phase, and return an empty array for facilities.",
			coordStr, day.TargetDate.Format("January 2, 2006"))
	} else {
		locInfo := fmt.Sprintf("%s (%s)", destName, coordStr)
		prompt = fmt.Sprintf("Research anchorages and weather for %s for %s. Radius %d %s.",
			locInfo, day.TargetDate.Format("January 2, 2006"), day.SearchRadius, day.SearchRadiusUnit)
		if earlierNearbyBriefing != nil && hasValidFacilities(earlierNearbyBriefing.Facilities) {
			slog.InfoContext(ctx, "Reusing facilities from earlier voyage day within 2 NM", "day_id", day.ID, "earlier_briefing_id", earlierNearbyBriefing.ID)
			reusableFacilities = json.RawMessage(earlierNearbyBriefing.Facilities)
			prompt += " Do not research facilities; I will provide those separately."
		} else if nearbyErr == nil && nearbyBriefing != nil && nearbyBriefing.DayID != day.ID && hasReusableFacilities(nearbyBriefing.Facilities) {
			slog.InfoContext(ctx, fmt.Sprintf("Found nearby existing briefing %d, reusing facilities", nearbyBriefing.ID))
			reusableFacilities = json.RawMessage(nearbyBriefing.Facilities)
			prompt += " Do not research facilities; I will provide those separately."
		}
	}
	if transitAndTrackPrompt := buildDayTransitAndTrackPrompt(day, distNM, course, hasCourse, plannedRouteInfo); transitAndTrackPrompt != "" {
		prompt += "\n\n" + transitAndTrackPrompt
	}

	var stateWeather, stateTides, stateSun, statePlaces json.RawMessage
	var stateLocalNotices, stateHydroCitations []string
	onEvent := func(evt agent.AgentEvent) {
		for _, p := range evt.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name != "" {
				h.broadcastProgress(sessionID, "tool", toolProgressMessage(p.FunctionCall.Name, destName))
			}
			if p.FunctionResponse != nil && p.FunctionResponse.Name == "hydrographic_pilot" {
				notices, cites := extractHydrographicPilotNotices(p.FunctionResponse.Response)
				if len(notices) > 0 {
					stateLocalNotices = append(stateLocalNotices, notices...)
				}
				if len(cites) > 0 {
					stateHydroCitations = append(stateHydroCitations, cites...)
				}
			}
		}
		if len(evt.Actions.StateDelta) > 0 {
			if wRaw, ok := evt.Actions.StateDelta["briefing_weather"]; ok && len(wRaw) > 0 {
				stateWeather = normalizeStateWeather(wRaw)
				h.broadcastProgressSection(sessionID, "weather", fmt.Sprintf("Weather forecast ready for %s", destName), stateWeather)
			}
			if tRaw, ok := evt.Actions.StateDelta["briefing_tides"]; ok && len(tRaw) > 0 {
				stateTides = normalizeStateTides(tRaw)
				h.broadcastProgressSection(sessionID, "tides", fmt.Sprintf("Tide chart ready for %s", destName), stateTides)
			}
			if sRaw, ok := evt.Actions.StateDelta["briefing_sun"]; ok && len(sRaw) > 0 {
				stateSun = normalizeStateSun(sRaw)
				h.broadcastProgressSection(sessionID, "sun", fmt.Sprintf("Daylight window ready for %s", destName), stateSun)
			}
			if pRaw, ok := evt.Actions.StateDelta["verified_places"]; ok && len(pRaw) > 0 {
				statePlaces = normalizeStatePlaces(pRaw)
				h.broadcastProgressSection(sessionID, "facilities", fmt.Sprintf("Verified facilities ready for %s", destName), statePlaces)
			}
		}
	}

	progressStop := make(chan struct{})
	go func() {
		t := time.NewTicker(8 * time.Second)
		defer t.Stop()
		messages := []string{
			fmt.Sprintf("Fetching weather & marine forecast for %s", destName),
			fmt.Sprintf("Checking NOAA tide tables & currents for %s", destName),
			fmt.Sprintf("Discovering anchorages, marinas, and shore facilities for %s", destName),
			fmt.Sprintf("Compiling pilot notes & navigational briefing for %s", destName),
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
	responseText, err := h.Agent.RunStreamingWithEvents(ctx, appName, userID, agentSessionID, prompt, onEvent)
	close(progressStop)
	hasStateFallback := len(stateWeather) > 0 || len(stateTides) > 0 || len(stateSun) > 0 || len(statePlaces) > 0 || len(reusableFacilities) > 0
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "high demand") {
			h.broadcastProgress(sessionID, "error_503", "Model is busy due to high demand. Please try again in a few minutes.")
		}
		if !hasStateFallback {
			h.saveEmptyBriefing(ctx, day)
			return
		}
		slog.WarnContext(ctx, "Salvaging day briefing from session state after agent error", "day_id", day.ID)
	}

	var output AgentOutput
	if responseText == "" {
		if !hasStateFallback {
			slog.ErrorContext(ctx, "No response from agent")
			h.saveEmptyBriefing(ctx, day)
			return
		}
		slog.WarnContext(ctx, "No text response from agent; salvaging day briefing from session state", "day_id", day.ID)
	} else {
		h.broadcastProgress(sessionID, "parsing", fmt.Sprintf("Mapping facilities & saving briefing for %s", destName))
		cleaned := repairMathInJSON(cleanJSON(responseText))
		if unmarshalErr := json.Unmarshal([]byte(cleaned), &output); unmarshalErr != nil {
			if lastObj := extractLastJSONObject(responseText); lastObj != "" {
				unmarshalErr = json.Unmarshal([]byte(repairMathInJSON(lastObj)), &output)
			}
			if unmarshalErr != nil {
				if !strings.HasSuffix(strings.TrimSpace(cleaned), "}") {
					slog.ErrorContext(ctx, "Agent JSON output is incomplete; likely truncated at the model output token limit",
						"day_id", day.ID, "location", destName, "response_chars", len(responseText))
				}
				slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", unmarshalErr, "raw", cleaned)
				if !hasStateFallback {
					h.saveEmptyBriefing(ctx, day)
					return
				}
				slog.WarnContext(ctx, "Salvaging day briefing from session state after JSON parse error", "day_id", day.ID)
			}
		}
	}

	if isEmptyOrNullJSON(output.WeatherSummary) && len(stateWeather) > 0 {
		output.WeatherSummary = stateWeather
	}
	if isEmptyOrNullJSON(output.Tides) && len(stateTides) > 0 {
		output.Tides = stateTides
	}
	if isEmptyOrNullJSON(output.SunPhase) && len(stateSun) > 0 {
		output.SunPhase = stateSun
	}
	if isEmptyOrNullJSON(output.Facilities) && len(statePlaces) > 0 {
		output.Facilities = statePlaces
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
				slog.InfoContext(ctx, fmt.Sprintf("Geocoding facility missing coordinates: %s near %s", f.Name, destName))
				lat, lng, err := geocodeFacility(ctx, f.Name, destName, destLat, destLng)
				if err == nil {
					facilities[i].Latitude = lat
					facilities[i].Longitude = lng
				} else {
					slog.WarnContext(ctx, fmt.Sprintf("Failed to geocode facility %s", f.Name), "error", err)
				}
			}(i)
		}
		wg.Wait()

		facilities = nearbyFacilities(facilities, destLat, destLng, radiusToNM(day.SearchRadius, day.SearchRadiusUnit))

		sort.Slice(facilities, func(i, j int) bool {
			if facilities[i].Type != facilities[j].Type {
				return facilities[i].Type < facilities[j].Type
			}
			return facilities[i].Name < facilities[j].Name
		})

		newBytes, _ := json.Marshal(facilities)
		output.Facilities = json.RawMessage(newBytes)
	}

	if len(stateLocalNotices) > 0 || len(stateHydroCitations) > 0 {
		output.PilotNotes = mergePilotNotesLocalNotices(output.PilotNotes, stateLocalNotices, stateHydroCitations)
	}

	briefing := &model.Briefing{
		DayID:          day.ID,
		WeatherSummary: model.RawJSON(output.WeatherSummary),
		SunPhase:       model.RawJSON(output.SunPhase),
		Tides:          model.RawJSON(output.Tides),
		Facilities:     model.RawJSON(output.Facilities),
		PilotNotes:     model.RawJSON(output.PilotNotes),
	}

	h.broadcastProgressSection(sessionID, "weather", fmt.Sprintf("Weather forecast saved for %s", destName), output.WeatherSummary)
	h.broadcastProgressSection(sessionID, "tides", fmt.Sprintf("Tide predictions saved for %s", destName), output.Tides)
	h.broadcastProgressSection(sessionID, "sun", fmt.Sprintf("Sun phase saved for %s", destName), output.SunPhase)
	h.broadcastProgressSection(sessionID, "facilities", fmt.Sprintf("Facilities mapped for %s", destName), output.Facilities)
	h.broadcastProgressSection(sessionID, "pilot_notes", fmt.Sprintf("Pilot notes compiled for %s", destName), output.PilotNotes)

	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save briefing", "error", err)
	} else {
		allDays, err := h.DB.ListDays(ctx, day.VoyageID, 0, 0)
		if err == nil && len(allDays) > 0 {
			sort.Slice(allDays, func(i, j int) bool { return allDays[i].TargetDate.Before(allDays[j].TargetDate) })
			h.performLookoutAuditLogic(day, briefing, allDays, sessionID)
		}
	}
	slog.InfoContext(ctx, "Briefing saved for day", "day_id", day.ID)
	h.broadcastProgress(sessionID, "done", fmt.Sprintf("Research complete for %s", destName))
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	dayID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Day ID")
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), dayID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Briefing not found")
		return
	}

	day, err := h.DB.GetDay(r.Context(), briefing.DayID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Day not found")
		return
	}
	voyage, err := h.DB.GetVoyage(r.Context(), day.VoyageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	destLat := day.EndLatitude
	destLng := day.EndLongitude
	if destLat == 0 && destLng == 0 {
		destLat = day.StartLatitude
		destLng = day.StartLongitude
	}

	if len(briefing.Facilities) > 0 {
		var facs []Facility
		if err := json.Unmarshal(briefing.Facilities, &facs); err == nil {
			facs = nearbyFacilities(facs, destLat, destLng, radiusToNM(day.SearchRadius, day.SearchRadiusUnit))
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

	days, err := h.DB.ListDays(r.Context(), voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list days")
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
		"day_count":  len(days),
		"stop_count": len(days),
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

		type redundantDayPair struct {
			day          model.VoyageDay
			primaryDayID int64
		}
		var primaryDays []model.VoyageDay
		var redundantDays []redundantDayPair
		for _, day := range days {
			var matchedPrimaryID int64
			if day.DayType != model.DayTypePassagePoint {
				for _, p := range primaryDays {
					if p.DayType != model.DayTypePassagePoint && daysWithin2NM(p, day) {
						matchedPrimaryID = p.ID
						break
					}
				}
			}
			if matchedPrimaryID != 0 {
				redundantDays = append(redundantDays, redundantDayPair{day: day, primaryDayID: matchedPrimaryID})
			} else {
				primaryDays = append(primaryDays, day)
			}
		}

		for _, day := range primaryDays {
			wg.Add(1)
			h.ResearchSem <- struct{}{}
			go func(d model.VoyageDay) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }()
				slog.InfoContext(ctx, "Starting day research for day", "day_id", d.ID)
				h.performDayResearchLogic(&d, sessionID)
			}(day)
		}

		wg.Wait()

		for _, pair := range redundantDays {
			wg.Add(1)
			h.ResearchSem <- struct{}{}
			go func(rDay model.VoyageDay, primaryDayID int64) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }()
				slog.InfoContext(ctx, "Running nearby day research after primary day completed", "voyage_id", voyageID, "day_id", rDay.ID, "primary_day_id", primaryDayID)
				h.performDayResearchLogic(&rDay, sessionID)
			}(pair.day, pair.primaryDayID)
		}

		wg.Wait()

		slog.InfoContext(ctx, "Full research complete for voyage", "voyage_id", voyageID)
	}()
}

func (h *Handler) saveEmptyBriefing(ctx context.Context, day *model.VoyageDay) {
	briefing := &model.Briefing{
		DayID:          day.ID,
		WeatherSummary: model.RawJSON([]byte(`{"summary":"Error: Agent failed to respond"}`)),
		SunPhase:       model.RawJSON([]byte(`{}`)),
		Tides:          model.RawJSON([]byte(`{}`)),
		Facilities:     model.RawJSON([]byte(`[]`)),
		PilotNotes:     model.RawJSON([]byte(`{}`)),
	}
	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		slog.ErrorContext(ctx, "Failed to save empty briefing", "day_id", day.ID, "error", err)
	}
}

func isEmptyOrNullJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}" || s == "[]"
}

func extractLastJSONObject(s string) string {
	end := strings.LastIndex(s, "}")
	if end < 0 {
		return ""
	}
	for start := strings.LastIndex(s[:end], "{"); start >= 0; start = strings.LastIndex(s[:start], "{") {
		candidate := s[start : end+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
		if start == 0 {
			break
		}
	}
	return ""
}

func normalizeStateWeather(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["wind_speed_kt"]; !ok {
		if v, ok := m["max_wind_kts"]; ok {
			m["wind_speed_kt"] = v
		}
	}
	if _, ok := m["temp_min_f"]; !ok {
		if v, ok := m["min_temp"]; ok {
			m["temp_min_f"] = v
		}
	}
	if _, ok := m["temp_max_f"]; !ok {
		if v, ok := m["max_temp"]; ok {
			m["temp_max_f"] = v
		}
	}
	if _, ok := m["wave_height_ft"]; !ok {
		if v, ok := m["wave_height"]; ok {
			m["wave_height_ft"] = v
		}
	}
	if sum, _ := m["summary"].(string); sum == "" {
		if dig, _ := m["digest"].(string); dig != "" {
			m["summary"] = dig
		} else if cond, _ := m["condition"].(string); cond != "" {
			wind, _ := m["wind_speed_kt"].(float64)
			m["summary"] = fmt.Sprintf("%s with max winds of %.1f kts", cond, wind)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

func normalizeStateTides(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["events"]; !ok {
		if tides, ok := m["tides"]; ok {
			m["events"] = tides
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

func normalizeStateSun(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	formatISO := func(s string) string {
		if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
			return t.Format("03:04 PM")
		}
		return s
	}
	if sr, ok := m["sunrise"].(string); ok && sr != "" {
		m["sunrise"] = formatISO(sr)
	}
	if ss, ok := m["sunset"].(string); ok && ss != "" {
		m["sunset"] = formatISO(ss)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

func normalizeStatePlaces(raw json.RawMessage) json.RawMessage {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}
	for _, item := range items {
		if _, ok := item["vicinity"]; !ok {
			if addr, ok := item["address"].(string); ok && addr != "" {
				item["vicinity"] = addr
			}
		}
		if _, ok := item["references"]; !ok {
			if uri, ok := item["website_uri"].(string); ok && uri != "" {
				item["references"] = []string{uri}
			} else if w, ok := item["website"].(string); ok && w != "" {
				item["references"] = []string{w}
			}
		}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return b
}

func buildDayTransitAndTrackPrompt(day *model.VoyageDay, distNM, course float64, hasCourse bool, plannedRouteInfo string) string {
	var b strings.Builder
	if day != nil && hasCourse && distNM > 0.1 {
		dateStr := ""
		if !day.TargetDate.IsZero() {
			dateStr = fmt.Sprintf(" on %s", day.TargetDate.Format("January 2, 2006"))
		}
		startLoc := day.StartLocationName
		if startLoc == "" {
			startLoc = fmt.Sprintf("%.4f, %.4f", day.StartLatitude, day.StartLongitude)
		}
		endLoc := day.EndLocationName
		if endLoc == "" {
			endLoc = fmt.Sprintf("%.4f, %.4f", day.EndLatitude, day.EndLongitude)
		}
		fmt.Fprintf(&b, "Day passage from %s (%.4f, %.4f) to destination %s (%.4f, %.4f)%s.",
			startLoc, day.StartLatitude, day.StartLongitude, endLoc, day.EndLatitude, day.EndLongitude, dateStr)
		fmt.Fprintf(&b, " Straight-line transit distance: %.1f NM on course %.0f° (%s).", distNM, course, bearingToCardinal(course))
		b.WriteString(" Call ComputePassageTimeline (passing both departure and arrival coordinates) and hydrographic_pilot for BOTH the departure harbor and the destination to evaluate departure tide gates, arrival daylight margins, and night-entry hazards.")
	}
	if plannedRouteInfo != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Planned Track for this leg:\n%s\n", strings.TrimSpace(plannedRouteInfo))
		b.WriteString("Compare the course, segment headings, waypoints, hazards, wind/tides/currents, and pilotage guidance against this planned track (rather than only a straight-line course between start and end), and pass the planned route details to hydrographic_pilot.")
		if strings.Contains(plannedRouteInfo, "Candidate Planned Route") {
			b.WriteString(" Multiple candidate planned routes are present: instruct harbourmaster and hydrographic_pilot to evaluate all candidate routes against wind angles, wave exposure, tidal currents/gates, daylight margins, and hydrographic hazards, and populate pilot_notes.route_comparison with the comparison and recommended route.")
		}
	}
	return b.String()
}

// extractHydrographicPilotNotices parses the tool response payload from
// hydrographic_pilot (whether returned as a raw JSON string under "result" or
// as a decoded map) and returns its local_notices and citations.
func extractHydrographicPilotNotices(resp map[string]any) (notices []string, citations []string) {
	if len(resp) == 0 {
		return nil, nil
	}
	type hydroPayload struct {
		LocalNotices []string `json:"local_notices"`
		Citations    []string `json:"citations"`
	}
	if rawStr, ok := resp["result"].(string); ok && strings.TrimSpace(rawStr) != "" {
		cleaned := cleanJSON(rawStr)
		var hp hydroPayload
		if err := json.Unmarshal([]byte(cleaned), &hp); err == nil {
			return hp.LocalNotices, hp.Citations
		}
	}
	if rawBytes, err := json.Marshal(resp); err == nil {
		var hp hydroPayload
		if err := json.Unmarshal(rawBytes, &hp); err == nil && (len(hp.LocalNotices) > 0 || len(hp.Citations) > 0) {
			return hp.LocalNotices, hp.Citations
		}
	}
	return nil, nil
}

// mergePilotNotesLocalNotices deterministically merges local_notices and
// citations captured from hydrographic_pilot into output.PilotNotes so LNM
// entries are never dropped if the LLM omits the local_notices field.
func mergePilotNotesLocalNotices(pilotNotes json.RawMessage, notices []string, citations []string) json.RawMessage {
	cleanNotices := make([]string, 0, len(notices))
	seenN := make(map[string]bool)
	for _, n := range notices {
		trimmed := strings.TrimSpace(n)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if (strings.HasPrefix(lower, "none") || strings.HasPrefix(lower, "no active")) && len(trimmed) < 40 {
			continue
		}
		if !seenN[lower] {
			seenN[lower] = true
			cleanNotices = append(cleanNotices, trimmed)
		}
	}
	if len(cleanNotices) == 0 && len(citations) == 0 {
		return pilotNotes
	}

	var obj map[string]any
	if len(pilotNotes) > 0 {
		_ = json.Unmarshal(pilotNotes, &obj)
	}
	if obj == nil {
		obj = make(map[string]any)
	}

	if len(cleanNotices) > 0 {
		var merged []string
		seenMerged := make(map[string]bool)
		if existing, ok := obj["local_notices"].([]any); ok {
			for _, item := range existing {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					k := strings.ToLower(strings.TrimSpace(s))
					if !seenMerged[k] {
						seenMerged[k] = true
						merged = append(merged, strings.TrimSpace(s))
					}
				}
			}
		}
		for _, n := range cleanNotices {
			k := strings.ToLower(n)
			if !seenMerged[k] {
				seenMerged[k] = true
				merged = append(merged, n)
			}
		}
		obj["local_notices"] = merged
	}

	if len(citations) > 0 {
		var mergedSrc []string
		seenSrc := make(map[string]bool)
		if existing, ok := obj["sources"].([]any); ok {
			for _, item := range existing {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					k := strings.ToLower(strings.TrimSpace(s))
					if !seenSrc[k] {
						seenSrc[k] = true
						mergedSrc = append(mergedSrc, strings.TrimSpace(s))
					}
				}
			}
		}
		for _, c := range citations {
			trimmed := strings.TrimSpace(c)
			if trimmed == "" {
				continue
			}
			k := strings.ToLower(trimmed)
			if !seenSrc[k] {
				seenSrc[k] = true
				mergedSrc = append(mergedSrc, trimmed)
			}
		}
		if len(mergedSrc) > 0 {
			obj["sources"] = mergedSrc
		}
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return pilotNotes
	}
	return json.RawMessage(out)
}
