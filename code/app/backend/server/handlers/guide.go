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

	appcontext "app/context"
	"app/models"
)

type GuideAgentOutput struct {
	Summary          string          `json:"summary"`
	SailingSeason    json.RawMessage `json:"sailing_season"`
	SecuritySafety   json.RawMessage `json:"security_safety"`
	Hazards          json.RawMessage `json:"hazards"`
	Hubs             json.RawMessage `json:"hubs"`
	CharterInfo      json.RawMessage `json:"charter_info"`
	Airports         json.RawMessage `json:"airports"`
	CountryInfo      json.RawMessage `json:"country_info"`
	Currencies       json.RawMessage `json:"currencies"`
	PointsOfInterest json.RawMessage `json:"points_of_interest"`
}

func (h *Handler) UploadVoyageSnapshot(w http.ResponseWriter, r *http.Request) {
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

	// Check ownership
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	// Limit upload size to 10MB
	r.ParseMultipartForm(10 << 20)

	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to retrieve image")
		return
	}
	defer file.Close()

	// Validate file content is an image
	buff := make([]byte, 512)
	if _, err := file.Read(buff); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to reset file pointer")
		return
	}

	contentType := http.DetectContentType(buff)
	if !strings.HasPrefix(contentType, "image/") {
		writeError(w, http.StatusBadRequest, "Invalid file type: must be an image")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to read image data", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if err := h.DB.SaveVoyageMap(r.Context(), voyageID, data); err != nil {
		slog.ErrorContext(r.Context(), "Failed to save map image to DB", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.WriteHeader(http.StatusOK)
	// Return the new URL (cache busted with timestamp)
	url := fmt.Sprintf("/api/v1/voyages/%d/map_image?t=%d", voyageID, time.Now().Unix())
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "url": url})
}

func (h *Handler) GetVoyageMapImage(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Auth Check: Public OR Owner
	isAuthorized := false
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	if voyage.IsPublic {
		isAuthorized = true
	} else {
		// Manual Session Check since this route is Public (Level 0)
		cookie, err := r.Cookie("navalplan_session")
		if err == nil {
			session, _ := h.DB.GetSession(r.Context(), cookie.Value)
			if session != nil && session.PersonID == voyage.PersonID {
				isAuthorized = true
			}
		}
	}

	if !isAuthorized {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	data, err := h.DB.GetVoyageMap(r.Context(), voyageID)
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusNotFound, "Image not found")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (h *Handler) TriggerGuideResearch(w http.ResponseWriter, r *http.Request) {
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

	sessionID := fmt.Sprintf("guide_%d_%d", voyageID, time.Now().Unix())

	// Pre-register progress channel before spawning goroutine so early events are buffered.
	h.ensureProgressChannel(sessionID, 25*time.Minute)

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "voyage_id": idStr, "session_id": sessionID})

	// Async processing
	go h.performGuideResearch(voyage, sessionID)
}

func (h *Handler) performGuideResearch(voyage *models.Voyage, sessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performGuideResearchLogic(voyage, sessionID)
}

func (h *Handler) performGuideResearchLogic(voyage *models.Voyage, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[guide-agent] Starting research for voyage %d", voyage.ID))
	h.broadcastProgress(sessionID, "start", "Starting voyage guide research")

	const appName = "pilot"
	const userID = "system"
	agentSessionID := fmt.Sprintf("voyage_%d_%d", voyage.ID, time.Now().Unix())

	// 1. Create Session
	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.saveEmptyGuide(ctx, voyage)
		return
	}

	h.broadcastProgress(sessionID, "agent", "Consulting the Pilot guide agent")

	// 2. Build prompt
	locName := "Unknown"
	if voyage.LocationName != nil {
		locName = *voyage.LocationName
	}

	locDetail := locName
	if voyage.PreciseLocation != nil && *voyage.PreciseLocation != "" &&
		voyage.Latitude != nil && voyage.Longitude != nil {
		locDetail = fmt.Sprintf("%s (Lat: %f, Lng: %f)", locName, *voyage.Latitude, *voyage.Longitude)
	}
	prompt := fmt.Sprintf("Research sailing guide for location: %s. Include summary, sailing_season, hazards, hubs, charter_info, airports, country_info (including language, timezone, emergency numbers), currencies, and points_of_interest.", locDetail)

	// 3. Run Agent
	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		h.saveEmptyGuide(ctx, voyage)
		return
	}

	if responseText == "" {
		slog.ErrorContext(ctx, "No response from agent")
		h.saveEmptyGuide(ctx, voyage)
		return
	}

	cleanedResponse := cleanJSON(responseText)
	if cleanedResponse == "" {
		slog.ErrorContext(ctx, "Agent returned non-JSON response", "response", responseText)
		h.saveEmptyGuide(ctx, voyage)
		return
	}

	var output GuideAgentOutput
	if err := json.Unmarshal([]byte(cleanedResponse), &output); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", cleanedResponse)
		h.saveEmptyGuide(ctx, voyage)
		return
	}

	guide := &models.VoyageGuide{
		VoyageID:         voyage.ID,
		Summary:          output.Summary,
		SailingSeason:    models.RawJSON(output.SailingSeason),
		SecuritySafety:   models.RawJSON(output.SecuritySafety),
		Hazards:          models.RawJSON(output.Hazards),
		Hubs:             models.RawJSON(output.Hubs),
		CharterInfo:      models.RawJSON(output.CharterInfo),
		Airports:         models.RawJSON(output.Airports),
		CountryInfo:      models.RawJSON(output.CountryInfo),
		Currencies:       models.RawJSON(output.Currencies),
		PointsOfInterest: models.RawJSON(output.PointsOfInterest),
		CreatedAt:        time.Now(),
	}

	if err := h.DB.CreateVoyageGuide(ctx, guide); err != nil {
		slog.ErrorContext(ctx, "Failed to save voyage guide", "error", err)
	}
	slog.InfoContext(ctx, fmt.Sprintf("Voyage guide saved for voyage %d", voyage.ID))
	h.broadcastProgress(sessionID, "done", "Voyage guide research complete")
}

func (h *Handler) saveEmptyGuide(ctx context.Context, voyage *models.Voyage) {
	guide := &models.VoyageGuide{
		VoyageID:         voyage.ID,
		Summary:          "Error: Research agent failed to provide a guide for this location.",
		SailingSeason:    models.RawJSON([]byte(`{}`)),
		SecuritySafety:   models.RawJSON([]byte(`{}`)),
		Hazards:          models.RawJSON([]byte(`{}`)),
		Hubs:             models.RawJSON([]byte(`[]`)),
		CharterInfo:      models.RawJSON([]byte(`{}`)),
		Airports:         models.RawJSON([]byte(`[]`)),
		CountryInfo:      models.RawJSON([]byte(`{}`)),
		Currencies:       models.RawJSON([]byte(`[]`)),
		PointsOfInterest: models.RawJSON([]byte(`[]`)),
		CreatedAt:        time.Now(),
	}
	h.DB.CreateVoyageGuide(ctx, guide)
}

type VoyageGuideResponse struct {
	Guide  *models.VoyageGuide `json:"guide"`
	MapURL string              `json:"map_url,omitempty"`
}

func (h *Handler) GetVoyageGuide(w http.ResponseWriter, r *http.Request) {
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

	// Check ownership via Voyage
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		// If voyage not found, guide also not found
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	guide, err := h.DB.GetVoyageGuide(r.Context(), voyageID)
	// We don't error out immediately if guide is not found,
	// because we might still have a map image.

	// Check for map image in DB
	var mapURL string
	if mapData, _ := h.DB.GetVoyageMap(r.Context(), voyageID); len(mapData) > 0 {
		mapURL = fmt.Sprintf("/api/v1/voyages/%d/map_image", voyageID)
	}

	// If we have neither a guide nor a map, then it's a 404
	if err != nil && mapURL == "" {
		// Log the error if it's something other than "not found" (depending on DB impl)
		// For now assuming err implies not found or db error
		slog.WarnContext(r.Context(), "Voyage guide not found for voyage", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusNotFound, "Voyage guide not found")
		return
	}

	if guide == nil {
		guide = &models.VoyageGuide{
			VoyageID: voyageID,
		}
	}

	resp := VoyageGuideResponse{
		Guide:  guide,
		MapURL: mapURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type PublicVoyageReport struct {
	Voyage          *models.Voyage                `json:"voyage"`
	Guide           *models.VoyageGuide           `json:"guide"`
	Stops           []models.Stop                 `json:"stops"`
	Briefings       []models.Briefing             `json:"briefings"`
	Recommendations []models.VoyageRecommendation `json:"recommendations"`
	MapURL          string                        `json:"map_url,omitempty"`
}

func (h *Handler) GetPublicVoyageGuide(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "Token required")
		return
	}

	// Find Public Voyage
	voyage, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		// This likely means not found or not public
		writeError(w, http.StatusNotFound, "Voyage not found or not public")
		return
	}

	// Fetch Guide
	guide, err := h.DB.GetVoyageGuide(r.Context(), voyage.ID)
	// We don't error out immediately if guide is not found

	// Fetch Stops
	stops, err := h.DB.ListStops(r.Context(), voyage.ID, 0, 0)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list stops for public voyage", "voyage_id", voyage.ID, "error", err)
		// Continue? Or fail? Let's continue with empty stops
		stops = []models.Stop{}
	}

	// Fetch Briefings
	briefings, err := h.DB.ListVoyageBriefings(r.Context(), voyage.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list briefings for public voyage", "voyage_id", voyage.ID, "error", err)
		briefings = []models.Briefing{}
	}

	// Fetch Recommendations
	recs, err := h.DB.ListVoyageRecommendations(r.Context(), voyage.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations for public voyage", "voyage_id", voyage.ID, "error", err)
		recs = []models.VoyageRecommendation{}
	}

	// Check for map image in DB
	var mapURL string
	if mapData, _ := h.DB.GetVoyageMap(r.Context(), voyage.ID); len(mapData) > 0 {
		mapURL = fmt.Sprintf("/api/v1/voyages/%d/map_image", voyage.ID)
	}

	if guide == nil {
		guide = &models.VoyageGuide{
			VoyageID: voyage.ID,
		}
	}

	resp := PublicVoyageReport{
		Voyage:          voyage,
		Guide:           guide,
		Stops:           stops,
		Briefings:       briefings,
		Recommendations: recs,
		MapURL:          mapURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ListVoyageBriefings(w http.ResponseWriter, r *http.Request) {
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

	// Check ownership
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	briefings, err := h.DB.ListVoyageBriefings(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list briefings for voyage", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list briefings")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefings)
}
