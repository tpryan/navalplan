package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/datastore"
	"app/models"
	"app/server/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/api/docs/v1"
)

type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListVoyages(personID int64) ([]models.Voyage, error) {
	args := m.Called(personID)
	return args.Get(0).([]models.Voyage), args.Error(1)
}

func (m *MockStore) CreateVoyage(v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) GetVoyage(id int64) (*models.Voyage, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Voyage), args.Error(1)
}

func (m *MockStore) UpdateVoyage(v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageSharing(id int64, shareToken *string, isPublic bool) error {
	args := m.Called(id, shareToken, isPublic)
	return args.Error(0)
}

func (m *MockStore) GetVoyageByToken(token string) (*models.Voyage, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Voyage), args.Error(1)
}

func (m *MockStore) DeleteVoyage(id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) ListStops(voyageID int64) ([]models.Stop, error) {
	args := m.Called(voyageID)
	return args.Get(0).([]models.Stop), args.Error(1)
}

func (m *MockStore) CreateStop(s *models.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) GetStop(id int64) (*models.Stop, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Stop), args.Error(1)
}

func (m *MockStore) UpdateStop(s *models.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) DeleteStop(id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) GetBriefing(stopID int64) (*models.Briefing, error) {
	args := m.Called(stopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Briefing), args.Error(1)
}

func (m *MockStore) CreateBriefing(b *models.Briefing) error {
	args := m.Called(b)
	return args.Error(0)
}

func (m *MockStore) GetVoyageGuide(voyageID int64) (*models.VoyageGuide, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.VoyageGuide), args.Error(1)
}

func (m *MockStore) CreateVoyageGuide(g *models.VoyageGuide) error {
	args := m.Called(g)
	return args.Error(0)
}

func (m *MockStore) FindPersonByGoogleID(googleID string) (*models.Person, error) {
	args := m.Called(googleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Person), args.Error(1)
}

func (m *MockStore) GetPersonByID(ctx context.Context, id int64) (*models.Person, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Person), args.Error(1)
}

func (m *MockStore) CreatePerson(googleID, email, name, pictureURL string) (*models.Person, error) {
	args := m.Called(googleID, email, name, pictureURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Person), args.Error(1)
}

func (m *MockStore) UpdatePersonName(id int64, name string) error {
	args := m.Called(id, name)
	return args.Error(0)
}

func (m *MockStore) CreateSession(token string, personID int64, expiresAt time.Time) error {
	args := m.Called(token, personID, expiresAt)
	return args.Error(0)
}

func (m *MockStore) GetSession(ctx context.Context, token string) (*models.Session, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Session), args.Error(1)
}

func (m *MockStore) DeleteSession(token string) error {
	args := m.Called(token)
	return args.Error(0)
}

var _ datastore.Store = (*MockStore)(nil)

type MockDocsService struct {
	mock.Mock
}

func (m *MockDocsService) Create(ctx context.Context, title string) (*docs.Document, error) {
	args := m.Called(ctx, title)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*docs.Document), args.Error(1)
}

func (m *MockDocsService) BatchUpdate(ctx context.Context, docID string, requests []*docs.Request) error {
	args := m.Called(ctx, docID, requests)
	return args.Error(0)
}

func TestListVoyages(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)

	personID := int64(1)
	expectedVoyages := []models.Voyage{
		{ID: 1, Title: "Test Voyage 1", PersonID: personID},
		{ID: 2, Title: "Test Voyage 2", PersonID: personID},
	}

	mockStore.On("ListVoyages", personID).Return(expectedVoyages, nil)

	req := httptest.NewRequest("GET", "/api/v1/voyages", nil)
	w := httptest.NewRecorder()

	handler.ListVoyages(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var voyages []models.Voyage
	json.NewDecoder(resp.Body).Decode(&voyages)
	assert.Equal(t, len(expectedVoyages), len(voyages))
	assert.Equal(t, expectedVoyages[0].Title, voyages[0].Title)

	mockStore.AssertExpectations(t)
}

func TestCreateVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)

	// We use strings.NewReader for the body
	body := `{"title": "New Voyage", "start_date": "2025-07-01T00:00:00Z", "end_date": "2025-07-14T00:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/v1/voyages", strings.NewReader(body))
	w := httptest.NewRecorder()

	// Capture the voyage passed to CreateVoyage to simulate ID assignment or just check args
	mockStore.On("CreateVoyage", mock.MatchedBy(func(v *models.Voyage) bool {
		return v.Title == "New Voyage" && v.PersonID == 1
	})).Return(nil)

	handler.CreateVoyage(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	mockStore.AssertExpectations(t)
}

func TestGetVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)

	voyageID := int64(123)
	expectedVoyage := &models.Voyage{ID: voyageID, Title: "My Voyage"}

	mockStore.On("GetVoyage", voyageID).Return(expectedVoyage, nil)

	// Need to setup chi context for URL params
	r := chi.NewRouter()
	r.Get("/voyages/{id}", handler.GetVoyage)

	req := httptest.NewRequest("GET", "/voyages/123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var v models.Voyage
	json.NewDecoder(resp.Body).Decode(&v)
	assert.Equal(t, expectedVoyage.Title, v.Title)

	mockStore.AssertExpectations(t)
}

func TestStopOperations(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Get("/voyages/{id}/stops", handler.ListStops)
	r.Post("/voyages/{id}/stops", handler.CreateStop)

	// Test ListStops
	voyageID := int64(1)
	expectedStops := []models.Stop{{ID: 10, LocationName: "Stop 1", VoyageID: voyageID}}
	mockStore.On("ListStops", voyageID).Return(expectedStops, nil)

	reqList := httptest.NewRequest("GET", "/voyages/1/stops", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)

	assert.Equal(t, http.StatusOK, wList.Code)

	// Test CreateStop
	body := `{"location_name": "New Stop", "latitude": 48.0, "longitude": -123.0, "target_date": "2025-07-02T00:00:00Z"}`
	reqCreate := httptest.NewRequest("POST", "/voyages/1/stops", strings.NewReader(body))
	wCreate := httptest.NewRecorder()

	mockStore.On("CreateStop", mock.MatchedBy(func(s *models.Stop) bool {
		return s.LocationName == "New Stop" && s.VoyageID == 1
	})).Return(nil)

	r.ServeHTTP(wCreate, reqCreate)
	mockStore.AssertExpectations(t)
}

func TestUpdateVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Put("/voyages/{id}", handler.UpdateVoyage)

	voyageID := int64(1)
	body := `{"title": "Updated Voyage"}`
	req := httptest.NewRequest("PUT", "/voyages/1", strings.NewReader(body))
	w := httptest.NewRecorder()

	mockStore.On("UpdateVoyage", mock.MatchedBy(func(v *models.Voyage) bool {
		return v.ID == voyageID && v.Title == "Updated Voyage"
	})).Return(nil)

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestDeleteVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Delete("/voyages/{id}", handler.DeleteVoyage)

	voyageID := int64(456)
	mockStore.On("DeleteVoyage", voyageID).Return(nil)

	req := httptest.NewRequest("DELETE", "/voyages/456", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestSharingOperations(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Post("/voyages/{id}/share", handler.EnableSharing)
	r.Delete("/voyages/{id}/share", handler.DisableSharing)
	r.Get("/public/voyages/{token}", handler.GetPublicVoyage)

	voyageID := int64(1)

	// Enable Sharing
	mockStore.On("UpdateVoyageSharing", voyageID, mock.AnythingOfType("*string"), true).Return(nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, IsPublic: true}, nil)

	reqEnable := httptest.NewRequest("POST", "/voyages/1/share", nil)
	wEnable := httptest.NewRecorder()
	r.ServeHTTP(wEnable, reqEnable)
	assert.Equal(t, http.StatusOK, wEnable.Code)

	// Get Public Voyage
	token := "some-token"
	mockStore.On("GetVoyageByToken", token).Return(&models.Voyage{ID: voyageID, Title: "Public Voyage"}, nil)

	reqPublic := httptest.NewRequest("GET", "/public/voyages/some-token", nil)
	wPublic := httptest.NewRecorder()
	r.ServeHTTP(wPublic, reqPublic)
	assert.Equal(t, http.StatusOK, wPublic.Code)

	mockStore.AssertExpectations(t)
}

func TestUpdateDeleteStop(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Put("/stops/{id}", handler.UpdateStop)
	r.Delete("/stops/{id}", handler.DeleteStop)

	stopID := int64(100)

	// Update
	body := `{"location_name": "Updated Stop"}`
	mockStore.On("UpdateStop", mock.MatchedBy(func(s *models.Stop) bool {
		return s.ID == stopID && s.LocationName == "Updated Stop"
	})).Return(nil)

	reqUpdate := httptest.NewRequest("PUT", "/stops/100", strings.NewReader(body))
	wUpdate := httptest.NewRecorder()
	r.ServeHTTP(wUpdate, reqUpdate)
	assert.Equal(t, http.StatusOK, wUpdate.Code)

	// Delete
	mockStore.On("DeleteStop", stopID).Return(nil)
	reqDelete := httptest.NewRequest("DELETE", "/stops/100", nil)
	wDelete := httptest.NewRecorder()
	r.ServeHTTP(wDelete, reqDelete)
	assert.Equal(t, http.StatusOK, wDelete.Code)

	mockStore.AssertExpectations(t)
}

func TestResearchBriefing(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Post("/stops/{id}/research", handler.TriggerResearch)
	r.Get("/stops/{id}/briefing", handler.GetBriefing)

	stopID := int64(10)

	// Trigger
	mockStore.On("GetStop", stopID).Return(&models.Stop{ID: stopID}, nil)
	reqTrigger := httptest.NewRequest("POST", "/stops/10/research", nil)
	wTrigger := httptest.NewRecorder()
	r.ServeHTTP(wTrigger, reqTrigger)
	assert.Equal(t, http.StatusAccepted, wTrigger.Code)

	// Get Briefing
	mockStore.On("GetBriefing", stopID).Return(&models.Briefing{ID: 1, StopID: stopID}, nil)
	reqGet := httptest.NewRequest("GET", "/stops/10/briefing", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	mockStore.AssertExpectations(t)
}

func TestDisableSharing(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Delete("/voyages/{id}/share", handler.DisableSharing)

	voyageID := int64(1)
	mockStore.On("UpdateVoyageSharing", voyageID, (*string)(nil), false).Return(nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, IsPublic: false}, nil)

	req := httptest.NewRequest("DELETE", "/voyages/1/share", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestTriggerFullVoyageResearch(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil)
	r := chi.NewRouter()
	r.Post("/voyages/{id}/research", handler.TriggerFullVoyageResearch)

	voyageID := int64(1)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID}, nil)
	mockStore.On("ListStops", voyageID).Return([]models.Stop{
		{ID: 10, LocationName: "Stop 1"},
		{ID: 11, LocationName: "Stop 2"},
	}, nil)

	req := httptest.NewRequest("POST", "/voyages/1/research", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	mockStore.AssertExpectations(t)
}
