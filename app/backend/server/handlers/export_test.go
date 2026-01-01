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
	handler := handlers.New(mockStore, mockDocs, "test_content")

	voyageID := int64(1)
	now := time.Now()
	voyage := &models.Voyage{
		ID:        voyageID,
		Title:     "Test Voyage",
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
	mockStore.On("ListStops", voyageID).Return(stops, nil)
	mockStore.On("GetBriefing", int64(10)).Return(briefing, nil)
	
	// Note: DB is NOT updated in this handler anymore (frontend handles the actual export)

	// Setup Router
	r := chi.NewRouter()
	r.Post("/voyages/{id}/export", handler.ExportVoyage)

	req := httptest.NewRequest("POST", "/voyages/1/export", nil)
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
