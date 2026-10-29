package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/internal/agent"
	"app/internal/model"
)

const (
	// recGeocodeSnapMinNM is the minimum discrepancy (NM) between an LLM-emitted
	// anchorage/mooring coordinate and Google Geocoding's location for that named
	// feature before we snap to the geocoded position (catches ~2-8 NM LLM memory drift
	// such as placing Bloody Point Anchorage on Poplar Island).
	recGeocodeSnapMinNM = 0.75
	// recGeocodeSnapMaxNM caps how far a geocode match may be from the LLM coordinate
	// so an unrelated distant namesake cannot hijack the pin.
	recGeocodeSnapMaxNM = 12.0
)

var genericMarineSuffixes = []string{
	" Anchorage", " Mooring Field", " Moorings", " Mooring",
	" Harbor Hub", " Boating Hub", " Central Hub", " Hub",
}

func cleanGeocodeFeatureName(name string) string {
	cleaned := strings.TrimSpace(name)
	lower := strings.ToLower(cleaned)
	for _, suffix := range genericMarineSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) && len(cleaned) > len(suffix)+2 {
			cleaned = strings.TrimSpace(cleaned[:len(cleaned)-len(suffix)])
			break
		}
	}
	return cleaned
}

func refineRecommendationCoordinates(ctx context.Context, recs []model.VoyageRecommendation, vicinity string, centerLat, centerLng, maxRadiusNM float64) {
	if len(recs) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range recs {
		recType := strings.ToLower(strings.TrimSpace(recs[i].Type))
		needsCheck := (recs[i].Latitude == 0 && recs[i].Longitude == 0) ||
			strings.Contains(recType, "anchor") ||
			strings.Contains(recType, "moor") ||
			recType == "hub"
		if !needsCheck {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := &recs[idx]
			queryName := cleanGeocodeFeatureName(r.Name)
			lat, lng, err := geocodeFacility(ctx, queryName, vicinity, centerLat, centerLng)
			if err != nil || (lat == 0 && lng == 0) {
				// If the LLM emitted (0,0) and full-name geocoding failed, retry with the
				// raw recommendation name before giving up.
				if r.Latitude == 0 && r.Longitude == 0 && queryName != r.Name {
					lat, lng, err = geocodeFacility(ctx, r.Name, vicinity, centerLat, centerLng)
				}
				if err != nil || (lat == 0 && lng == 0) {
					return
				}
			}
			if haversine(centerLat, centerLng, lat, lng, "nm") > maxRadiusNM {
				return
			}
			if r.Latitude == 0 && r.Longitude == 0 {
				slog.InfoContext(ctx, "Geocoded recommendation missing coordinates",
					"name", r.Name, "query", queryName, "lat", lat, "lng", lng)
				r.Latitude, r.Longitude = lat, lng
				return
			}
			driftNM := haversine(r.Latitude, r.Longitude, lat, lng, "nm")
			if driftNM >= recGeocodeSnapMinNM && driftNM <= recGeocodeSnapMaxNM {
				slog.InfoContext(ctx, "Snapped drifted recommendation coordinates to geocoded feature",
					"name", r.Name, "query", queryName,
					"old_lat", r.Latitude, "old_lng", r.Longitude,
					"new_lat", lat, "new_lng", lng, "drift_nm", driftNM)
				r.Latitude, r.Longitude = lat, lng
			}
		}(i)
	}
	wg.Wait()
}

func radiusToNM(radius int, unit string) float64 {
	r := float64(radius)
	switch unit {
	case "mi":
		return r * 0.868976
	case "km":
		return r / 1.852
	default:
		return r
	}
}

func recommendationCount(radius int, unit string) int {
	nm := radiusToNM(radius, unit)
	count := int(math.Round(nm * 3.0))
	if count < 25 {
		count = 25
	}
	if count > 60 {
		count = 60
	}
	return count
}

func searchBoundaryHint(centerLat, centerLng float64, radiusNM float64) string {
	latDeg := radiusNM / 60.0
	lngDeg := radiusNM / (60.0 * math.Cos(centerLat*math.Pi/180.0))

	nLat := centerLat + latDeg
	sLat := centerLat - latDeg
	eLng := centerLng + lngDeg
	wLng := centerLng - lngDeg

	fmtLat := func(lat float64) string {
		if lat >= 0 {
			return fmt.Sprintf("%.2f°N", lat)
		}
		return fmt.Sprintf("%.2f°S", math.Abs(lat))
	}

	fmtLng := func(lng float64) string {
		if lng >= 0 {
			return fmt.Sprintf("%.2f°E", lng)
		}
		return fmt.Sprintf("%.2f°W", math.Abs(lng))
	}

	hint := fmt.Sprintf(
		"The circle boundary reaches approximately: North edge (%s, %s), South edge (%s, %s), East edge (%s, %s), West edge (%s, %s). "+
			"CRITICAL OPEN-WATER CROSSING RULE: When the center is in open water (such as a strait, sound, channel, or bay), you MUST pass the EXACT circle center coordinates (%.4f, %.4f) to `facility_discovery` — NEVER pass the coordinates of only one island or shore to `facility_discovery`! "+
			"Identify the coastlines, major yachting ports, islands, and inlets located near EACH of those four perimeter coordinates (BOTH the West edge AND East edge, and BOTH the North edge AND South edge) and include all of their charter bases, marinas, moorings, fuel docks, and anchorages.",
		fmtLat(nLat), fmtLng(centerLng),
		fmtLat(sLat), fmtLng(centerLng),
		fmtLat(centerLat), fmtLng(eLng),
		fmtLat(centerLat), fmtLng(wLng),
		centerLat, centerLng,
	)
	if radiusNM > 27.0 {
		hint += " For this large search area (>27 NM), execute your parallel searches in Turn 1 across both central and outer compass sectors."
	}
	return hint
}

func haversine(lat1, lon1, lat2, lon2 float64, unit string) float64 {
	const (
		earthRadiusKm = 6371.0
		earthRadiusMi = 3958.8
		earthRadiusNm = 3440.1
	)

	var r float64
	switch unit {
	case "nm":
		r = earthRadiusNm
	case "mi":
		r = earthRadiusMi
	case "km":
		r = earthRadiusKm
	default:
		r = earthRadiusNm
	}

	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	deltaPhi := (lat2 - lat1) * math.Pi / 180
	deltaLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*
			math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return r * c
}

func (h *Handler) ListRecommendations(w http.ResponseWriter, r *http.Request) {
	v, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := v.ID

	recommendations, err := h.DB.ListVoyageRecommendations(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if guide, gErr := h.DB.GetVoyageGuide(r.Context(), voyageID); gErr == nil && guide != nil {
		enrichRecommendationsFromGuide(recommendations, guide)
	}
	for i := range recommendations {
		normalizeRecommendationRole(&recommendations[i])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"recommendations": recommendations,
	})
}

func (h *Handler) ensureRecChannel(sessionID string, ttl time.Duration) chan model.VoyageRecommendation {
	return h.recHub.ensure(sessionID, ttl)
}

func (h *Handler) broadcastRecommendation(sessionID string, rec model.VoyageRecommendation) {
	h.recHub.broadcast(sessionID, rec)
}

func (h *Handler) StreamRecommendations(w http.ResponseWriter, r *http.Request) {
	streamSSE(w, r, &h.recHub, "recommendation", 30*time.Minute, nil)
}

func (h *Handler) GenerateRecommendations(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	idStr := r.PathValue("id")

	ts := time.Now().Unix()
	sessionID := fmt.Sprintf("recommendation_%d_%d", voyage.ID, ts)
	progressSessionID := fmt.Sprintf("rec_progress_%d_%d", voyage.ID, ts)

	h.ensureProgressChannel(progressSessionID, 25*time.Minute)
	h.ensureRecChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":                 "Recommendation generation started",
		"voyage_id":           idStr,
		"session_id":          sessionID,
		"progress_session_id": progressSessionID,
	})

	go h.performRecommendationGeneration(voyage, sessionID, progressSessionID)
}

func (h *Handler) performRecommendationGeneration(v *model.Voyage, sessionID, progressSessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() {
		<-h.ResearchSem
		h.recHub.close(sessionID)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, "[navigator-agent] Generating recommendations for voyage", "voyage_id", v.ID)
	h.broadcastProgress(progressSessionID, "start", "Starting Local Pilot research")

	const appName = "specialist"
	const userID = "system"

	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "error", err)
	}

	radiusNM := radiusToNM(v.SearchRadius, v.SearchRadiusUnit)
	sessionState := map[string]any{
		"search_mode":        "area",
		"area_radius_meters": radiusNM * 1852.0,
	}
	if v.Latitude != nil && v.Longitude != nil {
		sessionState["stop_latitude"] = *v.Latitude
		sessionState["stop_longitude"] = *v.Longitude
	}
	if err := h.Agent.CreateSession(ctx, appName, userID, sessionID, sessionState); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.broadcastProgress(progressSessionID, "error", "Failed to start research session")
		return
	}

	h.broadcastProgress(progressSessionID, "agent", "Consulting the Local Pilot specialist agent")

	if v.Latitude == nil || v.Longitude == nil {
		slog.ErrorContext(ctx, "Voyage has no coordinates, cannot generate recommendations", "voyage_id", v.ID)
		h.broadcastProgress(progressSessionID, "error", "Voyage has no location set — please add coordinates first")
		return
	}

	locInfo := ""
	if v.LocationName != nil {
		locInfo = strings.TrimSpace(*v.LocationName)
	}
	titleHint := strings.TrimSpace(v.Title)
	if titleHint != "" && !strings.Contains(strings.ToLower(locInfo), strings.ToLower(titleHint)) {
		if locInfo == "" {
			locInfo = titleHint
		} else {
			locInfo = fmt.Sprintf("%s (including %s)", locInfo, titleHint)
		}
	}

	count := recommendationCount(v.SearchRadius, v.SearchRadiusUnit)
	boundaryHint := searchBoundaryHint(*v.Latitude, *v.Longitude, radiusNM)

	var latStr string
	if *v.Latitude >= 0 {
		latStr = fmt.Sprintf("%.4f°N", *v.Latitude)
	} else {
		latStr = fmt.Sprintf("%.4f°S", math.Abs(*v.Latitude))
	}
	var lngStr string
	if *v.Longitude >= 0 {
		lngStr = fmt.Sprintf("%.4f°E", *v.Longitude)
	} else {
		lngStr = fmt.Sprintf("%.4f°W", math.Abs(*v.Longitude))
	}

	prompt := fmt.Sprintf(
		"Recommend anchorages, moorings, marinas, and fuel docks within %d %s of center coordinates (latitude: %.4f, longitude: %.4f, %s, %s; center reference point near %s). "+
			"IMPORTANT: %q is only the nearest point to the circle center — the search circle spans %d %s in ALL directions (N, NE, E, SE, S, SW, W, NW) across multiple shores, harbors, and rivers. "+
			"Your batch_google_search queries and recommendations MUST explicitly cover major harbors, rivers, and creeks on EVERY shore inside the boundary (including Western/NW/SW shores as well as Eastern/NE/SE shores), not just %s. "+
			"Identify every commercial yacht charter embarkation base across ALL major yachting harbors inside the search circle (not just the center point) as voyage_role=\"start_end_base\" with stay_details.is_charter_base=true and stay_details.charter_companies populated. "+
			"Return at least %d recommendations, and include EVERY qualifying marina, mooring field, fuel dock, yacht club, and anchorage you find with verified coordinates — there is no upper limit. "+
			"Pass the exact center coordinates (latitude: %.4f, longitude: %.4f) and full radius (%d %s = %.0f meters) to facility_discovery with mode: \"area\" — do NOT shift the center to a single island or town. %s",
		v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, *v.Longitude, latStr, lngStr, locInfo,
		locInfo, v.SearchRadius, v.SearchRadiusUnit, locInfo,
		count, *v.Latitude, *v.Longitude, v.SearchRadius, v.SearchRadiusUnit, radiusNM*1852.0, boundaryHint)

	if v.StartDate != nil && v.EndDate != nil && v.StartDate.Equal(*v.EndDate) {
		prompt += " This is a day trip — a single-day outing with no overnight stay. " +
			"Prioritize day-use anchorages, lunch stops, and moorings suited to a few hours rather than overnight-only spots."
	}

	if h.DB != nil {
		if guide, gErr := h.DB.GetVoyageGuide(ctx, v.ID); gErr == nil && guide != nil && len(guide.CharterInfo) > 0 {
			prompt += fmt.Sprintf("\n\nExisting Destination Guide Charter & Base Intelligence (verify and include every primary embarkation base within the search radius as voyage_role=\"start_end_base\"):\n%s", string(guide.CharterInfo))
		}
	}

	var parsedCount int
	var statePlacesRaw json.RawMessage
	onEvent := func(evt agent.AgentEvent) {
		for _, p := range evt.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name != "" {
				h.broadcastProgress(progressSessionID, "tool", toolProgressMessage(p.FunctionCall.Name, locInfo))
			}
		}
		if len(evt.Actions.StateDelta) > 0 {
			if rawPlaces, ok := evt.Actions.StateDelta["verified_places"]; ok && len(rawPlaces) > 0 {
				statePlacesRaw = rawPlaces
				h.broadcastProgressSection(progressSessionID, "facilities", fmt.Sprintf("Verified area infrastructure discovered near %s", locInfo), rawPlaces)
			}
		}
	}

	progressStop := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		messages := []string{
			"Consulting regional nautical charts and pilot guides",
			"Evaluating anchorages and mooring fields across sectors",
			"Assessing shelter, depth, and nautical facilities",
			"Synthesizing recommendations for navigational review",
		}
		msgIdx := 0
		for {
			select {
			case <-progressStop:
				return
			case <-t.C:
				msg := messages[msgIdx%len(messages)]
				msgIdx++
				h.broadcastProgress(progressSessionID, "agent", msg)
			}
		}
	}()
	fullText, err := h.Agent.RunStreamingWithEvents(ctx, appName, userID, sessionID, prompt, onEvent)
	close(progressStop)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "high demand") {
			h.broadcastProgress(progressSessionID, "error_503", "Model is busy due to high demand. Please try again in a few minutes.")
		} else if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") || strings.Contains(err.Error(), "Resource exhausted") {
			h.broadcastProgress(progressSessionID, "error_429", "Model rate limit exceeded. Please wait a moment and try again.")
		} else {
			h.broadcastProgress(progressSessionID, "error", "Research agent failed to respond")
		}
		return
	}

	if strings.TrimSpace(fullText) == "" {
		slog.ErrorContext(ctx, "Agent returned empty response")
		h.broadcastProgress(progressSessionID, "error", "Research agent returned an empty response — please try again")
		return
	}

	h.broadcastProgress(progressSessionID, "parsing", "Charting anchorages, moorings, and marinas")

	fullText = cleanJSON(fullText)
	fullText = repairMathInJSON(fullText)

	var wrapper struct {
		Recommendations []model.VoyageRecommendation `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(fullText), &wrapper); err != nil {
		var directRecs []model.VoyageRecommendation
		if errArray := json.Unmarshal([]byte(fullText), &directRecs); errArray == nil {
			wrapper.Recommendations = directRecs
		} else {
			repaired := repairTruncatedJSONArray(fullText)
			if errRepaired := json.Unmarshal([]byte(repaired), &wrapper); errRepaired == nil && len(wrapper.Recommendations) > 0 {
				slog.WarnContext(ctx, "Salvaged recommendations from truncated agent response", "count", len(wrapper.Recommendations))
			} else if errDirectRepaired := json.Unmarshal([]byte(repaired), &directRecs); errDirectRepaired == nil && len(directRecs) > 0 {
				wrapper.Recommendations = directRecs
				slog.WarnContext(ctx, "Salvaged recommendations from truncated agent array", "count", len(wrapper.Recommendations))
			} else {
				slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", fullText)
				h.broadcastProgress(progressSessionID, "error", "Agent returned an unreadable response — please try again")
				return
			}
		}
	}

	recommendations := wrapper.Recommendations
	refineRecommendationCoordinates(ctx, recommendations, locInfo, *v.Latitude, *v.Longitude, radiusNM)
	recommendations = mergeVerifiedPlacesIntoRecommendations(recommendations, statePlacesRaw)
	if h.DB != nil {
		if guide, gErr := h.DB.GetVoyageGuide(ctx, v.ID); gErr == nil && guide != nil {
			enrichRecommendationsFromGuide(recommendations, guide)
		}
	}

	var savedRecs []model.VoyageRecommendation
	for _, rec := range recommendations {
		if rec.Latitude == 0 && rec.Longitude == 0 {
			continue
		}
		if isOpenWaterPassageWaypoint(rec) {
			continue
		}
		normalizeRecommendationRole(&rec)
		dist := haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, v.SearchRadiusUnit)
		if dist > float64(v.SearchRadius) {
			continue
		}
		rec.RadiusMiles = haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, "mi")

		rec.VoyageID = v.ID
		if err := h.DB.CreateVoyageRecommendation(ctx, &rec); err != nil {
			slog.ErrorContext(ctx, "Failed to save recommendation", "voyage_id", v.ID, "error", err)
			continue
		}

		h.broadcastRecommendation(sessionID, rec)
		savedRecs = append(savedRecs, rec)
		parsedCount++
	}

	h.broadcastProgress(progressSessionID, "done", fmt.Sprintf("Local Pilot complete — %d locations charted", parsedCount))
	slog.InfoContext(ctx, "Generated recommendations for voyage", "count", parsedCount, "voyage_id", v.ID)

	// Synchronize discovered Start/End Bases into the Voyage Guide's hubs list
	// so the Destination Guide modal reflects the exact start/stop bases found by specialist.
	h.syncStartEndBasesToGuide(ctx, v.ID, savedRecs)
}

func isCharterOperatorPlace(name string, types []string) bool {
	nl := strings.ToLower(name)
	// Exclude museums, fishing charters, tour boats, ferries, and small-craft rentals
	// that are not bareboat/crewed cruising yacht charter embarkation bases.
	nonCruisingKeywords := []string{
		"museum", "fishing", "sportfishing", "angler", "bait",
		"ferry", "water taxi", "launch service", "parasail", "kayak", "paddle",
		"canoe", "jet ski", "pontoon", "whale watch", "dolphin", "harbor tour",
		"sightseeing", "dinner cruise", "sunset cruise", "party boat",
	}
	for _, kw := range nonCruisingKeywords {
		if strings.Contains(nl, kw) {
			return false
		}
	}
	for _, t := range types {
		tl := strings.ToLower(strings.TrimSpace(t))
		if tl == "yacht_charter" || tl == "boat_rental" {
			return true
		}
	}
	return strings.Contains(nl, "yacht charter") ||
		strings.Contains(nl, "sailing charter") ||
		strings.Contains(nl, "bareboat")
}

func clearCharterBaseFromStayDetails(rec *model.VoyageRecommendation) {
	if len(rec.StayDetails) == 0 {
		return
	}
	var sd map[string]any
	if err := json.Unmarshal(rec.StayDetails, &sd); err != nil || sd == nil {
		return
	}
	_, hasBase := sd["is_charter_base"]
	_, hasCompanies := sd["charter_companies"]
	if !hasBase && !hasCompanies {
		return
	}
	sd["is_charter_base"] = false
	sd["charter_companies"] = []string{}
	if b, err := json.Marshal(sd); err == nil {
		rec.StayDetails = model.RawJSON(b)
	}
}

func addCharterCompanyToRecommendation(rec *model.VoyageRecommendation, companyName string) {
	if isStopoverOrNonBaseLocation(rec.Name, rec.Description, rec.Reasoning, rec.Type) {
		return
	}
	companyName = strings.TrimSpace(companyName)
	var sd map[string]any
	if len(rec.StayDetails) > 0 {
		_ = json.Unmarshal(rec.StayDetails, &sd)
	}
	if sd == nil {
		sd = make(map[string]any)
	}
	sd["is_charter_base"] = true
	var companies []string
	if rawList, ok := sd["charter_companies"].([]any); ok {
		for _, item := range rawList {
			if s, okStr := item.(string); okStr && strings.TrimSpace(s) != "" {
				companies = append(companies, strings.TrimSpace(s))
			}
		}
	} else if strList, ok := sd["charter_companies"].([]string); ok {
		companies = append(companies, strList...)
	}
	if companyName != "" {
		exists := false
		for _, c := range companies {
			if strings.EqualFold(c, companyName) {
				exists = true
				break
			}
		}
		if !exists {
			companies = append(companies, companyName)
		}
	}
	sd["charter_companies"] = companies
	if b, err := json.Marshal(sd); err == nil {
		rec.StayDetails = model.RawJSON(b)
	}
	rec.VoyageRole = "start_end_base"
}

func mergeVerifiedPlacesIntoRecommendations(recs []model.VoyageRecommendation, rawPlaces json.RawMessage) []model.VoyageRecommendation {
	if len(rawPlaces) == 0 {
		return recs
	}
	type verifiedPlace struct {
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Address    string   `json:"address"`
		Latitude   float64  `json:"latitude"`
		Longitude  float64  `json:"longitude"`
		WebsiteURI string   `json:"website_uri"`
		Website    string   `json:"website"`
		Types      []string `json:"types"`
	}
	var places []verifiedPlace
	if err := json.Unmarshal(rawPlaces, &places); err != nil || len(places) == 0 {
		return recs
	}

	existingNames := make(map[string]bool, len(recs))
	for _, r := range recs {
		existingNames[strings.ToLower(strings.TrimSpace(r.Name))] = true
	}

	for _, p := range places {
		name := strings.TrimSpace(p.Name)
		if name == "" || (p.Latitude == 0 && p.Longitude == 0) {
			continue
		}
		isCharterPlace := isCharterOperatorPlace(name, p.Types)
		pt := strings.TrimSpace(p.Type)
		if isCharterPlace && pt == "" {
			pt = "Marina"
		}
		switch pt {
		case "Marina", "Anchorage", "Mooring", "Fuel Station", "Yacht Club", "Boat Launch":
		default:
			continue
		}

		if isCharterPlace {
			bestIdx := -1
			bestDist := 0.5
			for i := range recs {
				if isStopoverOrNonBaseLocation(recs[i].Name, recs[i].Description, recs[i].Reasoning, recs[i].Type) {
					continue
				}
				if recs[i].Latitude == 0 && recs[i].Longitude == 0 {
					continue
				}
				d := haversine(recs[i].Latitude, recs[i].Longitude, p.Latitude, p.Longitude, "nm")
				if d < bestDist {
					bestDist = d
					bestIdx = i
				}
			}
			if bestIdx >= 0 {
				addCharterCompanyToRecommendation(&recs[bestIdx], name)
			}
		}

		lowerName := strings.ToLower(name)
		if existingNames[lowerName] {
			continue
		}
		tooClose := false
		for _, r := range recs {
			if r.Latitude == 0 && r.Longitude == 0 {
				continue
			}
			if haversine(r.Latitude, r.Longitude, p.Latitude, p.Longitude, "nm") < 0.08 &&
				strings.EqualFold(strings.TrimSpace(r.Type), pt) {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}

		url := p.WebsiteURI
		if url == "" {
			url = p.Website
		}
		var refs model.RawJSON
		if url != "" {
			if rb, err := json.Marshal([]string{url}); err == nil {
				refs = model.RawJSON(rb)
			}
		}
		desc := fmt.Sprintf("Verified %s discovered via regional nautical scan", strings.ToLower(pt))
		if isCharterPlace {
			desc = fmt.Sprintf("Verified yacht charter base and embarkation facility (%s)", pt)
		}
		if p.Address != "" {
			desc = fmt.Sprintf("%s (%s)", desc, p.Address)
		}
		rec := model.VoyageRecommendation{
			Name:        name,
			Type:        pt,
			Latitude:    p.Latitude,
			Longitude:   p.Longitude,
			URL:         url,
			Description: desc,
			Reasoning:   "Included from verified Google Places nautical infrastructure sweep within the cruising circle.",
			References:  refs,
		}
		if isCharterPlace && !isStopoverOrNonBaseLocation(rec.Name+" "+p.Address, rec.Description, rec.Reasoning, rec.Type) {
			addCharterCompanyToRecommendation(&rec, name)
		}
		normalizeRecommendationRole(&rec)
		recs = append(recs, rec)
		existingNames[lowerName] = true
	}
	return recs
}

func extractDistinctiveLocationTokens(fields ...string) []string {
	genericWords := map[string]bool{
		"marina": true, "harbor": true, "harbour": true, "yacht": true, "yachts": true,
		"yachting": true, "center": true, "centre": true, "club": true, "yard": true,
		"boatyard": true, "wharf": true, "dock": true, "docks": true, "pier": true,
		"basin": true, "cove": true, "port": true, "point": true, "island": true,
		"islands": true, "sound": true, "river": true, "creek": true, "channel": true,
		"passage": true, "north": true, "south": true, "east": true, "west": true,
		"inner": true, "outer": true, "upper": true, "lower": true, "downtown": true,
		"waterfront": true, "village": true, "town": true, "city": true, "county": true,
		"state": true, "united": true, "states": true, "charter": true, "charters": true,
		"sailing": true, "boat": true, "boats": true, "rental": true, "rentals": true,
	}
	var tokens []string
	seen := make(map[string]bool)
	for _, raw := range fields {
		f := strings.ToLower(strings.TrimSpace(raw))
		if f == "" {
			continue
		}
		if idx := strings.Index(f, ","); idx > 0 {
			phrase := strings.TrimSpace(f[:idx])
			if len(phrase) >= 4 && !genericWords[phrase] && !seen[phrase] {
				seen[phrase] = true
				tokens = append(tokens, phrase)
			}
		}
		cleaned := strings.NewReplacer("/", " ", ",", " ", "(", " ", ")", " ", "-", " ").Replace(f)
		for _, w := range strings.Fields(cleaned) {
			if len(w) >= 4 && !genericWords[w] && !seen[w] {
				seen[w] = true
				tokens = append(tokens, w)
			}
		}
	}
	return tokens
}

func enrichRecommendationsFromGuide(recs []model.VoyageRecommendation, guide *model.VoyageGuide) {
	if len(recs) == 0 || guide == nil || len(guide.CharterInfo) == 0 {
		return
	}
	var charter struct {
		IsCharterDestination bool `json:"is_charter_destination"`
		Companies            []struct {
			Name       string  `json:"name"`
			BaseMarina string  `json:"base_marina"`
			Location   string  `json:"location"`
			Latitude   float64 `json:"latitude"`
			Longitude  float64 `json:"longitude"`
			URL        string  `json:"url"`
		} `json:"companies"`
	}
	if err := json.Unmarshal(guide.CharterInfo, &charter); err != nil {
		return
	}
	if !charter.IsCharterDestination && len(charter.Companies) == 0 {
		return
	}

	for _, comp := range charter.Companies {
		compName := strings.TrimSpace(comp.Name)
		if compName == "" || !isCharterOperatorPlace(compName, []string{"yacht_charter"}) {
			continue
		}
		matchTokens := extractDistinctiveLocationTokens(comp.BaseMarina, comp.Location)

		for i := range recs {
			if isStopoverOrNonBaseLocation(recs[i].Name, recs[i].Description, recs[i].Reasoning, recs[i].Type) {
				continue
			}
			matched := false
			if comp.Latitude != 0 && comp.Longitude != 0 && recs[i].Latitude != 0 && recs[i].Longitude != 0 {
				if haversine(comp.Latitude, comp.Longitude, recs[i].Latitude, recs[i].Longitude, "nm") <= 0.8 {
					matched = true
				}
			}
			if !matched {
				// Match strictly against recommendation Name (never Description or Reasoning,
				// which often mention nearby cities like "within cruising range of ...").
				recName := strings.ToLower(recs[i].Name)
				for _, tok := range matchTokens {
					if tok != "" && strings.Contains(recName, tok) {
						matched = true
						break
					}
				}
			}
			if matched {
				addCharterCompanyToRecommendation(&recs[i], compName)
			}
		}
	}
}

func (h *Handler) syncStartEndBasesToGuide(ctx context.Context, voyageID int64, recs []model.VoyageRecommendation) {
	if len(recs) == 0 || h.DB == nil {
		return
	}
	guide, err := h.DB.GetVoyageGuide(ctx, voyageID)
	if err != nil || guide == nil || strings.HasPrefix(guide.Summary, "Error:") {
		return
	}

	type hubItem struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		URL         string   `json:"url,omitempty"`
		VoyageRole  string   `json:"voyage_role,omitempty"`
		References  []string `json:"references,omitempty"`
	}
	var hubs []hubItem
	if len(guide.Hubs) > 0 {
		_ = json.Unmarshal(guide.Hubs, &hubs)
	}

	existingNames := make(map[string]int, len(hubs))
	for i, hb := range hubs {
		existingNames[strings.ToLower(strings.TrimSpace(hb.Name))] = i
	}

	changed := false
	for _, r := range recs {
		if !isVerifiedCharterEmbarkationBase(r.Name, r.Description, r.Reasoning, r.VoyageRole, string(r.StayDetails)) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(r.Name))
		if idx, exists := existingNames[key]; exists {
			if hubs[idx].VoyageRole != "start_end_base" {
				hubs[idx].VoyageRole = "start_end_base"
				changed = true
			}
			continue
		}
		var refs []string
		if len(r.References) > 0 {
			_ = json.Unmarshal(r.References, &refs)
		}
		desc := r.Description
		if r.Reasoning != "" && !strings.Contains(desc, r.Reasoning) {
			desc = strings.TrimSpace(desc + " — " + r.Reasoning)
		}
		hubs = append([]hubItem{{
			Name:        r.Name,
			Description: desc,
			URL:         r.URL,
			VoyageRole:  "start_end_base",
			References:  refs,
		}}, hubs...)
		existingNames[key] = 0
		changed = true
	}

	if changed {
		if updatedBytes, err := json.Marshal(hubs); err == nil {
			guide.Hubs = model.RawJSON(updatedBytes)
			_ = h.DB.CreateVoyageGuide(ctx, guide)
		}
	}
}

func isOpenWaterPassageWaypoint(rec model.VoyageRecommendation) bool {
	name := strings.ToLower(strings.TrimSpace(rec.Name))
	desc := strings.ToLower(rec.Description + " " + rec.Reasoning + " " + string(rec.StayDetails))
	if strings.Contains(name, "waypoint") || strings.Contains(name, "channel crossing") ||
		strings.Contains(name, "shipping lane") || strings.Contains(name, "traffic separation") {
		return true
	}
	if (strings.Contains(desc, "open ocean channel") || strings.Contains(desc, "open-water passage") || strings.Contains(desc, "shipping channel crossing")) &&
		(strings.Contains(desc, "2000+ ft") || strings.Contains(desc, "1000+ ft") || strings.Contains(desc, "open ocean crossing")) {
		return true
	}
	return false
}

// isStopoverOrNonBaseLocation uses strictly region-independent structural rules
// (facility type, generic non-base facility category, and stopover/island description
// semantics) to identify locations that cannot be commercial charter embarkation bases.
func isStopoverOrNonBaseLocation(name, desc, reasoning, recType string) bool {
	t := strings.ToLower(strings.TrimSpace(recType))
	if strings.Contains(t, "anchor") || strings.Contains(t, "moor") ||
		strings.Contains(t, "fuel") || strings.Contains(t, "launch") ||
		strings.Contains(t, "yacht club") {
		return true
	}
	n := strings.ToLower(name)
	nonBaseCategoryKeywords := []string{
		"resort, spa", "resort & spa", "island resort", "beach resort",
		"harbour inn", "harbor inn", "beach club", "shipyard", "boatworks",
		"marine railway", "museum", "state park", "national park",
		"wildlife refuge", "nature reserve", "salt pond", "inner harbor & pond",
		"mooring field",
	}
	for _, kw := range nonBaseCategoryKeywords {
		if strings.Contains(n, kw) {
			return true
		}
	}

	full := strings.ToLower(name + " " + desc + " " + reasoning)
	hasExplicitCharterBase := strings.Contains(full, "charter base") ||
		strings.Contains(full, "charter fleet") ||
		strings.Contains(full, "bareboat charter") ||
		strings.Contains(full, "embarkation base") ||
		strings.Contains(full, "charter embarkation")
	if !hasExplicitCharterBase {
		stopoverPhrases := []string{
			"island cruising destination", "out-island", "uninhabited",
			"hurricane hole", "first or last night stopover", "overnight stopover",
		}
		for _, sp := range stopoverPhrases {
			if strings.Contains(full, sp) {
				return true
			}
		}
	}
	return false
}

func normalizeRecommendationRole(rec *model.VoyageRecommendation) {
	t := strings.ToLower(strings.TrimSpace(rec.Type))
	if strings.Contains(t, "fuel") || strings.Contains(t, "launch") {
		rec.VoyageRole = "service_only"
		clearCharterBaseFromStayDetails(rec)
		return
	}
	if strings.Contains(t, "anchor") || strings.Contains(t, "moor") {
		if strings.ToLower(strings.TrimSpace(rec.VoyageRole)) == "day_stop" {
			rec.VoyageRole = "day_stop"
			clearCharterBaseFromStayDetails(rec)
			return
		}
		rec.VoyageRole = "overnight_stop"
		clearCharterBaseFromStayDetails(rec)
		return
	}
	if isStopoverOrNonBaseLocation(rec.Name, rec.Description, rec.Reasoning, rec.Type) {
		rec.VoyageRole = "overnight_stop"
		clearCharterBaseFromStayDetails(rec)
		return
	}

	role := strings.ToLower(strings.TrimSpace(rec.VoyageRole))
	if isVerifiedCharterEmbarkationBase(rec.Name, rec.Description, rec.Reasoning, role, string(rec.StayDetails)) {
		rec.VoyageRole = "start_end_base"
		var sd map[string]any
		if len(rec.StayDetails) > 0 {
			_ = json.Unmarshal(rec.StayDetails, &sd)
		}
		if sd == nil {
			sd = make(map[string]any)
		}
		sd["is_charter_base"] = true
		if b, err := json.Marshal(sd); err == nil {
			rec.StayDetails = model.RawJSON(b)
		}
		return
	}

	clearCharterBaseFromStayDetails(rec)
	if role == "day_stop" || role == "service_only" {
		rec.VoyageRole = role
		return
	}
	rec.VoyageRole = "overnight_stop"
}

func isVerifiedCharterEmbarkationBase(name, desc, reasoning, role, stayDetailsJSON string) bool {
	if isStopoverOrNonBaseLocation(name, desc, reasoning, "") {
		return false
	}
	var sd struct {
		IsCharterBase    bool     `json:"is_charter_base"`
		CharterCompanies []string `json:"charter_companies"`
	}
	if len(stayDetailsJSON) > 0 {
		_ = json.Unmarshal([]byte(stayDetailsJSON), &sd)
	}
	for _, c := range sd.CharterCompanies {
		if isCharterOperatorPlace(c, []string{"yacht_charter"}) {
			return true
		}
	}

	full := strings.ToLower(name + " " + desc + " " + reasoning)

	// Region-agnostic positive evidence of a commercial charter embarkation base.
	charterEvidence := []string{
		"charter base", "charter fleet", "bareboat charter",
		"embarkation base", "charter embarkation", "homeport for charter",
		"turnaround base",
	}
	for _, sig := range charterEvidence {
		if strings.Contains(full, sig) {
			return true
		}
	}

	return false
}
