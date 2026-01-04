package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"google.golang.org/api/idtoken"

	appcontext "app/context"
	"app/models"
)

type GuideAgentOutput struct {
	Summary          string          `json:"summary"`
	SailingSeason    json.RawMessage `json:"sailing_season"`
	Hazards          json.RawMessage `json:"hazards"`
	Hubs             json.RawMessage `json:"hubs"`
	CharterInfo      json.RawMessage `json:"charter_info"`
	Airports         json.RawMessage `json:"airports"`
	CountryInfo      json.RawMessage `json:"country_info"`
	Currencies       json.RawMessage `json:"currencies"`
	PointsOfInterest json.RawMessage `json:"points_of_interest"`
}

func (h *Handler) UploadVoyageMap(w http.ResponseWriter, r *http.Request) {
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

	// Ensure maps directory exists
	mapsDir := filepath.Join(h.ContentDir, "maps")
	if err := os.MkdirAll(mapsDir, 0755); err != nil {
		log.Errorf("Failed to create maps directory: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Save file
	filename := fmt.Sprintf("voyage_%d.png", voyageID)
	dstPath := filepath.Join(mapsDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		log.Errorf("Failed to create map file: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Errorf("Failed to save map file: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "url": "/maps/" + filename})
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
	log.SetPrefix("guide-agent")

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "guide_agent"
	userID := "system"
	sessionID := fmt.Sprintf("voyage_%d", voyage.ID)

	ctx := context.Background()

	// SECURE CLIENT CREATION
	// If we are calling a Cloud Run service securely, we need an ID Token.
	var client *http.Client
	var err error

	if strings.Contains(agentURL, "run.app") {
		// Create an authenticated client that appends the OIDC token for the specific audience (agentURL)
		client, err = idtoken.NewClient(ctx, agentURL)
		if err != nil {
			log.Errorf("Failed to create authenticated client: %v", err)
			return
		}
	} else {
		// Default client for localhost development
		client = http.DefaultClient
	}

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		log.Infof("Failed to create agent session: %v", err)
	} else {
		respSession.Body.Close()
	}

	// 2. Run Agent
	locName := "Unknown"
	if voyage.LocationName != nil {
		locName = *voyage.LocationName
	}

	prompt := fmt.Sprintf("Research sailing guide for location: %s. Include summary, sailing_season, hazards, hubs, charter_info, airports, country_info (including language, timezone, emergency numbers), currencies, and points_of_interest.", locName)

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
		log.Infof("Failed to call agent: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Infof("Agent returned error: %s", body)
		return
	}

	var events []AgentEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		log.Infof("Failed to decode agent response: %v", err)
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
		log.Info("No response from agent")
		return
	}

	responseText = cleanJSON(responseText)

	var output GuideAgentOutput
	if err := json.Unmarshal([]byte(responseText), &output); err != nil {
		log.Infof("Failed to unmarshal agent JSON output: %v. Raw: %s", err, responseText)
		return
	}

	guide := &models.VoyageGuide{
		VoyageID:         voyage.ID,
		Summary:          output.Summary,
		SailingSeason:    models.RawJSON(output.SailingSeason),
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
		log.Infof("Failed to save voyage guide: %v", err)
	}
	log.Infof("Voyage guide saved for voyage %d", voyage.ID)
}

type VoyageGuideResponse struct {
	*models.VoyageGuide
	MapURL string `json:"map_url,omitempty"`
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

	// Check for map image
	var mapURL string
	mapFilename := fmt.Sprintf("voyage_%d.png", voyageID)
	mapPath := filepath.Join(h.ContentDir, "maps", mapFilename)
	if _, statErr := os.Stat(mapPath); statErr == nil {
		mapURL = "/maps/" + mapFilename
	}

	// If we have neither a guide nor a map, then it's a 404
	if err != nil && mapURL == "" {
		// Log the error if it's something other than "not found" (depending on DB impl)
		// For now assuming err implies not found or db error
		log.Warnf("Voyage guide not found for voyage %d: %v", voyageID, err)
		http.Error(w, "Voyage guide not found", http.StatusNotFound)
		return
	}

	if guide == nil {
		guide = &models.VoyageGuide{
			VoyageID: voyageID,
		}
	}

	resp := VoyageGuideResponse{
		VoyageGuide: guide,
		MapURL:      mapURL,
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
		log.Errorf("Failed to list briefings for voyage %d: %v", voyageID, err)
		http.Error(w, "Failed to list briefings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefings)
}