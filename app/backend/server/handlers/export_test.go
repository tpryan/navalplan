package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app/models"
	"app/server/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestExportVoyage_Success(t *testing.T) {
	mockStore := new(MockStore)
	mockDocs := new(MockDocsService)
	handler := handlers.New(mockStore, mockDocs, "test_content", "http://test-agent")

	personID := int64(1)
	voyageID := int64(1)
	now := time.Now()
	voyage := &models.Voyage{
		ID:        voyageID,
		Title:     "Test Voyage",
		PersonID:  personID,
		StartDate: now,
		EndDate:   now.Add(24 * time.Hour),
	}
	stops := []models.Stop{
		{ID: 10, VoyageID: voyageID, LocationName: "Stop 1", TargetDate: now},
	}
	briefing := &models.Briefing{
		ID:             1,
		StopID:         10,
		WeatherSummary: []byte(`{"summary": "Sunny"}`),
	}

	// DB Expectations
	mockStore.On("GetVoyage", voyageID).Return(voyage, nil)
	mockStore.On("ListStops", voyageID, 0, 0).Return(stops, nil)
	mockStore.On("ListVoyageBriefings", voyageID).Return([]models.Briefing{*briefing}, nil)
	// mockStore.On("GetBriefing", int64(10)).Return(briefing, nil) // Removed in N+1 fix

	// Note: DB is NOT updated in this handler anymore (frontend handles the actual export)

	// Setup Router
	r := chi.NewRouter()
	r.Post("/voyages/{id}/export", handler.ExportVoyage)

	req := httptest.NewRequest("POST", "/voyages/1/export", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp handlers.ExportResponse
	json.NewDecoder(w.Body).Decode(&resp)

	assert.Equal(t, "Logbook: Test Voyage", resp.Title)
	assert.NotEmpty(t, resp.Requests)
	// We expect at least header + stop + weather requests
	assert.Greater(t, len(resp.Requests), 2)

	mockStore.AssertExpectations(t)
}

func TestExportVoyage_Unauthorized(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")

	personID := int64(1)
	otherPersonID := int64(2)
	voyageID := int64(1)
	voyage := &models.Voyage{
		ID:       voyageID,
		PersonID: otherPersonID,
	}

	mockStore.On("GetVoyage", voyageID).Return(voyage, nil)

	r := chi.NewRouter()
	r.Post("/voyages/{id}/export", handler.ExportVoyage)

	req := httptest.NewRequest("POST", "/voyages/1/export", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	mockStore.AssertExpectations(t)
}

func TestExportVoyage_Unauthenticated(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")

	r := chi.NewRouter()
	r.Post("/voyages/{id}/export", handler.ExportVoyage)

	req := httptest.NewRequest("POST", "/voyages/1/export", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
