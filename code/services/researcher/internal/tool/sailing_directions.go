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
	"sync"
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
	mu               sync.RWMutex
	resolvedCorpora  map[string]string
}

func (p *SailingDirectionsProvider) Close() error {
	return nil
}

// NewSailingDirectionsTool creates the ADK function tool for query_sailing_directions.
func NewSailingDirectionsTool(projectID, location, coastPilotCorpus, ngaCorpus string) (tool.Tool, *SailingDirectionsProvider, error) {
	var client HTTPDoer = http.DefaultClient
	if projectID != "" && projectID != "your-project-id" {
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
		resolvedCorpora:  make(map[string]string),
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
		Text           string `json:"text"`
		SimilarityTopK int    `json:"similarityTopK,omitempty"`
	} `json:"query"`
}

type ragContextChunk struct {
	SourceURI         string  `json:"sourceUri"`
	SourceDisplayName string  `json:"sourceDisplayName"`
	Text              string  `json:"text"`
	Score             float64 `json:"score,omitempty"`
	Distance          float64 `json:"distance,omitempty"`
}

type retrieveContextsResponse struct {
	Contexts struct {
		Contexts []ragContextChunk `json:"contexts"`
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
	addCorpus := func(corpusID string) {
		if corpusID != "" {
			if res := p.resolveCorpus(reqCtx, corpusID); res != "" {
				resources = append(resources, ragResource{RagCorpus: res})
			}
		}
	}

	switch territory {
	case "international":
		addCorpus(p.ngaCorpus)
	case "us":
		addCorpus(p.coastPilotCorpus)
	default:
		addCorpus(p.coastPilotCorpus)
		addCorpus(p.ngaCorpus)
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

	slog.InfoContext(reqCtx, "RAG query starting",
		"query", args.Query,
		"territory", territory,
		"top_k", topK,
		"corpora_count", len(resources),
		"endpoint", endpoint,
	)

	type corpusOutcome struct {
		chunks    []ragContextChunk
		httpErr   error
		statusErr int
		decodeErr error
	}
	outcomes := make([]corpusOutcome, len(resources))
	var wg sync.WaitGroup
	for i, res := range resources {
		wg.Add(1)
		go func(idx int, r ragResource) {
			defer wg.Done()
			var reqBody retrieveContextsRequest
			reqBody.VertexRagStore.RagResources = []ragResource{r}
			reqBody.Query.Text = args.Query
			reqBody.Query.SimilarityTopK = topK

			payload, err := json.Marshal(reqBody)
			if err != nil {
				slog.ErrorContext(reqCtx, "RAG query request marshal failed",
					"query", args.Query,
					"corpus", r.RagCorpus,
					"error", err,
				)
				outcomes[idx].decodeErr = fmt.Errorf("marshaling retrieveContexts request: %w", err)
				return
			}

			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
			if err != nil {
				slog.ErrorContext(reqCtx, "RAG query HTTP request creation failed",
					"query", args.Query,
					"corpus", r.RagCorpus,
					"error", err,
				)
				outcomes[idx].decodeErr = fmt.Errorf("creating http request: %w", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := p.client.Do(req)
			if err != nil {
				slog.WarnContext(reqCtx, "RAG retrieveContexts HTTP call failed, falling back to graceful notice",
					"query", args.Query,
					"corpus", r.RagCorpus,
					"endpoint", endpoint,
					"duration_ms", time.Since(start).Milliseconds(),
					"error", err,
				)
				outcomes[idx].httpErr = err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				slog.WarnContext(reqCtx, "RAG retrieveContexts returned error status, falling back to graceful notice",
					"query", args.Query,
					"corpus", r.RagCorpus,
					"status_code", resp.StatusCode,
					"duration_ms", time.Since(start).Milliseconds(),
					"body", string(body),
				)
				outcomes[idx].statusErr = resp.StatusCode
				return
			}

			var parsed retrieveContextsResponse
			if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
				slog.ErrorContext(reqCtx, "RAG retrieveContexts response decode error",
					"query", args.Query,
					"corpus", r.RagCorpus,
					"duration_ms", time.Since(start).Milliseconds(),
					"error", err,
				)
				outcomes[idx].decodeErr = fmt.Errorf("decoding retrieveContexts response: %w", err)
				return
			}
			outcomes[idx].chunks = parsed.Contexts.Contexts
		}(i, res)
	}
	wg.Wait()

	var allChunks []ragContextChunk
	var successCount, httpFailCount, statusFailCount int
	var firstDecodeErr error
	for _, o := range outcomes {
		if o.decodeErr != nil {
			if firstDecodeErr == nil {
				firstDecodeErr = o.decodeErr
			}
			continue
		}
		if o.httpErr != nil {
			httpFailCount++
			continue
		}
		if o.statusErr != 0 {
			statusFailCount++
			continue
		}
		successCount++
		allChunks = append(allChunks, o.chunks...)
	}

	if successCount == 0 {
		if firstDecodeErr != nil {
			return SailingDirectionsResult{}, firstDecodeErr
		}
		if httpFailCount > 0 && statusFailCount == 0 {
			return SailingDirectionsResult{
				Query:           args.Query,
				Territory:       territory,
				Contexts:        "Official hydrographic sailing directions are currently unreachable; rely on Google Search results and nautical charts.",
				DebugDurationMS: time.Since(start).Milliseconds(),
			}, nil
		}
		return SailingDirectionsResult{
			Query:           args.Query,
			Territory:       territory,
			Contexts:        "Official hydrographic sailing directions are currently unavailable for this area; rely on Google Search results and nautical charts.",
			DebugDurationMS: time.Since(start).Milliseconds(),
		}, nil
	}

	formatted, chunkCount, sourceList := formatRagChunks(allChunks)
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

func (p *SailingDirectionsProvider) resolveCorpus(ctx context.Context, corpusID string) string {
	if corpusID == "" {
		return ""
	}
	if strings.HasPrefix(corpusID, "projects/") {
		return corpusID
	}
	isNumeric := true
	for _, r := range corpusID {
		if r < '0' || r > '9' {
			isNumeric = false
			break
		}
	}
	if isNumeric {
		return fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", p.projectID, p.location, corpusID)
	}

	p.mu.RLock()
	if p.resolvedCorpora != nil {
		if resolved, ok := p.resolvedCorpora[corpusID]; ok {
			p.mu.RUnlock()
			return resolved
		}
	}
	p.mu.RUnlock()

	// Query Vertex AI list ragCorpora endpoint to resolve displayName to resource name
	listURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/ragCorpora", p.location, p.projectID, p.location)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return formatCorpusResource(p.projectID, p.location, corpusID)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "Failed to dynamically list RAG corpora", "corpus_id", corpusID, "error", err)
		return formatCorpusResource(p.projectID, p.location, corpusID)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.WarnContext(ctx, "RAG corpora list returned non-OK status",
			"corpus_id", corpusID,
			"status_code", resp.StatusCode,
			"body", string(body),
		)
		return formatCorpusResource(p.projectID, p.location, corpusID)
	}

	var listResp struct {
		RagCorpora []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"ragCorpora"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		slog.WarnContext(ctx, "Failed to decode RAG corpora list response", "corpus_id", corpusID, "error", err)
		return formatCorpusResource(p.projectID, p.location, corpusID)
	}

	p.mu.Lock()
	if p.resolvedCorpora == nil {
		p.resolvedCorpora = make(map[string]string)
	}
	for _, c := range listResp.RagCorpora {
		p.resolvedCorpora[c.DisplayName] = c.Name
		p.resolvedCorpora[c.Name] = c.Name
	}
	resolved, ok := p.resolvedCorpora[corpusID]
	p.mu.Unlock()

	if ok {
		return resolved
	}

	slog.WarnContext(ctx, "RAG corpus displayName not found in project corpora list, using fallback path", "corpus_id", corpusID)
	return formatCorpusResource(p.projectID, p.location, corpusID)
}

func formatCorpusResource(projectID, location, corpusID string) string {
	if strings.HasPrefix(corpusID, "projects/") {
		return corpusID
	}
	return fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", projectID, location, corpusID)
}

func formatRagChunks(items []ragContextChunk) (string, int, []string) {
	if len(items) == 0 {
		return "No relevant hydrographic pilot contexts found for the query.", 0, nil
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

	return sb.String(), len(items), sources
}
