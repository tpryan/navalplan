package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

	if len(svc.Interfaces) == 0 {
		return "", fmt.Errorf("service %s has no interfaces", serviceID)
	}

	// For now, just return the first URL.
	// We might want to filter by ProtocolBinding (e.g., "HTTP_JSON" or "JSONRPC").
	return svc.Interfaces[0].URL, nil
}

// RegistryResolver resolves agent URLs by querying the Agent Registry for a base service.
type RegistryResolver struct {
	Client *Client
}

func (r *RegistryResolver) Resolve(ctx context.Context, appName string) (string, error) {
	// For this application, all agents are served by the "navalplan-researcher" base service.
	return r.Client.ResolveServiceURL(ctx, "navalplan-researcher")
}
