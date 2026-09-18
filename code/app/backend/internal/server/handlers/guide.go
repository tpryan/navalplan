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

	"app/internal/model"
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

	r.ParseMultipartForm(10 << 20)

	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to retrieve image")
		return
	}
	defer file.Close()

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

	isAuthorized := false
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	if voyage.IsPublic {
		isAuthorized = true
	} else {
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

	jobKey := fmt.Sprintf("guide:%d", voyageID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Guide research is already in progress for this voyage")
		return
	}

	sessionID := fmt.Sprintf("guide_%d_%d", voyageID, time.Now().Unix())

	h.ensureProgressChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "voyage_id": idStr, "session_id": sessionID})

	go h.performGuideResearch(voyage, sessionID, jobKey)
}

func (h *Handler) performGuideResearch(voyage *model.Voyage, sessionID, jobKey string) {
	defer h.releaseJob(jobKey)
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performGuideResearchLogic(voyage, sessionID)
}

func (h *Handler) performGuideResearchLogic(voyage *model.Voyage, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[guide-agent] Starting research for voyage %d", voyage.ID))
	h.broadcastProgress(sessionID, "start", "Starting voyage guide research")

	const appName = "pilot"
	const userID = "system"
	agentSessionID := fmt.Sprintf("voyage_%d_%d", voyage.ID, time.Now().Unix())

	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.failGuideResearch(ctx, voyage, sessionID, "Failed to start research session")
		return
	}

	h.broadcastProgress(sessionID, "agent", "Consulting the Pilot guide agent")

	locName := "Unknown"
	if voyage.LocationName != nil {
		locName = *voyage.LocationName
	}

	locDetail := locName
	if voyage.PreciseLocation != nil && *voyage.PreciseLocation != "" &&
		voyage.Latitude != nil && voyage.Longitude != nil {
		locDetail = fmt.Sprintf("%s (Lat: %f, Lng: %f)", locName, *voyage.Latitude, *voyage.Longitude)
	}

	scope := ""
	if voyage.Latitude != nil && voyage.Longitude != nil && voyage.SearchRadius > 0 {
		radiusNM := radiusToNM(voyage.SearchRadius, voyage.SearchRadiusUnit)
		boundary := searchBoundaryHint(*voyage.Latitude, *voyage.Longitude, radiusNM)
		scope = fmt.Sprintf(" Cover the entire cruising area within a %d %s (~%.0f nm) radius of this center point, not just the immediate vicinity of the named location. %s", voyage.SearchRadius, voyage.SearchRadiusUnit, radiusNM, boundary)
	}

	prompt := fmt.Sprintf("Research sailing guide for location: %s.%s Include summary, sailing_season, hazards, hubs, charter_info, airports, country_info (including language, timezone, emergency numbers), currencies, and points_of_interest.", locDetail, scope)

	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		h.failGuideResearch(ctx, voyage, sessionID, "Research agent failed to respond")
		return
	}

	if responseText == "" {
		slog.ErrorContext(ctx, "No response from agent")
		h.failGuideResearch(ctx, voyage, sessionID, "Agent returned no data")
		return
	}

	cleanedResponse := cleanJSON(responseText)
	if cleanedResponse == "" {
		slog.ErrorContext(ctx, "Agent returned non-JSON response", "response", responseText)
		h.failGuideResearch(ctx, voyage, sessionID, "Agent response could not be parsed")
		return
	}

	var output GuideAgentOutput
	if err := json.Unmarshal([]byte(cleanedResponse), &output); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", cleanedResponse)
		h.failGuideResearch(ctx, voyage, sessionID, "Agent response could not be parsed")
		return
	}

	guide := &model.VoyageGuide{
		VoyageID:         voyage.ID,
		Summary:          output.Summary,
		SailingSeason:    model.RawJSON(output.SailingSeason),
		SecuritySafety:   model.RawJSON(output.SecuritySafety),
		Hazards:          model.RawJSON(output.Hazards),
		Hubs:             model.RawJSON(output.Hubs),
		CharterInfo:      model.RawJSON(output.CharterInfo),
		Airports:         model.RawJSON(output.Airports),
		CountryInfo:      model.RawJSON(output.CountryInfo),
		Currencies:       model.RawJSON(output.Currencies),
		PointsOfInterest: model.RawJSON(output.PointsOfInterest),
		CreatedAt:        time.Now(),
	}

	if err := h.DB.CreateVoyageGuide(ctx, guide); err != nil {
		slog.ErrorContext(ctx, "Failed to save voyage guide", "error", err)
	}
	slog.InfoContext(ctx, fmt.Sprintf("Voyage guide saved for voyage %d", voyage.ID))
	h.broadcastProgress(sessionID, "done", "Voyage guide research complete")
}

func (h *Handler) failGuideResearch(ctx context.Context, voyage *model.Voyage, sessionID, msg string) {
	h.broadcastProgress(sessionID, "error", msg)

	existing, err := h.DB.GetVoyageGuide(ctx, voyage.ID)
	if err == nil && existing != nil && !strings.HasPrefix(existing.Summary, "Error:") {
		return
	}
	h.saveEmptyGuide(ctx, voyage)
}

func (h *Handler) saveEmptyGuide(ctx context.Context, voyage *model.Voyage) {
	guide := &model.VoyageGuide{
		VoyageID:         voyage.ID,
		Summary:          "Error: Research agent failed to provide a guide for this location.",
		SailingSeason:    model.RawJSON([]byte(`{}`)),
		SecuritySafety:   model.RawJSON([]byte(`{}`)),
		Hazards:          model.RawJSON([]byte(`{}`)),
		Hubs:             model.RawJSON([]byte(`[]`)),
		CharterInfo:      model.RawJSON([]byte(`{}`)),
		Airports:         model.RawJSON([]byte(`[]`)),
		CountryInfo:      model.RawJSON([]byte(`{}`)),
		Currencies:       model.RawJSON([]byte(`[]`)),
		PointsOfInterest: model.RawJSON([]byte(`[]`)),
		CreatedAt:        time.Now(),
	}
	h.DB.CreateVoyageGuide(ctx, guide)
}

type VoyageGuideResponse struct {
	Guide  *model.VoyageGuide `json:"guide"`
	MapURL string             `json:"map_url,omitempty"`
}

func (h *Handler) GetVoyageGuide(w http.ResponseWriter, r *http.Request) {
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

	guide, err := h.DB.GetVoyageGuide(r.Context(), voyageID)

	var mapURL string
	if mapData, _ := h.DB.GetVoyageMap(r.Context(), voyageID); len(mapData) > 0 {
		mapURL = fmt.Sprintf("/api/v1/voyages/%d/map_image", voyageID)
	}

	if err != nil && mapURL == "" {
		slog.WarnContext(r.Context(), "Voyage guide not found for voyage", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusNotFound, "Voyage guide not found")
		return
	}

	if guide == nil {
		guide = &model.VoyageGuide{
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
	Voyage          *model.Voyage                `json:"voyage"`
	Guide           *model.VoyageGuide           `json:"guide"`
	Stops           []model.Stop                 `json:"stops"`
	Briefings       []model.Briefing             `json:"briefings"`
	Recommendations []model.VoyageRecommendation `json:"recommendations"`
	MapURL          string                       `json:"map_url,omitempty"`
	Debriefs        []*model.TrackDebrief        `json:"debriefs,omitempty"`
}

func (h *Handler) GetPublicVoyageGuide(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "Token required")
		return
	}

	voyage, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found or not public")
		return
	}

	guide, err := h.DB.GetVoyageGuide(r.Context(), voyage.ID)

	stops, err := h.DB.ListStops(r.Context(), voyage.ID, 0, 0)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list stops for public voyage", "voyage_id", voyage.ID, "error", err)
		stops = []model.Stop{}
	}

	briefings, err := h.DB.ListVoyageBriefings(r.Context(), voyage.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list briefings for public voyage", "voyage_id", voyage.ID, "error", err)
		briefings = []model.Briefing{}
	}

	recs, err := h.DB.ListVoyageRecommendations(r.Context(), voyage.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations for public voyage", "voyage_id", voyage.ID, "error", err)
		recs = []model.VoyageRecommendation{}
	}

	var mapURL string
	if mapData, _ := h.DB.GetVoyageMap(r.Context(), voyage.ID); len(mapData) > 0 {
		mapURL = fmt.Sprintf("/api/v1/voyages/%d/map_image", voyage.ID)
	}

	var debriefs []*model.TrackDebrief
	if tracks, err := h.DB.ListVoyageTracks(r.Context(), voyage.ID); err == nil {
		stops, _ := h.DB.ListStops(r.Context(), voyage.ID, 100, 0)
		for _, t := range tracks {
			if len(t.Debrief) > 0 && string(t.Debrief) != "null" {
				var d model.TrackDebrief
				if err := json.Unmarshal(t.Debrief, &d); err == nil {
					if d.TrackName == "" && t.Name != "" {
						d.TrackName = t.Name
					}
					if d.TrackID == "" && t.ID != "" {
						d.TrackID = t.ID
					}
					if d.VoyageStopID == nil && t.VoyageStopID != nil {
						d.VoyageStopID = t.VoyageStopID
					}
					ResolveDebriefStartStop(&d, &t, stops)
					debriefs = append(debriefs, &d)
				}
			}
		}
	}

	if guide == nil {
		guide = &model.VoyageGuide{
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
		Debriefs:        debriefs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ListVoyageBriefings(w http.ResponseWriter, r *http.Request) {
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

	briefings, err := h.DB.ListVoyageBriefings(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list briefings for voyage", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list briefings")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefings)
}
