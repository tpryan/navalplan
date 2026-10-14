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
	SearchTextFunc   func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error)
	SearchNearbyFunc func(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error)
	CloseFunc        func() error
}

func (m *mockPlacesClient) SearchText(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
	if m.SearchTextFunc != nil {
		return m.SearchTextFunc(ctx, req, opts...)
	}
	return nil, nil
}

func (m *mockPlacesClient) SearchNearby(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error) {
	if m.SearchNearbyFunc != nil {
		return m.SearchNearbyFunc(ctx, req, opts...)
	}
	return &placespb.SearchNearbyResponse{}, nil
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

func TestFindPlaces_AmenityUsesNearbySearchByDistance(t *testing.T) {
	var got *placespb.SearchNearbyRequest
	textCalled := false
	mockClient := &mockPlacesClient{
		SearchNearbyFunc: func(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error) {
			got = req
			return &placespb.SearchNearbyResponse{Places: []*placespb.Place{
				{Location: &latlng.LatLng{Latitude: 10.001, Longitude: 20.0}, Types: []string{"pharmacy"}},
			}}, nil
		},
		SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
			textCalled = true
			return &placespb.SearchTextResponse{}, nil
		},
	}
	p := &PlacesProvider{client: mockClient}
	res, err := p.FindPlaces(newMockContext(), PlacesArgs{Query: "pharmacy", Latitude: 10.0, Longitude: 20.0, Radius: 1600})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if textCalled {
		t.Error("Text Search called for an amenity query; want Nearby Search only")
	}
	if got == nil {
		t.Fatal("Nearby Search not called")
	}
	if got.RankPreference != placespb.SearchNearbyRequest_DISTANCE {
		t.Errorf("RankPreference = %v, want DISTANCE", got.RankPreference)
	}
	circle, ok := got.LocationRestriction.GetType().(*placespb.SearchNearbyRequest_LocationRestriction_Circle)
	if !ok || circle.Circle.Radius != 1600 {
		t.Errorf("LocationRestriction = %v, want 1600 m circle", got.LocationRestriction)
	}
	if len(got.IncludedTypes) == 0 || got.IncludedTypes[0] != "pharmacy" {
		t.Errorf("IncludedTypes = %v, want pharmacy first", got.IncludedTypes)
	}
	if len(res.Places) != 1 {
		t.Errorf("got %d places, want 1", len(res.Places))
	}
}

func TestFindPlaces_MarineUsesTextSearchByDistance(t *testing.T) {
	var got *placespb.SearchTextRequest
	nearbyCalled := false
	mockClient := &mockPlacesClient{
		SearchNearbyFunc: func(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error) {
			nearbyCalled = true
			return &placespb.SearchNearbyResponse{}, nil
		},
		SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
			got = req
			return &placespb.SearchTextResponse{}, nil
		},
	}
	p := &PlacesProvider{client: mockClient}
	if _, err := p.FindPlaces(newMockContext(), PlacesArgs{Query: "anchorage", Latitude: 10.0, Longitude: 20.0, Radius: 1600}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nearbyCalled {
		t.Error("Nearby Search called for a marine query")
	}
	if got == nil || got.RankPreference != placespb.SearchTextRequest_DISTANCE {
		t.Errorf("Text Search request = %v, want RankPreference DISTANCE", got)
	}
}

func TestFindPlaces_NearbyErrorFallsBackToTextSearch(t *testing.T) {
	textCalled := false
	mockClient := &mockPlacesClient{
		SearchNearbyFunc: func(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error) {
			return nil, errors.New("INVALID_ARGUMENT: unsupported type")
		},
		SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
			textCalled = true
			return &placespb.SearchTextResponse{}, nil
		},
	}
	p := &PlacesProvider{client: mockClient}
	if _, err := p.FindPlaces(newMockContext(), PlacesArgs{Query: "cafe coffee shop", Latitude: 10.0, Longitude: 20.0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !textCalled {
		t.Error("Text Search fallback not called after Nearby Search error")
	}
}
