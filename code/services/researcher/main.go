// Package main is the entry point for the researcher service, orchestrating multiple AI agents
// (Researcher, Guide, Discovery) to assist with sailing voyage planning.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/logging"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

//go:embed prompts/search_specialist.md
var _searchSpecialistPrompt string

//go:embed prompts/harbourmaster.md
var _harbourmasterPrompt string

//go:embed prompts/pilot.md
var _pilotPrompt string

//go:embed prompts/commodore.md
var _commodorePrompt string

//go:embed prompts/specialist.md
var _specialistPrompt string

const maxOutputTokens = 65536

type Provider interface {
	Close() error
}

type autoCreateSessionService struct {
	session.Service
}

func (s *autoCreateSessionService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	resp, err := s.Service.Get(ctx, req)
	if err != nil {
		slog.Debug("Session not found, auto-creating", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)
		createResp, createErr := s.Service.Create(ctx, &session.CreateRequest{
			AppName:   req.AppName,
			UserID:    req.UserID,
			SessionID: req.SessionID,
		})
		if createErr != nil {
			// If creation failed, maybe it was created by another request in the meantime?
			if resp2, err2 := s.Service.Get(ctx, req); err2 == nil {
				return resp2, nil
			}
			return nil, createErr
		}
		return &session.GetResponse{Session: createResp.Session}, nil
	}
	return resp, nil
}

type Server struct {
	config *config.Config
	mu     sync.Mutex
	// timings stores the start time of tool executions, keyed by function call ID.
	timings map[string]time.Time

	providers []Provider

	// Telemetry streaming
	muClients sync.RWMutex
	clients   map[chan TelemetryEvent]bool
}

type TelemetryEvent struct {
	SessionID string `json:"session_id,omitempty"`
	Event     string `json:"event"` // "tool_start", "tool_end"
	Tool      string `json:"tool"`
	Duration  string `json:"duration,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

func (s *Server) broadcast(event TelemetryEvent) {
	s.muClients.RLock()
	defer s.muClients.RUnlock()
	for client := range s.clients {
		select {
		case client <- event:
		default:
			// Client slow, skip or drop
		}
	}
}

func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	messageChan := make(chan TelemetryEvent, 10)
	s.muClients.Lock()
	if s.clients == nil {
		s.clients = make(map[chan TelemetryEvent]bool)
	}
	s.clients[messageChan] = true
	s.muClients.Unlock()

	defer func() {
		s.muClients.Lock()
		delete(s.clients, messageChan)
		close(messageChan)
		s.muClients.Unlock()
	}()

	// Heartbeat
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n")
			f.Flush()
		case event := <-messageChan:
			jsonData, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonData)
			f.Flush()
		}
	}
}

func (s *Server) Close() {
	var failed int
	for _, p := range s.providers {
		if err := p.Close(); err != nil {
			slog.Error("Failed to close provider", "error", err)
			failed++
		}
	}
	if failed > 0 {
		slog.Warn("Some providers failed to close cleanly", "count", failed)
	}
}

func main() {
	// Load .env file (try current dir, then project root)
	godotenv.Load(".env")
	godotenv.Load("../../.env")

	cfg, err := config.New(os.Getenv)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logging.InitLogging(cfg.Env)

	slog.Info("config", "modelName", cfg.ModelName)
	slog.Info("config", "port", cfg.Port)
	if len(cfg.MapsAPIKey) > 5 {
		slog.Info("config", "MapsAPIKey", cfg.MapsAPIKey[:5]+"...")
	}

	ctx := context.Background()
	srv := &Server{
		config:  cfg,
		timings: make(map[string]time.Time),
	}
	defer srv.Close()

	if err := srv.run(ctx); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

func (s *Server) run(ctx context.Context) error {
	// Validate embedded prompts before attempting agent creation so that an
	// accidentally empty file fails fast with a clear message.
	prompts := map[string]string{
		"harbourmaster":     _harbourmasterPrompt,
		"pilot":             _pilotPrompt,
		"commodore":         _commodorePrompt,
		"specialist":        _specialistPrompt,
		"search_specialist": _searchSpecialistPrompt,
	}
	for name, p := range prompts {
		if len(strings.TrimSpace(p)) == 0 {
			return fmt.Errorf("embedded prompt %q is empty — check prompts/ directory", name)
		}
	}

	// Use a bounded context for agent/model initialisation. If the Gemini API
	// is unresponsive during startup we fail fast rather than hanging forever.
	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	defer initCancel()

	researcherTools, err := s.setupTools(initCtx)
	if err != nil {
		return fmt.Errorf("setting up tools: %w", err)
	}

	pilotAgent, err := s.createPilotAgent(initCtx, researcherTools)
	if err != nil {
		return fmt.Errorf("creating pilot agent: %w", err)
	}

	harbourmasterAgent, err := s.createHarbourmasterAgent(initCtx, researcherTools)
	if err != nil {
		return fmt.Errorf("creating harbourmaster agent: %w", err)
	}

	commodoreAgent, err := s.createCommodoreAgent(initCtx)
	if err != nil {
		return fmt.Errorf("creating commodore agent: %w", err)
	}

	specialistAgent, err := s.createSpecialistAgent(initCtx, researcherTools)
	if err != nil {
		return fmt.Errorf("creating specialist agent: %w", err)
	}

	loader, err := agent.NewMultiLoader(harbourmasterAgent, pilotAgent, commodoreAgent, specialistAgent)
	if err != nil {
		return fmt.Errorf("creating multi loader: %w", err)
	}

	config := &launcher.Config{
		AgentLoader:    loader,
		SessionService: &autoCreateSessionService{session.InMemoryService()},
	}

	// Start Custom Server
	mux := http.NewServeMux()

	// Expose to a2a for eval purposes
	s.registerAgentA2A(mux, harbourmasterAgent, "/invoke", config.SessionService)
	s.registerAgentA2A(mux, harbourmasterAgent, "/invoke/harbourmaster", config.SessionService)
	s.registerAgentA2A(mux, pilotAgent, "/invoke/pilot", config.SessionService)
	s.registerAgentA2A(mux, commodoreAgent, "/invoke/commodore", config.SessionService)
	s.registerAgentA2A(mux, specialistAgent, "/invoke/specialist", config.SessionService)

	// Special case: The root Agent Card at .well-known usually points to the main agent.
	// We'll point it to harbourmasterAgent for now.
	agentCard := s.buildAgentCard(harbourmasterAgent, "/invoke")
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create the ADK HTTP Server
	adkHandler, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     config.AgentLoader,
		SessionService:  config.SessionService,
		SSEWriteTimeout: 300 * time.Second,
		DebugConfig:     &adkrest.DebugTelemetryConfig{},
	})
	if err != nil {
		log.Fatalf("Failed to create ADK server: %v", err)
	}

	// Mount ADK under /api/
	mux.Handle("/api/", http.StripPrefix("/api", adkHandler))

	// Telemetry endpoint — streams internal tool execution events.
	// In production this is protected by Cloud Run IAM; no additional
	// application-level auth is enforced here.
	mux.HandleFunc("/telemetry", s.handleTelemetry)

	httpSrv := &http.Server{
		Addr:    ":" + s.config.Port,
		Handler: traceMiddleware(s.config.Project, loggingMiddleware(mux)),
	}

	// Listen for SIGTERM/SIGINT so Cloud Run scale-down drains in-flight requests.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		slog.Info("Shutdown signal received, draining requests...")
		// Allow up to 5 minutes for in-flight LLM calls to finish.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
	}()

	slog.Info("Starting custom server", "port", s.config.Port)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type agentConfig struct {
	name        string
	description string
	instruction string
	tools       []tool.Tool
	temperature float32
}

func (s *Server) createAgent(ctx context.Context, acfg *agentConfig) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](acfg.temperature),
	}

	m, err := gemini.NewModel(ctx, s.config.ModelName, &genai.ClientConfig{
		APIKey: s.config.GeminiAPIKey,
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:                  acfg.name,
		Model:                 m,
		Description:           acfg.description,
		Instruction:           acfg.instruction,
		Tools:                 acfg.tools,
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func (s *Server) registerAgentA2A(mux *http.ServeMux, a agent.Agent, path string, sessionService session.Service) {
	card := s.buildAgentCard(a, path)
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        a.Name(),
			Agent:          a,
			SessionService: sessionService,
		},
	})
	handler := a2asrv.NewHandler(executor)

	mux.Handle(path, a2asrv.NewJSONRPCHandler(handler))
	mux.Handle(path+"/agent-card.json", a2asrv.NewStaticAgentCardHandler(card))
	slog.Info("Registered A2A agent", "name", a.Name(), "path", path)
}

func (s *Server) buildAgentCard(a agent.Agent, path string) *a2a.AgentCard {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:" + s.config.Port
	}
	return &a2a.AgentCard{
		Name:               a.Name(),
		Skills:             adka2a.BuildAgentSkills(a),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		URL:                baseURL + path,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{},
		DefaultOutputModes: []string{},
	}
}

func (s *Server) setupTools(ctx context.Context) ([]tool.Tool, error) {
	weatherTool, wp, err := tools.NewWeatherTool()
	if err != nil {
		return nil, err
	}
	s.providers = append(s.providers, wp)

	tideTool, tp, err := tools.NewTideTool()
	if err != nil {
		return nil, err
	}
	s.providers = append(s.providers, tp)

	sunriseTool, sp, err := tools.NewSunriseTool(s.config.MapsAPIKey)
	if err != nil {
		return nil, err
	}
	s.providers = append(s.providers, sp)

	placesTool, pp, err := tools.NewPlacesTool(ctx, s.config.MapsAPIKey)
	if err != nil {
		return nil, err
	}
	s.providers = append(s.providers, pp)

	return []tool.Tool{weatherTool, tideTool, sunriseTool, placesTool}, nil
}

func (s *Server) createPilotAgent(ctx context.Context, researcherTools []tool.Tool) (agent.Agent, error) {
	searchTools, err := s.createSearchTools(ctx, "pilot_search_specialist")
	if err != nil {
		return nil, err
	}
	allTools := append(researcherTools, searchTools...)

	return s.createAgent(ctx, &agentConfig{
		name:        "pilot",
		description: "A Local Knowledge Expert and Sailing Guide.",
		instruction: _pilotPrompt,
		tools:       allTools,
		temperature: 0.25,
	})
}

func (s *Server) createHarbourmasterAgent(ctx context.Context, researcherTools []tool.Tool) (agent.Agent, error) {
	searchTools, err := s.createSearchTools(ctx, "harbourmaster_search_specialist")
	if err != nil {
		return nil, err
	}
	allTools := append(researcherTools, searchTools...)

	return s.createAgent(ctx, &agentConfig{
		name:        "harbourmaster",
		description: "A Virtual Harbourmaster that researches sailing destinations.",
		instruction: _harbourmasterPrompt,
		tools:       allTools,
		temperature: 0.25,
	})
}

func (s *Server) createCommodoreAgent(ctx context.Context) (agent.Agent, error) {
	searchTools, err := s.createSearchTools(ctx, "commodore_search_specialist")
	if err != nil {
		return nil, err
	}

	return s.createAgent(ctx, &agentConfig{
		name:        "commodore",
		description: "The Commodore - Global Seasonal Discovery Expert.",
		instruction: _commodorePrompt,
		tools:       searchTools,
		temperature: 0.25,
	})
}

func (s *Server) createSearchTools(ctx context.Context, name string) ([]tool.Tool, error) {
	searchAgent, err := s.createAgent(ctx, &agentConfig{
		name:        name,
		description: "Finds information on the web using Google Search.",
		instruction: _searchSpecialistPrompt,
		tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		temperature: 0.4,
	})
	if err != nil {
		return nil, err
	}

	individualTool := agenttool.New(searchAgent, nil)

	// Create the batch tool that uses the search agent in parallel
	batchTool := &tools.BatchSearchTool{
		Searcher: func(ctx context.Context, query string) (string, error) {
			// Create a runner for the search agent
			r, err := runner.New(runner.Config{
				AppName:        name,
				Agent:          searchAgent,
				SessionService: session.InMemoryService(), // Temporary session for the sub-search
			})

			if err != nil {
				return "", err
			}

			resp := r.Run(ctx, "system", "batch_"+fmt.Sprint(time.Now().UnixNano()), genai.NewContentFromText(query, "user"), agent.RunConfig{})

			var text string
			for event, err := range resp {
				if err != nil {
					slog.Error("Search agent stream error", "query", query, "error", err)
					continue
				}
				if event.Content != nil && event.Content.Role == "model" {
					for _, part := range event.Content.Parts {
						if part.Text != "" && !part.Thought {
							text += part.Text
						}
					}
				}
			}

			if text == "" {
				slog.Warn("Search agent returned empty result", "query", query)
			} else {
				slog.Debug("Search agent result", "query", query, "text_len", len(text))
			}

			return text, nil
		},
	}

	return []tool.Tool{individualTool, batchTool}, nil
}

func (s *Server) createSpecialistAgent(ctx context.Context, researcherTools []tool.Tool) (agent.Agent, error) {
	searchTools, err := s.createSearchTools(ctx, "specialist_search_specialist")
	if err != nil {
		return nil, err
	}
	allTools := append(researcherTools, searchTools...)

	return s.createAgent(ctx, &agentConfig{
		name:        "specialist",
		description: "A Local Pilot and Navigation Specialist.",
		instruction: _specialistPrompt,
		tools:       allTools,
		temperature: 0.25,
	})
}

func (s *Server) onBeforeTool(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timings[ctx.FunctionCallID()] = time.Now()

	argBytes, _ := json.Marshal(args)
	slog.Info("tool_start", "tool", t.Name(), "args", string(argBytes))

	s.broadcast(TelemetryEvent{
		SessionID: ctx.SessionID(),
		Event:     "tool_start",
		Tool:      t.Name(),
		Timestamp: time.Now().UnixMilli(),
	})
	return nil, nil
}

func (s *Server) onAfterTool(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
	s.mu.Lock()
	startTime, ok := s.timings[ctx.FunctionCallID()]
	if ok {
		delete(s.timings, ctx.FunctionCallID())
	}
	s.mu.Unlock()

	var duration string
	if ok {
		timesince := time.Since(startTime)
		duration = timesince.String()

		status := "success"
		if err != nil {
			status = "error"
		}
		slog.Info("tool_end", "tool", t.Name(), "duration", duration, "status", status)
	}

	s.broadcast(TelemetryEvent{
		SessionID: ctx.SessionID(),
		Event:     "tool_end",
		Tool:      t.Name(),
		Duration:  duration,
		Timestamp: time.Now().UnixMilli(),
	})

	return result, nil
}

var _ http.ResponseWriter = (*responseWriter)(nil)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			timesince := time.Since(start)
			str := timesince.String()

			level := slog.LevelInfo
			if ww.statusCode >= 400 {
				level = slog.LevelWarn
			}
			if ww.statusCode >= 500 {
				level = slog.LevelError
			}

			slog.Log(r.Context(), level, "Request handled",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.statusCode,
				"duration", str,
				"remote_addr", r.RemoteAddr,
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

// traceMiddleware exists to make sure when running on Cloud Run, trace
// ids are propegated so that you can get debugging and analysis.
func traceMiddleware(projectID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceHeader := r.Header.Get("X-Cloud-Trace-Context")
		traceParts := strings.Split(traceHeader, "/")
		if len(traceParts) > 0 && len(traceParts[0]) > 0 {
			traceID := traceParts[0]
			var trace string
			if projectID != "" {
				trace = fmt.Sprintf("projects/%s/traces/%s", projectID, traceID)
			} else {
				trace = traceID
			}
			ctx := logging.AddTraceToContext(r.Context(), trace)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r)
	})
}
