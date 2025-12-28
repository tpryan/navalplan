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
	"github.com/stretchr/testify/mock"
	"google.golang.org/api/docs/v1"
)

func TestExportVoyage_Success(t *testing.T) {
	mockStore := new(MockStore)
	mockDocs := new(MockDocsService)
	handler := handlers.New(mockStore, mockDocs)

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
	
	// Expect DB update after export
	mockStore.On("UpdateVoyage", mock.MatchedBy(func(v *models.Voyage) bool {
		return v.ID == voyageID && v.GoogleDocID != nil && *v.GoogleDocID == "doc123"
	})).Return(nil)

	// Docs Expectations
	mockDocs.On("Create", mock.Anything, "Logbook: Test Voyage").Return(&docs.Document{DocumentId: "doc123"}, nil)
	mockDocs.On("BatchUpdate", mock.Anything, "doc123", mock.Anything).Return(nil)

	// Setup Router
	r := chi.NewRouter()
	r.Post("/voyages/{id}/export", handler.ExportVoyage)

	req := httptest.NewRequest("POST", "/voyages/1/export", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	
	var resp handlers.ExportResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "doc123", resp.DocID)
	assert.Contains(t, resp.DocURL, "doc123")

	mockStore.AssertExpectations(t)
	mockDocs.AssertExpectations(t)
}