package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2/google"
)

// Client handles interaction with the Gemini Enterprise Agent Registry.
type Client struct {
	ProjectID string
	Location  string
	HTTP      *http.Client
}

// Service represents a registered service in the Agent Registry.
type Service struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Interfaces  []struct {
		ProtocolBinding string `json:"protocolBinding"`
		URL             string `json:"url"`
	} `json:"interfaces"`
	AgentSpec *struct {
		Type    string `json:"type"`
		Content struct {
			URL string `json:"url"`
		} `json:"content"`
	} `json:"agentSpec"`
}

// NewClient creates a new Agent Registry client.
func NewClient(ctx context.Context, projectID, location string) (*Client, error) {
	// The Agent Registry API requires an OAuth2 access token.
	// We use the cloud-platform scope to ensure we have the necessary permissions.
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to create default google client: %w", err)
	}

	return &Client{
		ProjectID: projectID,
		Location:  location,
		HTTP:      client,
	}, nil
}

// ResolveServiceURL finds the URL for a service by its ID (e.g., "navalplan-harbourmaster").
func (c *Client) ResolveServiceURL(ctx context.Context, serviceID string) (string, error) {
	url := fmt.Sprintf("https://agentregistry.googleapis.com/v1alpha/projects/%s/locations/%s/services/%s",
		c.ProjectID, c.Location, serviceID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var svc Service
	if err := json.NewDecoder(resp.Body).Decode(&svc); err != nil {
		return "", err
	}

	// First check interfaces (for CUSTOM/legacy services)
	if len(svc.Interfaces) > 0 {
		return svc.Interfaces[0].URL, nil
	}

	// Then fallback to AgentSpec (for A2A_AGENT_CARD services)
	if svc.AgentSpec != nil && svc.AgentSpec.Content.URL != "" {
		u := svc.AgentSpec.Content.URL
		// A2A Agent Cards store the A2A endpoint URL (e.g., https://.../invoke).
		// For the ADK REST API calls from the backend, we need the base URL
		// as the runner appends "/api/..." to it.
		if idx := strings.Index(u, "/invoke"); idx != -1 {
			u = u[:idx]
		}
		return u, nil
	}

	return "", fmt.Errorf("service %s has no interfaces and no valid AgentSpec URL", serviceID)
}

// RegistryResolver resolves agent URLs by querying the Agent Registry for a base service.
type RegistryResolver struct {
	Client *Client
}

func (r *RegistryResolver) Resolve(ctx context.Context, appName string) (string, error) {
	// For this application, all agents are served by the "navalplan-researcher" base service.
	return r.Client.ResolveServiceURL(ctx, "navalplan-researcher")
}
