package handlers

import (
	"bytes"
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Limit upload size to 10MB
	r.ParseMultipartForm(10 << 20)

	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Failed to retrieve image", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file content is an image
	buff := make([]byte, 512)
	if _, err := file.Read(buff); err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to reset file pointer", http.StatusInternalServerError)
		return
	}

	contentType := http.DetectContentType(buff)
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "Invalid file type: must be an image", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to read image data", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.DB.SaveVoyageMap(r.Context(), voyageID, data); err != nil {
		slog.ErrorContext(r.Context(), "Failed to save map image to DB", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Auth Check: Public OR Owner
	isAuthorized := false
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
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
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	data, err := h.DB.GetVoyageMap(r.Context(), voyageID)
	if err != nil || len(data) == 0 {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (h *Handler) TriggerGuideResearch(w http.ResponseWriter, r *http.Request) {
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

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "voyage_id": idStr})

	// Async processing
	go h.performGuideResearch(voyage)
}

func (h *Handler) performGuideResearch(voyage *models.Voyage) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performGuideResearchLogic(voyage)
}

func (h *Handler) performGuideResearchLogic(voyage *models.Voyage) {
	ctx := context.Background()
	slog.InfoContext(ctx, fmt.Sprintf("[guide-agent] Starting research for voyage %d", voyage.ID))

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "guide_agent"
	userID := "system"
	sessionID := fmt.Sprintf("voyage_%d", voyage.ID)

	client := h.AgentClient

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
	} else if respSession != nil {
		respSession.Body.Close()
	}

	// 2. Run Agent
	locName := "Unknown"
	if voyage.LocationName != nil {
		locName = *voyage.LocationName
	}

	locDetail := locName
	if voyage.PreciseLocation != nil && *voyage.PreciseLocation != "" {
		locDetail = fmt.Sprintf("%s (Lat: %f, Lng: %f)", locName, *voyage.Latitude, *voyage.Longitude)
	}
	prompt := fmt.Sprintf("Research sailing guide for location: %s. Include summary, sailing_season, hazards, hubs, charter_info, airports, country_info (including language, timezone, emergency numbers), currencies, and points_of_interest.", locDetail)

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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Check ownership via Voyage
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		// If voyage not found, guide also not found
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
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
		http.Error(w, "Voyage guide not found", http.StatusNotFound)
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
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	// Find Public Voyage
	voyage, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		// This likely means not found or not public
		http.Error(w, "Voyage not found or not public", http.StatusNotFound)
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	briefings, err := h.DB.ListVoyageBriefings(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list briefings for voyage", "voyage_id", voyageID, "error", err)
		http.Error(w, "Failed to list briefings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefings)
}
