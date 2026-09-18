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

type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]model.Voyage, error) {
	args := m.Called(personID, limit, offset)
	return args.Get(0).([]model.Voyage), args.Error(1)
}

func (m *MockStore) CreateVoyage(ctx context.Context, v *model.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) GetVoyage(ctx context.Context, id int64) (*model.Voyage, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Voyage), args.Error(1)
}

func (m *MockStore) UpdateVoyage(ctx context.Context, v *model.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageSharing(ctx context.Context, id int64, enable bool) (string, error) {
	args := m.Called(id, enable)
	return args.String(0), args.Error(1)
}

func (m *MockStore) UpdateVoyageCheckin(ctx context.Context, id int64, lat, lng float64, location string) error {
	args := m.Called(id, lat, lng, location)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageDates(ctx context.Context, id int64, start, end *time.Time) error {
	args := m.Called(id, start, end)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageConfig(ctx context.Context, id int64, radius int, unit string) error {
	args := m.Called(id, radius, unit)
	return args.Error(0)
}

func (m *MockStore) GetVoyageByToken(ctx context.Context, token string) (*model.Voyage, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Voyage), args.Error(1)
}

func (m *MockStore) DeleteVoyage(ctx context.Context, id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]model.Stop, error) {
	args := m.Called(voyageID, limit, offset)
	return args.Get(0).([]model.Stop), args.Error(1)
}

func (m *MockStore) ListLandfallStops(ctx context.Context, voyageID int64) ([]model.Stop, error) {
	args := m.Called(voyageID)
	return args.Get(0).([]model.Stop), args.Error(1)
}

func (m *MockStore) CreateStop(ctx context.Context, s *model.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) GetStop(ctx context.Context, id int64) (*model.Stop, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Stop), args.Error(1)
}

func (m *MockStore) GetStopByDate(ctx context.Context, voyageID int64, date time.Time) (*model.Stop, error) {
	args := m.Called(voyageID, date)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Stop), args.Error(1)
}

func (m *MockStore) DeletePassagePoints(ctx context.Context, voyageID int64) error {
	args := m.Called(voyageID)
	return args.Error(0)
}

func (m *MockStore) UpdateStop(ctx context.Context, s *model.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) DeleteStop(ctx context.Context, id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) GetBriefing(ctx context.Context, stopID int64) (*model.Briefing, error) {
	args := m.Called(stopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Briefing), args.Error(1)
}

func (m *MockStore) GetNearbyBriefing(ctx context.Context, lat, lng float64) (*model.Briefing, error) {
	args := m.Called(lat, lng)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Briefing), args.Error(1)
}

func (m *MockStore) ListVoyageBriefings(ctx context.Context, voyageID int64) ([]model.Briefing, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Briefing), args.Error(1)
}

func (m *MockStore) CreateBriefing(ctx context.Context, b *model.Briefing) error {
	args := m.Called(b)
	return args.Error(0)
}

func (m *MockStore) GetVoyageGuide(ctx context.Context, voyageID int64) (*model.VoyageGuide, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.VoyageGuide), args.Error(1)
}

func (m *MockStore) CreateVoyageGuide(ctx context.Context, g *model.VoyageGuide) error {
	args := m.Called(g)
	return args.Error(0)
}

func (m *MockStore) SaveVoyageMap(ctx context.Context, voyageID int64, data []byte) error {
	args := m.Called(voyageID, data)
	return args.Error(0)
}

func (m *MockStore) GetVoyageMap(ctx context.Context, voyageID int64) ([]byte, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockStore) ListVoyageRecommendations(ctx context.Context, voyageID int64) ([]model.VoyageRecommendation, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.VoyageRecommendation), args.Error(1)
}

func (m *MockStore) CreateVoyageRecommendation(ctx context.Context, r *model.VoyageRecommendation) error {
	args := m.Called(r)
	return args.Error(0)
}

func (m *MockStore) DeleteVoyageRecommendations(ctx context.Context, voyageID int64) error {
	args := m.Called(voyageID)
	return args.Error(0)
}

func (m *MockStore) FindPersonByGoogleID(ctx context.Context, googleID string) (*model.Person, error) {
	args := m.Called(googleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Person), args.Error(1)
}

func (m *MockStore) FindPersonByEmail(ctx context.Context, email string) (*model.Person, error) {
	args := m.Called(email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Person), args.Error(1)
}

func (m *MockStore) GetPersonByID(ctx context.Context, id int64) (*model.Person, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Person), args.Error(1)
}

func (m *MockStore) CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string, invitedBy *int64, isAdmin bool) (*model.Person, error) {
	args := m.Called(googleID, email, name, pictureURL, invitedBy, isAdmin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Person), args.Error(1)
}

func (m *MockStore) GetInvitation(ctx context.Context, email string) (*model.Invitation, error) {
	args := m.Called(email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Invitation), args.Error(1)
}

func (m *MockStore) CreateInvitation(ctx context.Context, email string, invitedBy int64, isAdmin bool) error {
	args := m.Called(email, invitedBy, isAdmin)
	return args.Error(0)
}

func (m *MockStore) DeleteInvitation(ctx context.Context, email string) error {
	args := m.Called(email)
	return args.Error(0)
}

func (m *MockStore) ListInvitations(ctx context.Context) ([]model.Invitation, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Invitation), args.Error(1)
}

func (m *MockStore) ListPeople(ctx context.Context, limit, offset int) ([]model.Person, error) {
	args := m.Called(limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Person), args.Error(1)
}

func (m *MockStore) CountPeople(ctx context.Context) (int, error) {
	args := m.Called()
	return args.Int(0), args.Error(1)
}

func (m *MockStore) UpdatePersonName(ctx context.Context, id int64, name string) error {
	args := m.Called(id, name)
	return args.Error(0)
}

func (m *MockStore) SetAdminStatus(ctx context.Context, id int64, isAdmin bool) error {
	args := m.Called(id, isAdmin)
	return args.Error(0)
}

func (m *MockStore) CreateSession(ctx context.Context, token string, personID int64, expiresAt time.Time) error {
	args := m.Called(token, personID, expiresAt)
	return args.Error(0)
}

func (m *MockStore) GetSession(ctx context.Context, token string) (*model.Session, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Session), args.Error(1)
}

func (m *MockStore) DeleteSession(ctx context.Context, token string) error {
	args := m.Called(token)
	return args.Error(0)
}

func (m *MockStore) ListRegionsByMonth(ctx context.Context, month int) ([]model.RegionWithSeasonality, error) {
	args := m.Called(month)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.RegionWithSeasonality), args.Error(1)
}

func (m *MockStore) GetRegionDetails(ctx context.Context, regionID int, month int) (*model.SailingRegion, *model.RegionSeasonality, error) {
	args := m.Called(regionID, month)
	var r *model.SailingRegion
	var s *model.RegionSeasonality
	if args.Get(0) != nil {
		r = args.Get(0).(*model.SailingRegion)
	}
	if args.Get(1) != nil {
		s = args.Get(1).(*model.RegionSeasonality)
	}
	return r, s, args.Error(2)
}

func (m *MockStore) UpsertRegion(ctx context.Context, region *model.SailingRegion) error {
	args := m.Called(region)
	return args.Error(0)
}

func (m *MockStore) UpsertSeasonality(ctx context.Context, seasonality *model.RegionSeasonality) error {
	args := m.Called(seasonality)
	return args.Error(0)
}

func (m *MockStore) DeleteSeasonalityForMonth(ctx context.Context, month int) error {
	args := m.Called(month)
	return args.Error(0)
}

func (m *MockStore) GetAllRegions(ctx context.Context) ([]model.SailingRegion, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.SailingRegion), args.Error(1)
}

func (m *MockStore) DeleteSeasonality(ctx context.Context, regionID int, month int) error {
	args := m.Called(regionID, month)
	return args.Error(0)
}

func (m *MockStore) ListAllFutureStops(ctx context.Context) ([]model.Stop, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Stop), args.Error(1)
}

func (m *MockStore) ListStopsInWindow(ctx context.Context, days int) ([]model.Stop, error) {
	args := m.Called(days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Stop), args.Error(1)
}

func (m *MockStore) UpsertSafetyAlerts(ctx context.Context, stopID int64, alerts model.RawJSON) error {
	args := m.Called(stopID, alerts)
	return args.Error(0)
}

func (m *MockStore) UpsertWeatherBriefing(ctx context.Context, stopID int64, weather model.WeatherSummary) error {
	args := m.Called(stopID, weather)
	return args.Error(0)
}

func (m *MockStore) CreateVoyageTrack(ctx context.Context, t *model.VoyageTrack) error {
	args := m.Called(t)
	return args.Error(0)
}

func (m *MockStore) GetVoyageTrack(ctx context.Context, id string) (*model.VoyageTrack, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.VoyageTrack), args.Error(1)
}

func (m *MockStore) ListVoyageTracks(ctx context.Context, voyageID int64) ([]model.VoyageTrack, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.VoyageTrack), args.Error(1)
}

func (m *MockStore) ListStopTracks(ctx context.Context, stopID int64) ([]model.VoyageTrack, error) {
	args := m.Called(stopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.VoyageTrack), args.Error(1)
}

func (m *MockStore) DeleteVoyageTrack(ctx context.Context, id string) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageTrackDebrief(ctx context.Context, id string, debrief model.RawJSON) error {
	args := m.Called(id, debrief)
	return args.Error(0)
}

var _ handlers.DBStore = (*MockStore)(nil)

func addPerson(req *http.Request, id int64) *http.Request {
	person := &model.Person{ID: id, Name: "Test User"}
	ctx := handlers.AddPersonToContext(req.Context(), person)
	return req.WithContext(ctx)
}

func TestListVoyages(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/voyages", handler.ListVoyages)

	personID := int64(1)
	expectedVoyages := []model.Voyage{
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

	var voyages []model.Voyage
	json.NewDecoder(resp.Body).Decode(&voyages)
	assert.Equal(t, len(expectedVoyages), len(voyages))
	assert.Equal(t, expectedVoyages[0].Title, voyages[0].Title)

	mockStore.AssertExpectations(t)
}

func TestCreateVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/voyages", handler.CreateVoyage)

	personID := int64(1)
	body := `{"title": "New Voyage", "start_date": "2025-07-01T00:00:00Z", "end_date": "2025-07-14T00:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/v1/voyages", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mockStore.On("CreateVoyage", mock.MatchedBy(func(v *model.Voyage) bool {
		return v.Title == "New Voyage" && v.PersonID == personID
	})).Return(nil)

	mux.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	mockStore.AssertExpectations(t)
}

func TestGetVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

	personID := int64(1)
	voyageID := int64(123)
	expectedVoyage := &model.Voyage{ID: voyageID, Title: "My Voyage", PersonID: personID}

	mockStore.On("GetVoyage", voyageID).Return(expectedVoyage, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}", handler.GetVoyage)

	req := httptest.NewRequest("GET", "/voyages/123", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var v model.Voyage
	json.NewDecoder(resp.Body).Decode(&v)
	assert.Equal(t, expectedVoyage.Title, v.Title)

	mockStore.AssertExpectations(t)
}

func TestStopOperations(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}/stops", handler.ListStops)
	mux.HandleFunc("POST /voyages/{id}/stops", handler.CreateStop)

	personID := int64(1)
	voyageID := int64(1)

	expectedStops := []model.Stop{{ID: 10, LocationName: "Stop 1", VoyageID: voyageID}}
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("ListStops", voyageID, 50, 0).Return(expectedStops, nil)

	reqList := httptest.NewRequest("GET", "/voyages/1/stops", nil)
	reqList = addPerson(reqList, personID)
	wList := httptest.NewRecorder()
	mux.ServeHTTP(wList, reqList)

	assert.Equal(t, http.StatusOK, wList.Code)

	body := `{"location_name": "New Stop", "latitude": 48.0, "longitude": -123.0, "target_date": "2025-07-02T00:00:00Z"}`
	reqCreate := httptest.NewRequest("POST", "/voyages/1/stops", strings.NewReader(body))
	reqCreate = addPerson(reqCreate, personID)
	wCreate := httptest.NewRecorder()

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("CreateStop", mock.MatchedBy(func(s *model.Stop) bool {
		return s.LocationName == "New Stop" && s.VoyageID == 1
	})).Return(nil)

	mux.ServeHTTP(wCreate, reqCreate)
	mockStore.AssertExpectations(t)
}

func TestUpdateVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /voyages/{id}", handler.UpdateVoyage)

	personID := int64(1)
	voyageID := int64(1)
	body := `{"title": "Updated Voyage"}`
	req := httptest.NewRequest("PUT", "/voyages/1", strings.NewReader(body))
	req = addPerson(req, personID)
	w := httptest.NewRecorder()

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)

	mockStore.On("UpdateVoyage", mock.MatchedBy(func(v *model.Voyage) bool {
		return v.ID == voyageID && v.Title == "Updated Voyage"
	})).Return(nil)

	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestDeleteVoyage(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /voyages/{id}", handler.DeleteVoyage)

	personID := int64(1)
	voyageID := int64(456)

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
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
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /voyages/{id}/share", handler.EnableSharing)
	mux.HandleFunc("DELETE /voyages/{id}/share", handler.DisableSharing)
	mux.HandleFunc("GET /public/voyages/{token}", handler.GetPublicVoyage)

	personID := int64(1)
	voyageID := int64(1)

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID, IsPublic: false}, nil).Once()
	mockStore.On("UpdateVoyageSharing", voyageID, true).Return("new-token", nil)

	reqEnable := httptest.NewRequest("POST", "/voyages/1/share", nil)
	reqEnable = addPerson(reqEnable, personID)
	wEnable := httptest.NewRecorder()
	mux.ServeHTTP(wEnable, reqEnable)
	assert.Equal(t, http.StatusOK, wEnable.Code)

	token := "some-token"
	mockStore.On("GetVoyageByToken", token).Return(&model.Voyage{ID: voyageID, Title: "Public Voyage"}, nil)

	reqPublic := httptest.NewRequest("GET", "/public/voyages/some-token", nil)
	wPublic := httptest.NewRecorder()
	mux.ServeHTTP(wPublic, reqPublic)
	assert.Equal(t, http.StatusOK, wPublic.Code)

	mockStore.AssertExpectations(t)
}

func TestUpdateDeleteStop(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /stops/{id}", handler.UpdateStop)
	mux.HandleFunc("DELETE /stops/{id}", handler.DeleteStop)

	stopID := int64(100)
	voyageID := int64(50)
	personID := int64(1)

	mockStore.On("GetStop", stopID).Return(&model.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)

	body := `{"location_name": "Updated Stop"}`
	mockStore.On("UpdateStop", mock.MatchedBy(func(s *model.Stop) bool {
		return s.ID == stopID && s.LocationName == "Updated Stop" && s.VoyageID == voyageID
	})).Return(nil)

	reqUpdate := httptest.NewRequest("PUT", "/stops/100", strings.NewReader(body))
	reqUpdate = addPerson(reqUpdate, personID)
	wUpdate := httptest.NewRecorder()
	mux.ServeHTTP(wUpdate, reqUpdate)
	assert.Equal(t, http.StatusOK, wUpdate.Code)

	mockStore.On("GetStop", stopID).Return(&model.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
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
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /stops/{id}/research", handler.TriggerResearch)
	mux.HandleFunc("GET /stops/{id}/briefing", handler.GetBriefing)

	stopID := int64(10)
	voyageID := int64(5)
	personID := int64(1)

	mockStore.On("GetStop", stopID).Return(&model.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("ListStops", voyageID, 0, 0).Return([]model.Stop{}, nil).Maybe()
	mockStore.On("GetNearbyBriefing", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mockStore.On("CreateBriefing", mock.Anything, mock.Anything).Return(nil).Maybe()

	reqTrigger := httptest.NewRequest("POST", "/stops/10/research", nil)
	reqTrigger = addPerson(reqTrigger, personID)
	wTrigger := httptest.NewRecorder()
	mux.ServeHTTP(wTrigger, reqTrigger)
	assert.Equal(t, http.StatusAccepted, wTrigger.Code)

	mockStore.On("GetBriefing", stopID).Return(&model.Briefing{ID: 1, StopID: stopID}, nil)
	mockStore.On("GetStop", stopID).Return(&model.Stop{ID: stopID, VoyageID: voyageID}, nil)
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)

	reqGet := httptest.NewRequest("GET", "/stops/10/briefing", nil)
	reqGet = addPerson(reqGet, personID)
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	mockStore.AssertExpectations(t)
}

func TestDisableSharing(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /voyages/{id}/share", handler.DisableSharing)

	personID := int64(1)
	voyageID := int64(1)

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID, IsPublic: true}, nil).Once()
	mockStore.On("UpdateVoyageSharing", voyageID, false).Return("", nil)

	req := httptest.NewRequest("DELETE", "/voyages/1/share", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockStore.AssertExpectations(t)
}

func TestTriggerFullVoyageResearch(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /voyages/{id}/research", handler.TriggerFullVoyageResearch)

	personID := int64(1)
	voyageID := int64(1)

	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)

	mockStore.On("ListStops", voyageID, 0, 0).Return([]model.Stop{
		{ID: 10, LocationName: "Stop 1"},
		{ID: 11, LocationName: "Stop 2"},
	}, nil)

	mockStore.On("ListStops", mock.Anything, 0, 0).Return([]model.Stop{
		{ID: 10, LocationName: "Stop 1"},
		{ID: 11, LocationName: "Stop 2"},
	}, nil).Maybe()
	mockStore.On("GetBriefing", mock.Anything).Return(nil, nil).Maybe()
	mockStore.On("GetNearbyBriefing", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mockStore.On("GetVoyageGuide", mock.Anything).Return(nil, assert.AnError).Maybe()
	mockStore.On("CreateBriefing", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockStore.On("CreateVoyageGuide", mock.Anything, mock.Anything).Return(nil).Maybe()

	req := httptest.NewRequest("POST", "/voyages/1/research", nil)
	req = addPerson(req, personID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	mockStore.AssertExpectations(t)
}

func TestPersonHandlers(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /person", handler.GetPerson)
	mux.HandleFunc("PUT /person", handler.UpdatePerson)

	t.Run("GetPerson_Success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/person", nil)
		req = addPerson(req, 1)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var p model.Person
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

		var p model.Person
		json.NewDecoder(w.Body).Decode(&p)
		assert.Equal(t, "New Name", p.Name)
	})
}

func TestGuideHandlers(t *testing.T) {
	mockStore := new(MockStore)
	tempDir := t.TempDir()
	handler := handlers.New(mockStore, tempDir, "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /voyages/{id}/guide", handler.GetVoyageGuide)
	mux.HandleFunc("POST /voyages/{id}/research_guide", handler.TriggerGuideResearch)

	personID := int64(1)

	t.Run("GetVoyageGuide_Found", func(t *testing.T) {
		voyageID := int64(1)
		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetVoyageGuide", voyageID).Return(&model.VoyageGuide{ID: 1, Summary: "Found"}, nil)
		mockStore.On("GetVoyageMap", voyageID).Return([]byte("fake-image"), nil)

		req := httptest.NewRequest("GET", "/voyages/1/guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp handlers.VoyageGuideResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotNil(t, resp.Guide)
		assert.Equal(t, "Found", resp.Guide.Summary)
		assert.Contains(t, resp.MapURL, "/api/v1/voyages/1/map_image")
	})

	t.Run("GetVoyageGuide_NotFound", func(t *testing.T) {
		voyageID := int64(999)
		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetVoyageGuide", voyageID).Return(nil, assert.AnError)
		mockStore.On("GetVoyageMap", voyageID).Return(nil, assert.AnError)

		req := httptest.NewRequest("GET", "/voyages/999/guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("TriggerGuideResearch_Success", func(t *testing.T) {
		voyageID := int64(2)
		loc := "Sea"
		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID, LocationName: &loc}, nil)
		mockStore.On("GetVoyageGuide", voyageID).Return(nil, assert.AnError).Maybe()
		mockStore.On("CreateVoyageGuide", mock.Anything).Return(nil).Maybe()

		req := httptest.NewRequest("POST", "/voyages/2/research_guide", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})
}

func TestGetPilotReport(t *testing.T) {
	mockStore := new(MockStore)
	handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/voyages/{id}/pilot_report", handler.GetPilotReport)

	personID := int64(1)
	voyageID := int64(123)

	t.Run("Success", func(t *testing.T) {
		debriefJSON := `{"track_id":"trk-1","track_name":"Leg 1","recorded_distance_nm":12.5,"planned_distance_nm":10.0,"summary":"Great sail"}`
		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetVoyageGuide", voyageID).Return(&model.VoyageGuide{Summary: "Test Guide"}, nil)
		mockStore.On("ListVoyageRecommendations", voyageID).Return([]model.VoyageRecommendation{{Name: "Rec 1"}}, nil)
		mockStore.On("GetVoyageMap", voyageID).Return([]byte("fake-image"), nil)
		mockStore.On("ListStops", voyageID, 100, 0).Return([]model.Stop{}, nil)
		mockStore.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
			{ID: "trk-1", Name: "Leg 1", Debrief: model.RawJSON(debriefJSON)},
		}, nil)

		req := httptest.NewRequest("GET", "/api/v1/voyages/123/pilot_report", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var report model.PilotReport
		json.NewDecoder(w.Body).Decode(&report)
		assert.Equal(t, "Test Guide", report.Guide.Summary)
		assert.Equal(t, 1, len(report.Recommendations))
		assert.Equal(t, 1, len(report.Debriefs))
		assert.Equal(t, "Leg 1", report.Debriefs[0].TrackName)
		assert.Equal(t, 12.5, report.Debriefs[0].RecordedDistanceNM)
		mockStore.AssertExpectations(t)
	})

	t.Run("Associates debrief with starting stop", func(t *testing.T) {
		localMock := new(MockStore)
		localHandler := handlers.New(localMock, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
		localMux := http.NewServeMux()
		localMux.HandleFunc("GET /api/v1/voyages/{id}/pilot_report", localHandler.GetPilotReport)

		stop1 := model.Stop{ID: 10, LocationName: "Cowes"}
		stop2 := model.Stop{ID: 11, LocationName: "Newtown"}
		stop3 := model.Stop{ID: 12, LocationName: "Yarmouth"}
		dest11 := int64(11)
		dest12 := int64(12)
		debrief1JSON := `{"track_id":"trk-1","track_name":"Full Voyage - Leg 1: Cowes to Newtown","voyage_stop_id":11}`
		debrief2JSON := `{"track_id":"trk-2","track_name":"Full Voyage - Leg 2: Newtown to Yarmouth","voyage_stop_id":12}`

		localMock.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		localMock.On("GetVoyageGuide", voyageID).Return(&model.VoyageGuide{Summary: "Test Guide"}, nil)
		localMock.On("ListVoyageRecommendations", voyageID).Return([]model.VoyageRecommendation{}, nil)
		localMock.On("GetVoyageMap", voyageID).Return([]byte("fake-image"), nil)
		localMock.On("ListStops", voyageID, 100, 0).Return([]model.Stop{stop1, stop2, stop3}, nil)
		localMock.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
			{ID: "trk-1", Name: "Full Voyage - Leg 1: Cowes to Newtown", VoyageStopID: &dest11, Debrief: model.RawJSON(debrief1JSON)},
			{ID: "trk-2", Name: "Full Voyage - Leg 2: Newtown to Yarmouth", VoyageStopID: &dest12, Debrief: model.RawJSON(debrief2JSON)},
		}, nil)

		req := httptest.NewRequest("GET", "/api/v1/voyages/123/pilot_report", nil)
		req = addPerson(req, personID)
		w := httptest.NewRecorder()

		localMux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var report model.PilotReport
		json.NewDecoder(w.Body).Decode(&report)
		assert.Equal(t, 2, len(report.Debriefs))
		assert.NotNil(t, report.Debriefs[0].StartStopID)
		assert.Equal(t, int64(10), *report.Debriefs[0].StartStopID)
		assert.NotNil(t, report.Debriefs[1].StartStopID)
		assert.Equal(t, int64(11), *report.Debriefs[1].StartStopID)
		localMock.AssertExpectations(t)
	})
}

func TestCheckAgentHealth(t *testing.T) {
	tests := []struct {
		name          string
		agentStatus   int
		agentDelay    time.Duration
		emptyAgentURL bool
		expectedError bool
	}{
		{
			name:          "Empty Agent URL succeeds",
			emptyAgentURL: true,
			expectedError: false,
		},
		{
			name:          "Healthy agent returns no error",
			agentStatus:   http.StatusOK,
			expectedError: false,
		},
		{
			name:          "Unhealthy agent status returns error",
			agentStatus:   http.StatusInternalServerError,
			expectedError: true,
		},
		{
			name:          "Slow agent times out fast",
			agentDelay:    6 * time.Second,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockStore)
			var agentURL string
			if !tt.emptyAgentURL {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tt.agentDelay > 0 {
						time.Sleep(tt.agentDelay)
					}
					status := tt.agentStatus
					if status == 0 {
						status = http.StatusOK
					}
					w.WriteHeader(status)
				}))
				defer srv.Close()
				agentURL = srv.URL
			}

			h := handlers.New(mockStore, "test_content", agentURL, &agent.StaticResolver{BaseURL: agentURL})
			ctx := context.Background()

			err := h.CheckAgentHealth(ctx)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
