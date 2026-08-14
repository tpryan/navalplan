package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// SearchFunc is a function that can execute a single search query.
type SearchFunc func(ctx context.Context, query string) (string, error)

// BatchSearchTool executes multiple search queries in parallel.
type BatchSearchTool struct {
	Searcher SearchFunc
}

type BatchSearchArgs struct {
	Queries []string `json:"queries" adk:"description:A list of 8-12 specific search queries to execute simultaneously."`
}

type BatchSearchResult struct {
	Results map[string]string `json:"results"`
}

func (t *BatchSearchTool) Name() string {
	return "batch_google_search"
}

func (t *BatchSearchTool) Description() string {
	return "Executes a list of search queries in parallel and returns all results. Use this to gather all required regional data (weather, hazards, hubs, etc.) in a single turn."
}

func (t *BatchSearchTool) IsLongRunning() bool {
	return false
}

// Declaration returns the function declaration for the LLM.
func (t *BatchSearchTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		ParametersJsonSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"queries": {
					Type:        genai.TypeArray,
					Description: "A list of 8-12 specific search queries to execute simultaneously.",
					Items: &genai.Schema{
						Type: genai.TypeString,
					},
				},
			},
			Required: []string{"queries"},
		},
	}
}

// ProcessRequest satisfies toolinternal.RequestProcessor.
func (t *BatchSearchTool) ProcessRequest(ctx agent.Context, req *model.LLMRequest) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	name := t.Name()
	if _, ok := req.Tools[name]; ok {
		return fmt.Errorf("duplicate tool: %q", name)
	}
	req.Tools[name] = t

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}

	// Find an existing genai.Tool with FunctionDeclarations
	var funcTool *genai.Tool
	for _, tool := range req.Config.Tools {
		if tool != nil && tool.FunctionDeclarations != nil {
			funcTool = tool
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{t.Declaration()},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, t.Declaration())
	}
	return nil
}

// Run satisfies toolinternal.FunctionTool.
func (t *BatchSearchTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	m, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected args type, got: %T", args)
	}

	// Convert map to struct
	var searchArgs BatchSearchArgs
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &searchArgs); err != nil {
		return nil, err
	}

	result, err := t.Execute(ctx, &searchArgs)
	if err != nil {
		return nil, err
	}

	// Convert struct to map
	var resultMap map[string]any
	rb, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rb, &resultMap); err != nil {
		return nil, err
	}

	return resultMap, nil
}

func (t *BatchSearchTool) Execute(ctx agent.Context, args *BatchSearchArgs) (*BatchSearchResult, error) {
	if len(args.Queries) == 0 {
		return &BatchSearchResult{Results: make(map[string]string)}, nil
	}

	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, q := range args.Queries {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()

			// Call the provided searcher function
			res, err := t.Searcher(ctx, query)

			mu.Lock()
			if err != nil {
				results[query] = "Error: " + err.Error()
			} else {
				results[query] = res
			}
			mu.Unlock()
		}(q)
	}
	wg.Wait()

	return &BatchSearchResult{Results: results}, nil
}
