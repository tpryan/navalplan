package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/logging"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

//go:embed prompts/search_specialist.md
var searchSpecialistPrompt string

//go:embed prompts/stop_agent.md
var stopAgentPrompt string

//go:embed prompts/voyage_agent.md
var voyageAgentPrompt string

//go:embed prompts/discovery_agent.md
var discoveryAgentPrompt string

const maxOutputTokens = 65536

type Provider interface {
	Close() error
}

type Server struct {
	config  *config.Config
	timings sync.Map

	providers []Provider
}

func (s *Server) Close() {
	for _, p := range s.providers {
		if err := p.Close(); err != nil {
			slog.Error("Failed to close provider", "error", err)
		}
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
	slog.Info("config", "MapsAPIKey", cfg.MapsAPIKey[:5]+"...")

	ctx := context.Background()
	if err := run(ctx, cfg); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	srv := &Server{
		config:  cfg,
		timings: sync.Map{},
	}
	defer srv.Close()

	voyageAgent, err := srv.createVoyageAgent(ctx)
	if err != nil {
		return fmt.Errorf("creating guide agent: %w", err)
	}

	stopAgent, err := srv.createStopAgent(ctx)
	if err != nil {
		return fmt.Errorf("creating researcher agent: %w", err)
	}

	discoveryAgent, err := srv.createDiscoveryAgent(ctx)
	if err != nil {
		return fmt.Errorf("creating discovery agent: %w", err)
	}

	loader, err := agent.NewMultiLoader(stopAgent, voyageAgent, discoveryAgent)
	if err != nil {
		return fmt.Errorf("creating multi loader: %w", err)
	}

	config := &launcher.Config{
		AgentLoader:    loader,
		SessionService: session.InMemoryService(),
	}

	// Create the ADK HTTP Handler
	adkHandler := adkrest.NewHandler(config, 120*time.Second)

	// Start Custom Server
	mux := http.NewServeMux()

	// Mount ADK under /api/
	mux.Handle("/api/", http.StripPrefix("/api", adkHandler))

	slog.Info("Starting custom server", "port", cfg.Port)
	// Apply Trace Middleware then Logging Middleware
	return http.ListenAndServe(":"+cfg.Port, traceMiddleware(cfg.Project, loggingMiddleware(mux)))
}

func (s *Server) createVoyageAgent(ctx context.Context) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](0.4),
	}

	m, err := gemini.NewModel(ctx, s.config.ModelName, &genai.ClientConfig{
		APIKey: s.config.GeminiAPIKey,
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "guide_agent",
		Model:       m,
		Description: "A Local Knowledge Expert and Sailing Guide.",
		Instruction: voyageAgentPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func (s *Server) createStopAgent(ctx context.Context) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](0.4),
	}

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

	placesTool, pp, err := tools.NewPlacesTool(s.config.MapsAPIKey)
	if err != nil {
		return nil, err
	}
	s.providers = append(s.providers, pp)

	// Create a dedicated model instance
	m, err := gemini.NewModel(ctx, s.config.ModelName, &genai.ClientConfig{
		APIKey: s.config.GeminiAPIKey,
	})
	if err != nil {
		return nil, err
	}

	// 2. Define Sub-Agent (Search Specialist)
	searchAgent, err := llmagent.New(llmagent.Config{
		Name:        "search_specialist",
		Model:       m,
		Description: "Finds information on the web (facilities, reviews).",
		Instruction: searchSpecialistPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		GenerateContentConfig: genConfig,
	})
	if err != nil {
		return nil, err
	}

	// 3. Define Parent Agent (Researcher / Orchestrator)
	return llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       m,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: stopAgentPrompt,
		Tools: []tool.Tool{
			weatherTool,
			tideTool,
			sunriseTool,
			placesTool,
			agenttool.New(searchAgent, nil),
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func (s *Server) createDiscoveryAgent(ctx context.Context) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](0.2), // Lower temperature for more consistent JSON
	}

	m, err := gemini.NewModel(ctx, s.config.ModelName, &genai.ClientConfig{
		APIKey: s.config.GeminiAPIKey,
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "discovery_agent",
		Model:       m,
		Description: "The Commodore - Global Seasonal Discovery Expert.",
		Instruction: discoveryAgentPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func (s *Server) onBeforeTool(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	s.timings.Store(ctx.FunctionCallID(), time.Now())
	return nil, nil
}

func (s *Server) onAfterTool(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
	if startTime, ok := s.timings.LoadAndDelete(ctx.FunctionCallID()); ok {
		timesince := time.Since(startTime.(time.Time))
		str := timesince.String()
		slog.Debug("tool execution", "tool", t.Name(), "duration", str)
	}
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
