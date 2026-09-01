package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// HTTPDoer abstracts HTTP requests for testing.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// SailingDirectionsArgs defines input parameters for the query_sailing_directions tool.
type SailingDirectionsArgs struct {
	Query     string `json:"query" description:"Specific navigational question, channel depth, bridge clearance, harbor name, or passage leg."`
	Territory string `json:"territory,omitempty" description:"Scope of pilot data: 'us' for NOAA Coast Pilot, 'international' for NGA Sailing Directions, 'all' for both (default)."`
	TopK      int    `json:"top_k,omitempty" description:"Number of context chunks to retrieve (default: 5, max: 10)."`
}

// CoastPilotArgs defines input parameters for the query_coast_pilot tool.
type CoastPilotArgs struct {
	Query string `json:"query" description:"Specific question for US coastal waters, channels, or harbors."`
	TopK  int    `json:"top_k,omitempty" description:"Number of context chunks to retrieve (default: 5, max: 10)."`
}

// SailingDirectionsResult defines the output of the sailing directions and coast pilot tools.
type SailingDirectionsResult struct {
	Query           string `json:"query"`
	Territory       string `json:"territory"`
	Contexts        string `json:"contexts"`
	DebugDurationMS int64  `json:"debug_duration_ms"`
}

// SailingDirectionsProvider handles querying Vertex AI RAG corpora for hydrographic pilot data.
type SailingDirectionsProvider struct {
	projectID        string
	location         string
	coastPilotCorpus string
	ngaCorpus        string
	client           HTTPDoer
}

func (p *SailingDirectionsProvider) Close() error {
	return nil
}

// NewSailingDirectionsTool creates the ADK function tool for query_sailing_directions.
func NewSailingDirectionsTool(projectID, location, coastPilotCorpus, ngaCorpus string) (tool.Tool, *SailingDirectionsProvider, error) {
	var client HTTPDoer = http.DefaultClient
	if projectID != "" {
		googleClient, err := google.DefaultClient(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
		if err == nil {
			client = googleClient
		}
	}
	if location == "" {
		location = "us-central1"
	}
	provider := &SailingDirectionsProvider{
		projectID:        projectID,
		location:         location,
		coastPilotCorpus: coastPilotCorpus,
		ngaCorpus:        ngaCorpus,
		client:           client,
	}
	t, err := functiontool.New(functiontool.Config{
		Name:        "query_sailing_directions",
		Description: "Searches official hydrographic pilot books (NOAA Coast Pilot for US waters and NGA Sailing Directions for international/foreign waters). Returns authoritative channel depths, bridge vertical clearances, tidal rips, buoyage, hazards, and port regulations.",
	}, provider.QuerySailingDirections)
	return t, provider, err
}

// NewCoastPilotTool creates a dedicated ADK function tool for query_coast_pilot.
func NewCoastPilotTool(provider *SailingDirectionsProvider) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "query_coast_pilot",
		Description: "Searches NOAA Coast Pilot (Volumes 1-10) for US coastal waters, channels, bridge clearances, anchorages, and hazards.",
	}, provider.QueryCoastPilot)
}

type ragResource struct {
	RagCorpus string `json:"ragCorpus"`
}

type retrieveContextsRequest struct {
	VertexRagStore struct {
		RagResources []ragResource `json:"ragResources"`
	} `json:"vertexRagStore"`
	Query struct {
		Text string `json:"text"`
	} `json:"query"`
}

type retrieveContextsResponse struct {
	Contexts struct {
		Contexts []struct {
			SourceURI         string  `json:"sourceUri"`
			SourceDisplayName string  `json:"sourceDisplayName"`
			Text              string  `json:"text"`
			Score             float64 `json:"score,omitempty"`
			Distance          float64 `json:"distance,omitempty"`
		} `json:"contexts"`
	} `json:"contexts"`
}

func (p *SailingDirectionsProvider) QueryCoastPilot(ctx agent.Context, args CoastPilotArgs) (SailingDirectionsResult, error) {
	reqCtx := context.Background()
	if ctx != nil {
		reqCtx = ctx
	}
	slog.DebugContext(reqCtx, "QueryCoastPilot delegated to QuerySailingDirections", "query", args.Query)
	return p.QuerySailingDirections(ctx, SailingDirectionsArgs{
		Query:     args.Query,
		Territory: "us",
		TopK:      args.TopK,
	})
}

func (p *SailingDirectionsProvider) QuerySailingDirections(ctx agent.Context, args SailingDirectionsArgs) (SailingDirectionsResult, error) {
	start := time.Now()
	reqCtx := context.Background()
	if ctx != nil {
		reqCtx = ctx
	}

	if strings.TrimSpace(args.Query) == "" {
		slog.WarnContext(reqCtx, "RAG query rejected: empty query parameter")
		return SailingDirectionsResult{}, fmt.Errorf("query parameter is required")
	}

	territory := strings.ToLower(strings.TrimSpace(args.Territory))
	if territory == "" {
		territory = "all"
	}

	topK := args.TopK
	if topK <= 0 || topK > 10 {
		topK = 5
	}

	// If projectID is not configured (e.g. unit test or local mockup without GCP), return a helpful response.
	if p.projectID == "" {
		slog.DebugContext(reqCtx, "RAG query running in local test mode (no GCP project configured)",
			"query", args.Query,
			"territory", territory,
		)
		return SailingDirectionsResult{
			Query:           args.Query,
			Territory:       territory,
			Contexts:        "Sailing directions service is in local test mode (no GCP project configured).",
			DebugDurationMS: time.Since(start).Milliseconds(),
		}, nil
	}

	var resources []ragResource
	switch territory {
	case "international":
		if p.ngaCorpus != "" {
			resources = append(resources, ragResource{
				RagCorpus: formatCorpusResource(p.projectID, p.location, p.ngaCorpus),
			})
		}
	case "us":
		if p.coastPilotCorpus != "" {
			resources = append(resources, ragResource{
				RagCorpus: formatCorpusResource(p.projectID, p.location, p.coastPilotCorpus),
			})
		}
	default:
		if p.coastPilotCorpus != "" {
			resources = append(resources, ragResource{
				RagCorpus: formatCorpusResource(p.projectID, p.location, p.coastPilotCorpus),
			})
		}
		if p.ngaCorpus != "" {
			resources = append(resources, ragResource{
				RagCorpus: formatCorpusResource(p.projectID, p.location, p.ngaCorpus),
			})
		}
	}

	if len(resources) == 0 {
		slog.WarnContext(reqCtx, "RAG query aborted: no RAG corpus configured for territory",
			"query", args.Query,
			"territory", territory,
		)
		return SailingDirectionsResult{
			Query:           args.Query,
			Territory:       territory,
			Contexts:        "No RAG corpus configured for requested territory.",
			DebugDurationMS: time.Since(start).Milliseconds(),
		}, nil
	}

	endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s:retrieveContexts",
		p.location, p.projectID, p.location)

	var reqBody retrieveContextsRequest
	reqBody.VertexRagStore.RagResources = resources
	reqBody.Query.Text = args.Query

	slog.InfoContext(reqCtx, "RAG query starting",
		"query", args.Query,
		"territory", territory,
		"top_k", topK,
		"corpora_count", len(resources),
		"endpoint", endpoint,
	)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		slog.ErrorContext(reqCtx, "RAG query request marshal failed",
			"query", args.Query,
			"error", err,
		)
		return SailingDirectionsResult{}, fmt.Errorf("marshaling retrieveContexts request: %w", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		slog.ErrorContext(reqCtx, "RAG query HTTP request creation failed",
			"query", args.Query,
			"error", err,
		)
		return SailingDirectionsResult{}, fmt.Errorf("creating http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		slog.ErrorContext(reqCtx, "RAG retrieveContexts HTTP call failed",
			"query", args.Query,
			"endpoint", endpoint,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return SailingDirectionsResult{}, fmt.Errorf("%w: retrieveContexts call failed: %w", ErrAPIUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(reqCtx, "RAG retrieveContexts returned error status",
			"query", args.Query,
			"status_code", resp.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"body", string(body),
		)
		return SailingDirectionsResult{}, fmt.Errorf("%w: vertex rag error (%d): %s", ErrAPIUnavailable, resp.StatusCode, string(body))
	}

	formatted, chunkCount, sourceList, err := formatRagContextResponse(resp.Body)
	if err != nil {
		slog.ErrorContext(reqCtx, "RAG retrieveContexts response decode error",
			"query", args.Query,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return SailingDirectionsResult{}, err
	}

	durationMS := time.Since(start).Milliseconds()
	slog.InfoContext(reqCtx, "RAG query completed",
		"query", args.Query,
		"territory", territory,
		"chunks_returned", chunkCount,
		"sources", sourceList,
		"duration_ms", durationMS,
	)

	return SailingDirectionsResult{
		Query:           args.Query,
		Territory:       territory,
		Contexts:        formatted,
		DebugDurationMS: durationMS,
	}, nil
}

func formatCorpusResource(projectID, location, corpusID string) string {
	if strings.HasPrefix(corpusID, "projects/") {
		return corpusID
	}
	return fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", projectID, location, corpusID)
}

func formatRagContextResponse(body io.Reader) (string, int, []string, error) {
	var resp retrieveContextsResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return "", 0, nil, fmt.Errorf("decoding retrieveContexts response: %w", err)
	}

	items := resp.Contexts.Contexts
	if len(items) == 0 {
		return "No relevant hydrographic pilot contexts found for the query.", 0, nil, nil
	}

	var sources []string
	var sb strings.Builder
	for i, item := range items {
		sourceName := item.SourceDisplayName
		if sourceName == "" {
			sourceName = item.SourceURI
		}
		if sourceName == "" {
			sourceName = "Official Nautical Publication"
		}
		sources = append(sources, sourceName)

		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}

		if item.Score > 0 {
			sb.WriteString(fmt.Sprintf("### Source: %s (Relevance: %.2f)\n", sourceName, item.Score))
		} else {
			sb.WriteString(fmt.Sprintf("### Source: %s\n", sourceName))
		}
		sb.WriteString(strings.TrimSpace(item.Text))
	}

	return sb.String(), len(items), sources, nil
}
