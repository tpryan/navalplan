package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

var (
	timeWarn            = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	timeUrgentWarn      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	thresholdWarn       = time.Second * 5
	thresholdUrgentWarn = time.Second * 30
)

//go:embed prompts/search_specialist.md
var searchSpecialistPrompt string

//go:embed prompts/researcher_agent.md
var researcherAgentPrompt string

//go:embed prompts/guide_agent.md
var guideAgentPrompt string

//go:embed prompts/discovery_agent.md
var discoveryAgentPrompt string

type Server struct {
	model   model.LLM
	timings sync.Map
}

func main() {
	// Configure charmbracelet/log
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	log.SetPrefix("agent")

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Load .env
	godotenv.Load("../../.env")

	mapsKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if mapsKey != "" {
		if len(mapsKey) > 5 {
			log.Info("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", mapsKey[:5]+"...")
		} else {
			log.Info("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", "SET (short)")
		}
	} else {
		log.Warn("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", "NOT SET")
	}

	ctx := context.Background()

	// 1. Initialize Gemini Model
	modelName := os.Getenv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.0-flash-001"
	}

	log.Info("config", "modelName", modelName)

	geminiModel, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		return fmt.Errorf("creating model: %w", err)
	}

	srv := &Server{
		model:   geminiModel,
		timings: sync.Map{},
	}

	researchAgent, err := srv.createResearcherAgent()
	if err != nil {
		return fmt.Errorf("creating researcher agent: %w", err)
	}

	guideAgent, err := srv.createGuideAgent()
	if err != nil {
		return fmt.Errorf("creating guide agent: %w", err)
	}

	discoveryAgent, err := srv.createDiscoveryAgent()
	if err != nil {
		return fmt.Errorf("creating discovery agent: %w", err)
	}

	// 4. Launch the Server
	loader, err := agent.NewMultiLoader(researchAgent, guideAgent, discoveryAgent)
	if err != nil {
		return fmt.Errorf("creating multi loader: %w", err)
	}

	config := &launcher.Config{
		AgentLoader:    loader,
		SessionService: session.InMemoryService(),
	}

	// Port handling
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("NAVALPLAN_AGENT_PORT")
	}
	if port == "" {
		port = "8081" // Default fallback
	}

	// Create the ADK HTTP Handler
	adkHandler := adkrest.NewHandler(config, 120*time.Second)

	// Start Custom Server
	mux := http.NewServeMux()

	// Mount ADK under /api/
	mux.Handle("/api/", http.StripPrefix("/api", adkHandler))

	log.Info("Starting custom server", "port", port)
	return http.ListenAndServe(":"+port, loggingMiddleware(mux))
}

func (s *Server) createResearcherAgent() (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.4),
	}

	weatherTool, err := tools.NewWeatherTool()
	if err != nil {
		return nil, err
	}

	tideTool, err := tools.NewTideTool()
	if err != nil {
		return nil, err
	}

	sunriseTool, err := tools.NewSunriseTool()
	if err != nil {
		return nil, err
	}

	placesTool, err := tools.NewPlacesTool()
	if err != nil {
		return nil, err
	}

	// 2. Define Sub-Agent (Search Specialist)
	searchAgent, err := llmagent.New(llmagent.Config{
		Name:        "search_specialist",
		Model:       s.model,
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
		Model:       s.model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: researcherAgentPrompt,
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

func (s *Server) createGuideAgent() (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.4),
	}

	return llmagent.New(llmagent.Config{
		Name:        "guide_agent",
		Model:       s.model,
		Description: "A Local Knowledge Expert and Sailing Guide.",
		Instruction: guideAgentPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func (s *Server) createDiscoveryAgent() (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.2), // Lower temperature for more consistent JSON
	}

	return llmagent.New(llmagent.Config{
		Name:        "discovery_agent",
		Model:       s.model,
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

		switch {
		case timesince > thresholdUrgentWarn:
			str = timeUrgentWarn.Render(str)
		case timesince > thresholdWarn:
			str = timeWarn.Render(str)

		}

		log.Debug(fmt.Sprintf("tool:%s  %s", t.Name(), str))
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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		defer func() {
			timesince := time.Since(start)
			str := timesince.String()

			switch {
			case timesince > time.Second*2:
				str = timeUrgentWarn.Render(str)
			case timesince > time.Millisecond*100:
				str = timeWarn.Render(str)

			}

			log.Info(fmt.Sprintf("%s %s %s %d %s", r.Method, r.URL.Path, r.RemoteAddr, ww.statusCode, str))
		}()

		next.ServeHTTP(ww, r)
	})
}
