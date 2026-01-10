package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	clog "github.com/charmbracelet/log"
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

var timeWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
var timeUrgentWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))

var thresholdWarn = time.Second * 5
var thresholdUrgentWarn = time.Second * 30

//go:embed prompts/search_specialist.md
var searchSpecialistPrompt string

//go:embed prompts/researcher_agent.md
var researcherAgentPrompt string

//go:embed prompts/guide_agent.md
var guideAgentPrompt string

//go:embed prompts/discovery_agent.md
var discoveryAgentPrompt string

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
		next.ServeHTTP(ww, r)

		timesince := time.Since(start)
		str := timesince.String()

		switch {
		case timesince > time.Second*2:
			str = timeUrgentWarn.Render(str)
		case timesince > time.Millisecond*100:
			str = timeWarn.Render(str)

		}

		clog.Info(fmt.Sprintf("%s %s %s %d %s", r.Method, r.URL.Path, r.RemoteAddr, ww.statusCode, str))
	})
}

func main() {
	// Configure charmbracelet/log
	clog.SetOutput(os.Stdout)
	clog.SetLevel(clog.DebugLevel)
	clog.SetPrefix("agent")

	// Redirect standard log to charmbracelet/log
	stdLog := clog.StandardLog()
	log.SetOutput(stdLog.Writer())
	log.SetFlags(0)

	// Load .env
	godotenv.Load("../../.env")

	mapsKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if mapsKey != "" {
		if len(mapsKey) > 5 {
			clog.Info("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", mapsKey[:5]+"...")
		} else {
			clog.Info("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", "SET (short)")
		}
	} else {
		clog.Warn("config", "NAVALPLAN_BACKEND_MAPS_API_KEY", "NOT SET")
	}

	ctx := context.Background()

	// 1. Initialize Gemini Model
	// We use gemini-2.0-flash-001 as it is the current stable flash model, unless overridden by env var
	modelName := os.Getenv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.0-flash-001"
	}

	clog.Info("config", "modelName", modelName)

	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		clog.Fatalf("Failed to create model: %v", err)
	}

	researchAgent, err := CreateResearcherAgent(model)
	if err != nil {
		clog.Fatalf("Failed to create researcher agent: %v", err)
	}

	guideAgent, err := CreateGuideAgent(model)
	if err != nil {
		clog.Fatalf("Failed to create guide agent: %v", err)
	}

	discoveryAgent, err := CreateDiscoveryAgent(model)
	if err != nil {
		clog.Fatalf("Failed to create discovery agent: %v", err)
	}

	// 4. Launch the Server
	loader, err := agent.NewMultiLoader(researchAgent, guideAgent, discoveryAgent)
	if err != nil {
		clog.Fatalf("Failed to create multi loader: %v", err)
	}

	config := &launcher.Config{
		AgentLoader:    loader,
		SessionService: session.InMemoryService(),
	}

	// Recovery for main process
	defer func() {
		if r := recover(); r != nil {
			clog.Printf("Recovered from panic in main: %v", r)
		}
	}()

	// Port handling for Cloud Run compatibility
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

	clog.Info("Starting custom server", "port", port)
	if err := http.ListenAndServe(":"+port, loggingMiddleware(mux)); err != nil {
		clog.Fatalf("Server failed: %v", err)
	}
}

func CreateResearcherAgent(model model.LLM) (agent.Agent, error) {
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
		Model:       model,
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
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: researcherAgentPrompt,
		Tools: []tool.Tool{
			weatherTool,
			tideTool,
			sunriseTool,
			placesTool,
			agenttool.New(searchAgent, nil),
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func CreateGuideAgent(model model.LLM) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.4),
	}

	return llmagent.New(llmagent.Config{
		Name:        "guide_agent",
		Model:       model,
		Description: "A Local Knowledge Expert and Sailing Guide.",
		Instruction: guideAgentPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func CreateDiscoveryAgent(model model.LLM) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.2), // Lower temperature for more consistent JSON
	}

	return llmagent.New(llmagent.Config{
		Name:        "discovery_agent",
		Model:       model,
		Description: "The Commodore - Global Seasonal Discovery Expert.",
		Instruction: discoveryAgentPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{onAfterTool},
		GenerateContentConfig: genConfig,
	})
}

func ObscureString(input, toObscure string) string {
	// The number of runes (characters) in the input determines the length of the output.
	str := strings.Repeat("*", len(toObscure))
	return strings.ReplaceAll(input, toObscure, str)

}

var toolTimings sync.Map

func onBeforeTool(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	toolTimings.Store(ctx.FunctionCallID(), time.Now())
	return nil, nil
}

func onAfterTool(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
	if startTime, ok := toolTimings.LoadAndDelete(ctx.FunctionCallID()); ok {
		timesince := time.Since(startTime.(time.Time))
		str := timesince.String()

		switch {
		case timesince > thresholdUrgentWarn:
			str = timeUrgentWarn.Render(str)
		case timesince > thresholdWarn:
			str = timeWarn.Render(str)

		}

		clog.Info(fmt.Sprintf("tool:%s  %s", t.Name(), str))
	}
	return result, nil
}
