package tool

import (
	"bytes"
	"io"
	"net/http"
	"strings"
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
			projectID:  "test-proj",
			mockStatus: http.StatusInternalServerError,
			mockResp:   `{"error": "internal error"}`,
			wantErr:    true,
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
