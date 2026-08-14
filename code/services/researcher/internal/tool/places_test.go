package tool

import (
	"context"
	"errors"
	"fmt"
	"testing"

	placespb "cloud.google.com/go/maps/places/apiv1/placespb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/genproto/googleapis/type/latlng"
)

// mockPlacesClient is a mock implementation of the PlacesClient interface.
type mockPlacesClient struct {
	SearchTextFunc func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error)
	CloseFunc      func() error
}

func (m *mockPlacesClient) SearchText(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
	if m.SearchTextFunc != nil {
		return m.SearchTextFunc(ctx, req, opts...)
	}
	return nil, nil
}

func (m *mockPlacesClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func TestNewPlacesTool(t *testing.T) {
	tool, _, err := NewPlacesTool(context.Background(), "dummy-key")
	if err != nil {
		t.Fatalf("NewPlacesTool() error = %v", err)
	}

	if tool.Name() != "find_places_nearby" {
		t.Errorf("NewPlacesTool().Name() = %v, want %v", tool.Name(), "find_places_nearby")
	}

	expectedDesc := "Finds places (e.g. marinas, restaurants) near a location using Google Maps Text Search. Returns specific locations with Lat/Lng."
	if tool.Description() != expectedDesc {
		t.Errorf("NewPlacesTool().Description() = %v, want %v", tool.Description(), expectedDesc)
	}
}

func TestFindPlaces_Success(t *testing.T) {
	mockClient := &mockPlacesClient{
		SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
			if req.TextQuery != "marina" {
				return nil, fmt.Errorf("unexpected query: %s", req.TextQuery)
			}
			return &placespb.SearchTextResponse{
				Places: []*placespb.Place{
					{
						// DisplayName:      &placespb.LocalizedText{Text: "Test Marina"},
						FormattedAddress: "123 Ocean Dr",
						Location:         &latlng.LatLng{Latitude: 10.0, Longitude: 20.0},
						Rating:           4.5,
						UserRatingCount:  int32Ptr(100),
						BusinessStatus:   placespb.Place_OPERATIONAL,
						Types:            []string{"marina"},
						WebsiteUri:       "http://example.com",
					},
				},
			}, nil
		},
	}

	p := &PlacesProvider{client: mockClient}
	args := PlacesArgs{
		Query:     "marina",
		Latitude:  10.0,
		Longitude: 20.0,
	}

	resp, err := p.FindPlaces(newMockContext(), args)
	if err != nil {
		t.Fatalf("FindPlaces() error = %v", err)
	}

	if len(resp.Places) != 1 {
		t.Errorf("Expected 1 place, got %d", len(resp.Places))
	}
	if resp.Places[0].Address != "123 Ocean Dr" {
		t.Errorf("Expected place address '123 Ocean Dr', got %s", resp.Places[0].Address)
	}
}

func TestFindPlaces_APIError(t *testing.T) {
	mockClient := &mockPlacesClient{
		SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
			return nil, fmt.Errorf("API error")
		},
	}

	p := &PlacesProvider{client: mockClient}
	args := PlacesArgs{Query: "marina"}

	_, err := p.FindPlaces(newMockContext(), args)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !errors.Is(err, ErrAPIUnavailable) {
		t.Errorf("expected ErrAPIUnavailable, got %v", err)
	}
}

func TestFindPlaces_RadiusClamping(t *testing.T) {
	tests := []struct {
		name           string
		inputRadius    float64
		expectedRadius float64
	}{
		{
			name:           "Radius zero uses default 5000",
			inputRadius:    0,
			expectedRadius: 5000,
		},
		{
			name:           "Radius within limits preserved",
			inputRadius:    15000,
			expectedRadius: 15000,
		},
		{
			name:           "Radius exceeding 50000 clamped to 50000",
			inputRadius:    74080,
			expectedRadius: 50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRadius float64
			mockClient := &mockPlacesClient{
				SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
					if req.LocationBias != nil {
						if circle, ok := req.LocationBias.Type.(*placespb.SearchTextRequest_LocationBias_Circle); ok && circle.Circle != nil {
							capturedRadius = circle.Circle.Radius
						}
					}
					return &placespb.SearchTextResponse{}, nil
				},
			}

			p := &PlacesProvider{client: mockClient}
			_, err := p.FindPlaces(newMockContext(), PlacesArgs{
				Query:     "marina",
				Latitude:  10.0,
				Longitude: 20.0,
				Radius:    tt.inputRadius,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if capturedRadius != tt.expectedRadius {
				t.Errorf("got radius %v, want %v", capturedRadius, tt.expectedRadius)
			}
		})
	}
}
