package tool

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type mockHTTPDoer struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}

func TestNewSailingDirectionsTool(t *testing.T) {
	tool, provider, err := NewSailingDirectionsTool("test-project", "us-central1", "cp-corpus", "nga-corpus")
	if err != nil {
		t.Fatalf("NewSailingDirectionsTool() error = %v", err)
	}

	if tool.Name() != "query_sailing_directions" {
		t.Errorf("tool.Name() = %v, want query_sailing_directions", tool.Name())
	}

	coastPilotTool, err := NewCoastPilotTool(provider)
	if err != nil {
		t.Fatalf("NewCoastPilotTool() error = %v", err)
	}
	if coastPilotTool.Name() != "query_coast_pilot" {
		t.Errorf("coastPilotTool.Name() = %v, want query_coast_pilot", coastPilotTool.Name())
	}
}

func TestQuerySailingDirections(t *testing.T) {
	tests := []struct {
		name        string
		args        SailingDirectionsArgs
		mockResp    string
		mockStatus  int
		mockErr     error
		projectID   string
		wantErr     bool
		wantContain string
	}{
		{
			name: "empty query error",
			args: SailingDirectionsArgs{
				Query: "",
			},
			projectID: "test-proj",
			wantErr:   true,
		},
		{
			name: "local mode when projectID empty",
			args: SailingDirectionsArgs{
				Query: "Cape Cod Canal clearance",
			},
			projectID:   "",
			wantErr:     false,
			wantContain: "local test mode",
		},
		{
			name: "successful us query",
			args: SailingDirectionsArgs{
				Query:     "Newport Harbor bridge clearances",
				Territory: "us",
			},
			projectID:  "test-proj",
			mockStatus: http.StatusOK,
			mockResp: `{
				"contexts": {
					"contexts": [
						{
							"sourceDisplayName": "CPB2_WEB.pdf",
							"text": "The Newport Pell Bridge has a vertical clearance of 213 feet in the main navigation span.",
							"score": 0.92
						}
					]
				}
			}`,
			wantErr:     false,
			wantContain: "Newport Pell Bridge",
		},
		{
			name: "successful international query",
			args: SailingDirectionsArgs{
				Query:     "Nassau Harbour pilotage",
				Territory: "international",
			},
			projectID:  "test-proj",
			mockStatus: http.StatusOK,
			mockResp: `{
				"contexts": {
					"contexts": [
						{
							"sourceDisplayName": "Pub_147.pdf",
							"text": "Pilotage is compulsory for vessels over 150 gross tons entering Nassau Harbour.",
							"score": 0.88
						}
					]
				}
			}`,
			wantErr:     false,
			wantContain: "Pub_147.pdf",
		},
		{
			name: "empty contexts returned",
			args: SailingDirectionsArgs{
				Query: "Unknown remote reef",
			},
			projectID:  "test-proj",
			mockStatus: http.StatusOK,
			mockResp: `{
				"contexts": {
					"contexts": []
				}
			}`,
			wantErr:     false,
			wantContain: "No relevant hydrographic pilot contexts found",
		},
		{
			name: "api error status",
			args: SailingDirectionsArgs{
				Query: "Chesapeake Bay controlling depth",
			},
			projectID:   "test-proj",
			mockStatus:  http.StatusInternalServerError,
			mockResp:    `{"error": "internal error"}`,
			wantErr:     false,
			wantContain: "unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDoer := &mockHTTPDoer{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(bytes.NewBufferString(tt.mockResp)),
					}, nil
				},
			}

			provider := &SailingDirectionsProvider{
				projectID:        tt.projectID,
				location:         "us-central1",
				coastPilotCorpus: "coast-pilot-corpus",
				ngaCorpus:        "nga-sailing-directions-corpus",
				client:           mockDoer,
			}

			res, err := provider.QuerySailingDirections(newMockContext(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("QuerySailingDirections() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.wantContain != "" {
				if !strings.Contains(res.Contexts, tt.wantContain) {
					t.Errorf("Contexts = %q, want containing %q", res.Contexts, tt.wantContain)
				}
			}
		})
	}
}

func TestQueryCoastPilot(t *testing.T) {
	mockDoer := &mockHTTPDoer{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"contexts": {
						"contexts": [
							{
								"sourceDisplayName": "CPB1_WEB.pdf",
								"text": "Cape Cod Canal controlling depth is 32 feet at mean low water.",
								"score": 0.95
							}
						]
					}
				}`)),
			}, nil
		},
	}

	provider := &SailingDirectionsProvider{
		projectID:        "test-proj",
		location:         "us-central1",
		coastPilotCorpus: "coast-pilot-corpus",
		ngaCorpus:        "nga-sailing-directions-corpus",
		client:           mockDoer,
	}

	res, err := provider.QueryCoastPilot(newMockContext(), CoastPilotArgs{
		Query: "Cape Cod Canal controlling depth",
	})
	if err != nil {
		t.Fatalf("QueryCoastPilot() error = %v", err)
	}

	if !strings.Contains(res.Contexts, "32 feet") {
		t.Errorf("Expected context to contain '32 feet', got %s", res.Contexts)
	}
	if res.Territory != "us" {
		t.Errorf("Expected Territory 'us', got %s", res.Territory)
	}
}

func TestQuerySailingDirections_AllTerritorySplitsRequestsAndPassesTopK(t *testing.T) {
	var retrieveCalls atomic.Int32
	mockDoer := &mockHTTPDoer{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/ragCorpora") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"ragCorpora": [
							{"name": "projects/test-proj/locations/us-central1/ragCorpora/111", "displayName": "coast-pilot-corpus"},
							{"name": "projects/test-proj/locations/us-central1/ragCorpora/222", "displayName": "nga-sailing-directions-corpus"}
						]
					}`)),
				}, nil
			}
			bodyBytes, _ := io.ReadAll(req.Body)
			bodyStr := string(bodyBytes)
			if strings.Count(bodyStr, `"ragCorpus"`) != 1 {
				t.Errorf("expected exactly 1 ragCorpus per retrieveContexts request, got body: %s", bodyStr)
			}
			if !strings.Contains(bodyStr, `"similarityTopK":7`) {
				t.Errorf("expected similarityTopK:7 in request body, got: %s", bodyStr)
			}
			retrieveCalls.Add(1)
			source := "CPB7_WEB.pdf"
			text := "San Pedro Channel controlling depth."
			if strings.Contains(bodyStr, "ragCorpora/222") {
				source = "Pub_154.pdf"
				text = "Catalina approach notes."
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"contexts": {
						"contexts": [
							{"sourceDisplayName": "` + source + `", "text": "` + text + `", "score": 0.9}
						]
					}
				}`)),
			}, nil
		},
	}

	provider := &SailingDirectionsProvider{
		projectID:        "test-proj",
		location:         "us-central1",
		coastPilotCorpus: "coast-pilot-corpus",
		ngaCorpus:        "nga-sailing-directions-corpus",
		client:           mockDoer,
		resolvedCorpora:  make(map[string]string),
	}

	res, err := provider.QuerySailingDirections(newMockContext(), SailingDirectionsArgs{
		Query:     "San Pedro Channel hazards",
		Territory: "all",
		TopK:      7,
	})
	if err != nil {
		t.Fatalf("QuerySailingDirections() error = %v", err)
	}
	if retrieveCalls.Load() != 2 {
		t.Errorf("expected 2 retrieveContexts calls (one per corpus), got %d", retrieveCalls.Load())
	}
	if !strings.Contains(res.Contexts, "San Pedro Channel controlling depth.") || !strings.Contains(res.Contexts, "Catalina approach notes.") {
		t.Errorf("expected merged contexts from both corpora, got: %s", res.Contexts)
	}
}

func TestResolveCorpus(t *testing.T) {
	tests := []struct {
		name     string
		corpusID string
		mockResp string
		want     string
	}{
		{
			name:     "empty corpus ID",
			corpusID: "",
			want:     "",
		},
		{
			name:     "full project resource name",
			corpusID: "projects/my-proj/locations/us-central1/ragCorpora/12345",
			want:     "projects/my-proj/locations/us-central1/ragCorpora/12345",
		},
		{
			name:     "numeric corpus ID",
			corpusID: "7540406762822696960",
			want:     "projects/test-proj/locations/us-central1/ragCorpora/7540406762822696960",
		},
		{
			name:     "displayName resolved dynamically via API",
			corpusID: "coast-pilot-corpus",
			mockResp: `{
				"ragCorpora": [
					{
						"name": "projects/test-proj/locations/us-central1/ragCorpora/7540406762822696960",
						"displayName": "coast-pilot-corpus"
					}
				]
			}`,
			want: "projects/test-proj/locations/us-central1/ragCorpora/7540406762822696960",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDoer := &mockHTTPDoer{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(tt.mockResp)),
					}, nil
				},
			}

			provider := &SailingDirectionsProvider{
				projectID:       "test-proj",
				location:        "us-central1",
				client:          mockDoer,
				resolvedCorpora: make(map[string]string),
			}

			got := provider.resolveCorpus(context.Background(), tt.corpusID)
			if got != tt.want {
				t.Errorf("resolveCorpus(%q) = %q, want %q", tt.corpusID, got, tt.want)
			}
		})
	}
}
