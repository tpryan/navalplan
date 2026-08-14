package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/agent"
	"app/internal/model"
	"app/internal/server/handlers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCreateVoyage_SameDayDayTrip(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/voyages", handler.CreateVoyage)

	personID := int64(1)
	body := `{"title": "Sunday Sail", "start_date": "2026-07-19T00:00:00Z", "end_date": "2026-07-19T00:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/v1/voyages", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mockStore.On("CreateVoyage", mock.MatchedBy(func(v *model.Voyage) bool {
		return v.Title == "Sunday Sail" &&
			v.PersonID == personID &&
			v.StartDate != nil && v.EndDate != nil &&
			v.StartDate.Equal(*v.EndDate)
	})).Return(nil)

	mux.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created model.Voyage
	json.NewDecoder(resp.Body).Decode(&created)
	assert.NotNil(t, created.StartDate)
	assert.NotNil(t, created.EndDate)
	assert.True(t, created.StartDate.Equal(*created.EndDate), "day trip should round-trip with start_date == end_date")

	mockStore.AssertExpectations(t)
}

func TestUpdateVoyage_SameDayDayTrip(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /voyages/{id}", handler.UpdateVoyage)

	personID := int64(1)
	voyageID := int64(1)
	body := `{"title": "Sunday Sail", "start_date": "2026-07-19T00:00:00Z", "end_date": "2026-07-19T00:00:00Z"}`
	req := httptest.NewRequest("PUT", "/voyages/1", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)

	mockStore.On("UpdateVoyage", mock.MatchedBy(func(v *model.Voyage) bool {
		return v.ID == voyageID &&
			v.StartDate != nil && v.EndDate != nil &&
			v.StartDate.Equal(*v.EndDate)
	})).Return(nil)

	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestInterpolatePassagePoints_DayTripIsNoOp(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

	voyageID := int64(42)
	tripDate := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	landfalls := []model.Stop{
		{ID: 1, VoyageID: voyageID, TargetDate: tripDate, StopType: model.StopTypeLandfall, LocationName: "Home Marina"},
	}

	mockStore.On("ListLandfallStops", voyageID).Return(landfalls, nil)
	mockStore.On("DeletePassagePoints", voyageID).Return(nil)

	created, err := handler.InterpolatePassagePoints(context.Background(), voyageID)

	assert.NoError(t, err)
	assert.Empty(t, created)
	mockStore.AssertExpectations(t)
	mockStore.AssertNotCalled(t, "CreateStop", mock.Anything)
}
