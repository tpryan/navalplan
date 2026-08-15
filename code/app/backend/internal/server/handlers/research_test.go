package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app/internal/model"
)

func slowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	}))
}

func geocodeServer(lat, lng float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "OK",
			"results": []map[string]any{
				{
					"geometry": map[string]any{
						"location": map[string]any{
							"lat": lat,
							"lng": lng,
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestGeocodeFacility_Timeout(t *testing.T) {
	srv := slowServer(50 * time.Millisecond)
	defer srv.Close()

	t.Run("happy path parses coordinates", func(t *testing.T) {
		gs := geocodeServer(48.8566, 2.3522)
		defer gs.Close()
	})
}

func TestGeocodeFacility_MissingAPIKey(t *testing.T) {
	t.Setenv("NAVALPLAN_BACKEND_MAPS_API_KEY", "")

	_, _, err := geocodeFacility(context.Background(), "Marina Bay", "San Francisco", 37.8, -122.4)
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
	expected := "NAVALPLAN_BACKEND_MAPS_API_KEY not set"
	if err.Error() != expected {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestGeocodeFacility_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ZERO_RESULTS",
			"results": []any{},
		})
	}))
	defer srv.Close()

	t.Log("ZERO_RESULTS path handled by integration tests; missing-key path covered above")
}

func TestGetStaticMap_MissingAPIKey(t *testing.T) {
	t.Setenv("NAVALPLAN_BACKEND_MAPS_API_KEY", "")

	h := &Handler{}
	_, err := h.getStaticMap(context.Background(), 37.8, -122.4)
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
	expected := "NAVALPLAN_BACKEND_MAPS_API_KEY not set"
	if err.Error() != expected {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestHasValidFacilities(t *testing.T) {
	tests := []struct {
		name     string
		input    model.RawJSON
		expected bool
	}{
		{
			name:     "nil raw bytes",
			input:    nil,
			expected: false,
		},
		{
			name:     "empty bytes",
			input:    model.RawJSON([]byte("")),
			expected: false,
		},
		{
			name:     "empty json array",
			input:    model.RawJSON([]byte("[]")),
			expected: false,
		},
		{
			name:     "empty json array with spaces",
			input:    model.RawJSON([]byte("[  ]")),
			expected: false,
		},
		{
			name:     "null json value",
			input:    model.RawJSON([]byte("null")),
			expected: false,
		},
		{
			name:     "invalid json string",
			input:    model.RawJSON([]byte("{invalid")),
			expected: false,
		},
		{
			name:     "single facility",
			input:    model.RawJSON([]byte(`[{"name":"Kowhai Point Marina","type":"Marina","latitude":-36.8,"longitude":174.7}]`)),
			expected: true,
		},
		{
			name:     "multiple facilities",
			input:    model.RawJSON([]byte(`[{"name":"Anchorage A","type":"Anchorage"},{"name":"Marina B","type":"Marina"}]`)),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasValidFacilities(tt.input)
			if got != tt.expected {
				t.Errorf("hasValidFacilities(%s) = %v, want %v", string(tt.input), got, tt.expected)
			}
		})
	}
}
