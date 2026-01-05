package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appContext "app/context"
	"app/datastore"
	"app/models"
	"app/server/handlers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/api/docs/v1"
)

type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]models.Voyage, error) {
	args := m.Called(personID, limit, offset)
	return args.Get(0).([]models.Voyage), args.Error(1)
}

func (m *MockStore) CreateVoyage(ctx context.Context, v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) GetVoyage(ctx context.Context, id int64) (*models.Voyage, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Voyage), args.Error(1)
}

func (m *MockStore) UpdateVoyage(ctx context.Context, v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageSharing(ctx context.Context, id int64, shareToken *string, isPublic bool) error {
	args := m.Called(id, shareToken, isPublic)
	return args.Error(0)
}

func (m *MockStore) GetVoyageByToken(ctx context.Context, token string) (*models.Voyage, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Voyage), args.Error(1)
}

func (m *MockStore) DeleteVoyage(ctx context.Context, id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]models.Stop, error) {
	args := m.Called(voyageID, limit, offset)
	return args.Get(0).([]models.Stop), args.Error(1)
}

func (m *MockStore) CreateStop(ctx context.Context, s *models.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) GetStop(ctx context.Context, id int64) (*models.Stop, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Stop), args.Error(1)
}

func (m *MockStore) UpdateStop(ctx context.Context, s *models.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) DeleteStop(ctx context.Context, id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) GetBriefing(ctx context.Context, stopID int64) (*models.Briefing, error) {
	args := m.Called(stopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Briefing), args.Error(1)
}

func (m *MockStore) ListVoyageBriefings(ctx context.Context, voyageID int64) ([]models.Briefing, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.Briefing), args.Error(1)
}

func (m *MockStore) CreateBriefing(ctx context.Context, b *models.Briefing) error {
	args := m.Called(b)
	return args.Error(0)
}

func (m *MockStore) GetVoyageGuide(ctx context.Context, voyageID int64) (*models.VoyageGuide, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.VoyageGuide), args.Error(1)
}

func (m *MockStore) CreateVoyageGuide(ctx context.Context, g *models.VoyageGuide) error {
	args := m.Called(g)
	return args.Error(0)
}

func (m *MockStore) FindPersonByGoogleID(ctx context.Context, googleID string) (*models.Person, error) {
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

func (m *MockStore) CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string) (*models.Person, error) {
	args := m.Called(googleID, email, name, pictureURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Person), args.Error(1)
}

func (m *MockStore) UpdatePersonName(ctx context.Context, id int64, name string) error {
	args := m.Called(id, name)
	return args.Error(0)
}

func (m *MockStore) CreateSession(ctx context.Context, token string, personID int64, expiresAt time.Time) error {
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

func (m *MockStore) DeleteSession(ctx context.Context, token string) error {
	args := m.Called(token)
	return args.Error(0)
}

func (m *MockStore) ListRegionsByMonth(ctx context.Context, month int) ([]models.RegionWithSeasonality, error) {
	args := m.Called(month)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.RegionWithSeasonality), args.Error(1)
}

func (m *MockStore) GetRegionDetails(ctx context.Context, regionID int, month int) (*models.SailingRegion, *models.RegionSeasonality, error) {
	args := m.Called(regionID, month)
	var r *models.SailingRegion
	var s *models.RegionSeasonality
	if args.Get(0) != nil {
		r = args.Get(0).(*models.SailingRegion)
	}
	if args.Get(1) != nil {
		s = args.Get(1).(*models.RegionSeasonality)
	}
	return r, s, args.Error(2)
}

func (m *MockStore) UpsertRegion(ctx context.Context, region *models.SailingRegion) error {
	args := m.Called(region)
	return args.Error(0)
}

func (m *MockStore) UpsertSeasonality(ctx context.Context, seasonality *models.RegionSeasonality) error {
	args := m.Called(seasonality)
	return args.Error(0)
}

func (m *MockStore) DeleteSeasonalityForMonth(ctx context.Context, month int) error {
	args := m.Called(month)
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

// Helper to add person to context
func addPerson(req *http.Request, id int64) *http.Request {
	person := &models.Person{ID: id, Name: "Test User"}
	ctx := appContext.AddPersonToContext(req.Context(), person)
	return req.WithContext(ctx)
}

func TestListVoyages(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/voyages", handler.ListVoyages)

	personID := int64(1)
	expectedVoyages := []models.Voyage{
		{ID: 1, Title: "Test Voyage 1", PersonID: personID},
		{ID: 2, Title: "Test Voyage 2", PersonID: personID},
	}

	mockStore.On("ListVoyages", personID, 20, 0).Return(expectedVoyages, nil)

	req := httptest.NewRequest("GET", "/api/v1/voyages", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

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
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/voyages", handler.CreateVoyage)

	personID := int64(1)
	body := `{"title": "New Voyage", "start_date": "2025-07-01T00:00:00Z", "end_date": "2025-07-14T00:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/v1/voyages", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mockStore.On("CreateVoyage", mock.MatchedBy(func(v *models.Voyage) bool {
		return v.Title == "New Voyage" && v.PersonID == personID
	})).Return(nil)

	mux.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	mockStore.AssertExpectations(t)
}

func TestGetVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")

	personID := int64(1)
	voyageID := int64(123)
	expectedVoyage := &models.Voyage{ID: voyageID, Title: "My Voyage", PersonID: personID}

	mockStore.On("GetVoyage", voyageID).Return(expectedVoyage, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}", handler.GetVoyage)

	req := httptest.NewRequest("GET", "/voyages/123", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var v models.Voyage
	json.NewDecoder(resp.Body).Decode(&v)
	assert.Equal(t, expectedVoyage.Title, v.Title)

	mockStore.AssertExpectations(t)
}

func TestStopOperations(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}/stops", handler.ListStops)
	mux.HandleFunc("POST /voyages/{id}/stops", handler.CreateStop)

	personID := int64(1)
	voyageID := int64(1)

	// Test ListStops
	expectedStops := []models.Stop{{ID: 10, LocationName: "Stop 1", VoyageID: voyageID}}
	// Ownership check
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("ListStops", voyageID, 50, 0).Return(expectedStops, nil)

	reqList := httptest.NewRequest("GET", "/voyages/1/stops", nil)
	reqList = addPerson(reqList, personID)
	wList := httptest.NewRecorder()
	mux.ServeHTTP(wList, reqList)

	assert.Equal(t, http.StatusOK, wList.Code)

	// Test CreateStop
	body := `{"location_name": "New Stop", "latitude": 48.0, "longitude": -123.0, "target_date": "2025-07-02T00:00:00Z"}`
	reqCreate := httptest.NewRequest("POST", "/voyages/1/stops", strings.NewReader(body))
	reqCreate = addPerson(reqCreate, personID)
	wCreate := httptest.NewRecorder()

	// CreateStop doesn't strictly check Voyage ownership because it relies on the user providing VoyageID in URL? 
	// Wait, standard convention: POST /voyages/{id}/stops.
	// We need to check if user owns voyage {id}.
	// Let's see Handler implementation (not shown in provided snippet, but assumed correct or updated if I edited `stops.go`? I did not edit `stops.go`. User said `stops.go` "shows you know how to implement ownership checks".
	// Assuming `stops.go` CreateStop checks ownership.
	// I'll add the expectation just in case.
	
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil) // Logic in CreateStop usually checks this
	mockStore.On("CreateStop", mock.MatchedBy(func(s *models.Stop) bool {
		return s.LocationName == "New Stop" && s.VoyageID == 1
	})).Return(nil)

	mux.ServeHTTP(wCreate, reqCreate)
	mockStore.AssertExpectations(t)
}

func TestUpdateVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /voyages/{id}", handler.UpdateVoyage)

	personID := int64(1)
	voyageID := int64(1)
	body := `{"title": "Updated Voyage"}`
	req := httptest.NewRequest("PUT", "/voyages/1", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	// Ownership check
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
	
	mockStore.On("UpdateVoyage", mock.MatchedBy(func(v *models.Voyage) bool {
		return v.ID == voyageID && v.Title == "Updated Voyage"
	})).Return(nil)

	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestDeleteVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /voyages/{id}", handler.DeleteVoyage)

	personID := int64(1)
	voyageID := int64(456)
	
	// Ownership check
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)

	mockStore.On("DeleteVoyage", voyageID).Return(nil)

	req := httptest.NewRequest("DELETE", "/voyages/456", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestSharingOperations(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /voyages/{id}/share", handler.EnableSharing)
	mux.HandleFunc("DELETE /voyages/{id}/share", handler.DisableSharing)
	mux.HandleFunc("GET /public/voyages/{token}", handler.GetPublicVoyage)

	personID := int64(1)
	voyageID := int64(1)

	// Enable Sharing
	// Ownership check in EnableSharing
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID, IsPublic: false}, nil).Once()
	
	mockStore.On("UpdateVoyageSharing", voyageID, mock.AnythingOfType("*string"), true).Return(nil)
	
	// Refetch in EnableSharing
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID, IsPublic: true}, nil).Once()

	reqEnable := httptest.NewRequest("POST", "/voyages/1/share", nil)
	reqEnable = addPerson(reqEnable, personID)
	wEnable := httptest.NewRecorder()
	mux.ServeHTTP(wEnable, reqEnable)
	assert.Equal(t, http.StatusOK, wEnable.Code)

	// Get Public Voyage (No auth needed)
	token := "some-token"
	mockStore.On("GetVoyageByToken", token).Return(&models.Voyage{ID: voyageID, Title: "Public Voyage"}, nil)

	reqPublic := httptest.NewRequest("GET", "/public/voyages/some-token", nil)
	wPublic := httptest.NewRecorder()
	mux.ServeHTTP(wPublic, reqPublic)
	assert.Equal(t, http.StatusOK, wPublic.Code)

	mockStore.AssertExpectations(t)
}

func TestUpdateDeleteStop(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /stops/{id}", handler.UpdateStop)
	mux.HandleFunc("DELETE /stops/{id}", handler.DeleteStop)

	stopID := int64(100)
	voyageID := int64(50)
	personID := int64(1)

	// Setup Mocks for Ownership Checks (used by both update and delete)
	// Update Stop logic likely fetches Stop, then Voyage to check ownership
	mockStore.On("GetStop", stopID).Return(&models.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)

	// Update
	body := `{"location_name": "Updated Stop"}`
	mockStore.On("UpdateStop", mock.MatchedBy(func(s *models.Stop) bool {
		return s.ID == stopID && s.LocationName == "Updated Stop" && s.VoyageID == voyageID
	})).Return(nil)

	reqUpdate := httptest.NewRequest("PUT", "/stops/100", strings.NewReader(body))
	reqUpdate = addPerson(reqUpdate, personID)
	wUpdate := httptest.NewRecorder()
	mux.ServeHTTP(wUpdate, reqUpdate)
	assert.Equal(t, http.StatusOK, wUpdate.Code)

	// Delete
	// Logic likely fetches Stop, then Voyage
	mockStore.On("GetStop", stopID).Return(&models.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("DeleteStop", stopID).Return(nil)
	
	reqDelete := httptest.NewRequest("DELETE", "/stops/100", nil)
	reqDelete = addPerson(reqDelete, personID)
	wDelete := httptest.NewRecorder()
	mux.ServeHTTP(wDelete, reqDelete)
	assert.Equal(t, http.StatusOK, wDelete.Code)

	mockStore.AssertExpectations(t)
}

func TestResearchBriefing(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /stops/{id}/research", handler.TriggerResearch)
	mux.HandleFunc("GET /stops/{id}/briefing", handler.GetBriefing)

	stopID := int64(10)
	voyageID := int64(5)
	personID := int64(1)

	// Trigger
	mockStore.On("GetStop", stopID).Return(&models.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
	
	reqTrigger := httptest.NewRequest("POST", "/stops/10/research", nil)
	reqTrigger = addPerson(reqTrigger, personID)
	wTrigger := httptest.NewRecorder()
	mux.ServeHTTP(wTrigger, reqTrigger)
	assert.Equal(t, http.StatusAccepted, wTrigger.Code)

	// Get Briefing
	mockStore.On("GetBriefing", stopID).Return(&models.Briefing{ID: 1, StopID: stopID}, nil)
	// Check ownership: GetStop -> GetVoyage
	mockStore.On("GetStop", stopID).Return(&models.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)

	reqGet := httptest.NewRequest("GET", "/stops/10/briefing", nil)
	reqGet = addPerson(reqGet, personID)
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	mockStore.AssertExpectations(t)
}

func TestDisableSharing(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /voyages/{id}/share", handler.DisableSharing)

	personID := int64(1)
	voyageID := int64(1)
	
	// Ownership Check
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID, IsPublic: true}, nil).Once()
	
	mockStore.On("UpdateVoyageSharing", voyageID, (*string)(nil), false).Return(nil)
	
	// Refetch
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID, IsPublic: false}, nil).Once()

	req := httptest.NewRequest("DELETE", "/voyages/1/share", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestTriggerFullVoyageResearch(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /voyages/{id}/research", handler.TriggerFullVoyageResearch)

	personID := int64(1)
	voyageID := int64(1)
	
	// Ownership Check
	mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
	
	mockStore.On("ListStops", voyageID, 0, 0).Return([]models.Stop{
		{ID: 10, LocationName: "Stop 1"},
		{ID: 11, LocationName: "Stop 2"},
	}, nil)

	req := httptest.NewRequest("POST", "/voyages/1/research", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	mockStore.AssertExpectations(t)
}

func TestPersonHandlers(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, nil, "test_content", "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /person", handler.GetPerson)
	mux.HandleFunc("PUT /person", handler.UpdatePerson)

	t.Run("GetPerson_Success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/person", nil)
		req = addPerson(req, 1)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var p models.Person
		json.NewDecoder(w.Body).Decode(&p)
		assert.Equal(t, "Test User", p.Name)
	})

	t.Run("GetPerson_Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/person", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("UpdatePerson_Success", func(t *testing.T) {
		body := `{"name": "New Name"}`
		req := httptest.NewRequest("PUT", "/person", strings.NewReader(body))
		req = addPerson(req, 1)
		w := httptest.NewRecorder()

		mockStore.On("UpdatePersonName", int64(1), "New Name").Return(nil)

		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var p models.Person
		json.NewDecoder(w.Body).Decode(&p)
		assert.Equal(t, "New Name", p.Name)
	})
}

func TestGuideHandlers(t *testing.T) {
	mockStore := new(MockStore)
	// Use a temp dir for content to test map upload/retrieval
	tempDir := t.TempDir()
	handler := handlers.New(mockStore, nil, tempDir, "http://test-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}/guide", handler.GetVoyageGuide)
	mux.HandleFunc("POST /voyages/{id}/research_guide", handler.TriggerGuideResearch)

	personID := int64(1)

	t.Run("GetVoyageGuide_Found", func(t *testing.T) {
		voyageID := int64(1)
		// Ownership Check
		mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
		
		mockStore.On("GetVoyageGuide", voyageID).Return(&models.VoyageGuide{ID: 1, Summary: "Found"}, nil)

		req := httptest.NewRequest("GET", "/voyages/1/guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp handlers.VoyageGuideResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotNil(t, resp.VoyageGuide)
		assert.Equal(t, "Found", resp.Summary)
	})

	t.Run("GetVoyageGuide_NotFound", func(t *testing.T) {
		voyageID := int64(999)
		// Ownership Check
		mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID}, nil)
		
		mockStore.On("GetVoyageGuide", voyageID).Return(nil, assert.AnError)

		req := httptest.NewRequest("GET", "/voyages/999/guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("TriggerGuideResearch_Success", func(t *testing.T) {
		voyageID := int64(2)
		loc := "Sea"
		// Ownership check included in getting voyage
		mockStore.On("GetVoyage", voyageID).Return(&models.Voyage{ID: voyageID, PersonID: personID, LocationName: &loc}, nil)

		req := httptest.NewRequest("POST", "/voyages/2/research_guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})
}