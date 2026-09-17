package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/internal/agent"
	"app/internal/model"
	"app/internal/store"

	"google.golang.org/api/idtoken"
)

// DBStore defines the database operations required by HTTP handlers.
type DBStore interface {
	// Voyages
	ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]model.Voyage, error)
	CreateVoyage(ctx context.Context, v *model.Voyage) error
	GetVoyage(ctx context.Context, id int64) (*model.Voyage, error)
	UpdateVoyage(ctx context.Context, v *model.Voyage) error
	DeleteVoyage(ctx context.Context, id int64) error
	UpdateVoyageSharing(ctx context.Context, id int64, enable bool) (string, error)
	UpdateVoyageCheckin(ctx context.Context, id int64, lat, lng float64, location string) error
	UpdateVoyageDates(ctx context.Context, id int64, start, end *time.Time) error
	UpdateVoyageConfig(ctx context.Context, id int64, radius int, unit string) error
	GetVoyageByToken(ctx context.Context, token string) (*model.Voyage, error)

	// Stops
	ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]model.Stop, error)
	ListLandfallStops(ctx context.Context, voyageID int64) ([]model.Stop, error)
	CreateStop(ctx context.Context, s *model.Stop) error
	GetStop(ctx context.Context, id int64) (*model.Stop, error)
	GetStopByDate(ctx context.Context, voyageID int64, date time.Time) (*model.Stop, error)
	DeletePassagePoints(ctx context.Context, voyageID int64) error
	UpdateStop(ctx context.Context, s *model.Stop) error
	DeleteStop(ctx context.Context, id int64) error
	ListAllFutureStops(ctx context.Context) ([]model.Stop, error)
	ListStopsInWindow(ctx context.Context, days int) ([]model.Stop, error)

	// Briefings
	GetBriefing(ctx context.Context, stopID int64) (*model.Briefing, error)
	GetNearbyBriefing(ctx context.Context, lat, lng float64) (*model.Briefing, error)
	ListVoyageBriefings(ctx context.Context, voyageID int64) ([]model.Briefing, error)
	CreateBriefing(ctx context.Context, b *model.Briefing) error
	UpsertSafetyAlerts(ctx context.Context, stopID int64, alerts model.RawJSON) error
	UpsertWeatherBriefing(ctx context.Context, stopID int64, weather model.WeatherSummary) error

	// Guides & Maps
	GetVoyageGuide(ctx context.Context, voyageID int64) (*model.VoyageGuide, error)
	CreateVoyageGuide(ctx context.Context, g *model.VoyageGuide) error
	SaveVoyageMap(ctx context.Context, voyageID int64, data []byte) error
	GetVoyageMap(ctx context.Context, voyageID int64) ([]byte, error)

	// Recommendations
	ListVoyageRecommendations(ctx context.Context, voyageID int64) ([]model.VoyageRecommendation, error)
	CreateVoyageRecommendation(ctx context.Context, r *model.VoyageRecommendation) error
	DeleteVoyageRecommendations(ctx context.Context, voyageID int64) error

	// People & Auth
	FindPersonByGoogleID(ctx context.Context, googleID string) (*model.Person, error)
	FindPersonByEmail(ctx context.Context, email string) (*model.Person, error)
	GetPersonByID(ctx context.Context, id int64) (*model.Person, error)
	CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string, invitedBy *int64, isAdmin bool) (*model.Person, error)
	ListPeople(ctx context.Context, limit, offset int) ([]model.Person, error)
	CountPeople(ctx context.Context) (int, error)
	UpdatePersonName(ctx context.Context, id int64, name string) error
	SetAdminStatus(ctx context.Context, id int64, isAdmin bool) error

	// Invitations
	GetInvitation(ctx context.Context, email string) (*model.Invitation, error)
	CreateInvitation(ctx context.Context, email string, invitedBy int64, isAdmin bool) error
	DeleteInvitation(ctx context.Context, email string) error
	ListInvitations(ctx context.Context) ([]model.Invitation, error)

	// Sessions
	CreateSession(ctx context.Context, token string, personID int64, expiresAt time.Time) error
	GetSession(ctx context.Context, token string) (*model.Session, error)
	DeleteSession(ctx context.Context, token string) error

	// Discovery / Regions
	ListRegionsByMonth(ctx context.Context, month int) ([]model.RegionWithSeasonality, error)
	GetRegionDetails(ctx context.Context, regionID int, month int) (*model.SailingRegion, *model.RegionSeasonality, error)
	UpsertRegion(ctx context.Context, region *model.SailingRegion) error
	UpsertSeasonality(ctx context.Context, seasonality *model.RegionSeasonality) error
	DeleteSeasonalityForMonth(ctx context.Context, month int) error
	GetAllRegions(ctx context.Context) ([]model.SailingRegion, error)
	DeleteSeasonality(ctx context.Context, regionID int, month int) error

	// Tracks
	CreateVoyageTrack(ctx context.Context, t *model.VoyageTrack) error
	GetVoyageTrack(ctx context.Context, id string) (*model.VoyageTrack, error)
	ListVoyageTracks(ctx context.Context, voyageID int64) ([]model.VoyageTrack, error)
	ListStopTracks(ctx context.Context, stopID int64) ([]model.VoyageTrack, error)
	DeleteVoyageTrack(ctx context.Context, id string) error
	UpdateVoyageTrackDebrief(ctx context.Context, id string, debrief model.RawJSON) error
}

var _ DBStore = (*store.DB)(nil)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	DB           DBStore
	ContentDir   string
	AgentURL     string
	AgentClient  *http.Client
	HealthClient *http.Client
	Agent        *agent.AgentRunner
	Resolver     agent.Resolver
	ResearchSem  chan struct{}

	muHealth        sync.RWMutex
	lastHealthCheck time.Time
	lastHealthErr   error

	muRecStreams sync.RWMutex
	recStreams   map[string]chan model.VoyageRecommendation

	muProgressStreams sync.RWMutex
	progressStreams   map[string]chan model.ProgressEvent

	muActiveJobs sync.Mutex
	activeJobs   map[string]struct{}
}

// New creates a new Handler with the given dependencies.
func New(db DBStore, contentDir string, agentURL string, resolver agent.Resolver) *Handler {
	var client *http.Client
	var err error

	if agentURL != "" && strings.Contains(agentURL, "run.app") {
		client, err = idtoken.NewClient(context.Background(), agentURL)
		if err != nil {
			slog.Error("Failed to create authenticated agent client", "error", err)
			client = &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 20,
				},
			}
		} else {
			client.Timeout = 0
			if transport, ok := client.Transport.(*http.Transport); ok {
				transport.MaxIdleConns = 100
				transport.MaxIdleConnsPerHost = 20
			}
		}
	} else {
		client = &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		}
	}

	if resolver == nil {
		resolver = &agent.StaticResolver{BaseURL: agentURL}
	}

	agentRunner := &agent.AgentRunner{Client: client, Resolver: resolver}

	reRunner, err := agent.NewReasoningEngineRunner(context.Background())
	if err != nil {
		slog.Warn("Failed to initialize Reasoning Engine runner", "error", err)
	} else {
		agentRunner.ReasoningEngine = reRunner
	}

	healthClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		},
	}

	return &Handler{
		DB:              db,
		ContentDir:      contentDir,
		AgentURL:        agentURL,
		AgentClient:     client,
		HealthClient:    healthClient,
		Agent:           agentRunner,
		Resolver:        resolver,
		ResearchSem:     make(chan struct{}, 10),
		recStreams:      make(map[string]chan model.VoyageRecommendation),
		progressStreams: make(map[string]chan model.ProgressEvent),
		activeJobs:      make(map[string]struct{}),
	}
}

func (h *Handler) tryClaimJob(key string) bool {
	h.muActiveJobs.Lock()
	defer h.muActiveJobs.Unlock()
	if _, ok := h.activeJobs[key]; ok {
		return false
	}
	h.activeJobs[key] = struct{}{}
	return true
}

func (h *Handler) releaseJob(key string) {
	h.muActiveJobs.Lock()
	defer h.muActiveJobs.Unlock()
	delete(h.activeJobs, key)
}

func (h *Handler) CheckAgentHealth(ctx context.Context) error {
	if h.AgentURL == "" {
		return nil
	}

	if strings.HasPrefix(h.AgentURL, "projects/") && strings.Contains(h.AgentURL, "/reasoningEngines/") {
		return nil
	}

	h.muHealth.RLock()
	if time.Since(h.lastHealthCheck) < 10*time.Second {
		err := h.lastHealthErr
		h.muHealth.RUnlock()
		return err
	}
	h.muHealth.RUnlock()

	h.muHealth.Lock()
	defer h.muHealth.Unlock()

	if time.Since(h.lastHealthCheck) < 10*time.Second {
		return h.lastHealthErr
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := h.AgentURL + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		h.lastHealthCheck = time.Now()
		h.lastHealthErr = err
		return err
	}

	client := h.HealthClient
	if client == nil {
		client = h.AgentClient
	}

	resp, err := client.Do(req)
	if err != nil {
		h.lastHealthCheck = time.Now()
		h.lastHealthErr = err
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("agent returned non-200 status: %d", resp.StatusCode)
		h.lastHealthCheck = time.Now()
		h.lastHealthErr = err
		return err
	}

	h.lastHealthCheck = time.Now()
	h.lastHealthErr = nil
	return nil
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	start := -1
	if startObj != -1 && (startArr == -1 || startObj < startArr) {
		start = startObj
	} else {
		start = startArr
	}

	endObj := strings.LastIndex(s, "}")
	endArr := strings.LastIndex(s, "]")

	end := -1
	if endObj != -1 && (endArr == -1 || endObj > endArr) {
		end = endObj
	} else {
		end = endArr
	}

	if start != -1 && end != -1 && end > start {
		s = s[start : end+1]
	} else {
		return ""
	}

	s = strings.ReplaceAll(s, `\'`, `'`)
	return strings.TrimSpace(s)
}

func repairTruncatedJSONArray(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if json.Valid([]byte(s)) {
		return s
	}

	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	if startObj == -1 && startArr == -1 {
		return s
	}

	isObjRoot := startObj != -1 && (startArr == -1 || startObj < startArr)
	start := startObj
	if !isObjRoot {
		start = startArr
	}

	// Scan backwards from the end for each '}' to find the last valid closure
	for i := len(s) - 1; i >= start; i-- {
		if s[i] == '}' {
			sub := s[start : i+1]
			if isObjRoot {
				// Try closing array and object wrapper: ...]}
				candidate := sub + "\n]}"
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				// Try closing object only: ...}
				candidateObj := sub + "\n}"
				if json.Valid([]byte(candidateObj)) {
					return candidateObj
				}
			} else {
				// Array root: ...]
				candidate := sub + "\n]"
				if json.Valid([]byte(candidate)) {
					return candidate
				}
			}
		}
	}

	return s
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
