package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app/internal/config"
	"app/internal/model"
	"app/internal/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]model.Voyage, error) {
	args := m.Called(personID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Voyage), args.Error(1)
}

func (m *MockStore) CreateVoyage(ctx context.Context, v *model.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyage(ctx context.Context, v *model.Voyage) error {
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

func (m *MockStore) UpdateVoyageSharing(ctx context.Context, id int64, enable bool) (string, error) {
	args := m.Called(id, enable)
	return args.String(0), args.Error(1)
}

func (m *MockStore) UpdateVoyageCheckin(ctx context.Context, id int64, lat, lng float64, location string) error {
	args := m.Called(id, lat, lng, location)
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

func (m *MockStore) UpdateVoyageDates(ctx context.Context, id int64, start, end *time.Time) error {
	args := m.Called(id, start, end)
	return args.Error(0)
}

func (m *MockStore) UpdateVoyageConfig(ctx context.Context, id int64, radius int, unit string) error {
	args := m.Called(id, radius, unit)
	return args.Error(0)
}

func (m *MockStore) ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]model.Stop, error) {
	args := m.Called(voyageID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
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

func (m *MockStore) UpdateStop(ctx context.Context, s *model.Stop) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *MockStore) DeleteStop(ctx context.Context, id int64) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockStore) ListLandfallStops(ctx context.Context, voyageID int64) ([]model.Stop, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]model.Stop), args.Error(1)
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
	args := m.Called(ctx, id)
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
	args := m.Called(ctx, token)
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

func (m *MockStore) CreateVoyageTrack(ctx context.Context, track *model.VoyageTrack) error {
	args := m.Called(track)
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

func TestServerHealth(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)
	assert.NotNil(t, srv)

	srv.Routes(cfg.ContentDir)

	tests := []struct {
		name         string
		endpoint     string
		expectedCode int
		expectedJSON string
	}{
		{
			name:         "healthz endpoint",
			endpoint:     "/healthz",
			expectedCode: http.StatusOK,
			expectedJSON: `{"status":"ok"}` + "\n",
		},
		{
			name:         "health endpoint",
			endpoint:     "/health",
			expectedCode: http.StatusOK,
			expectedJSON: `{"status":"ok"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.endpoint, nil)
			w := httptest.NewRecorder()

			srv.Mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			assert.Equal(t, tt.expectedJSON, w.Body.String())
		})
	}
}

func TestServerRoutes(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)

	srv.Routes(cfg.ContentDir)

	req := httptest.NewRequest("GET", "/api/v1/voyages", nil)
	w := httptest.NewRecorder()

	srv.Mux.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestAuthMiddleware(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{ContentDir: "."}
	srv, _ := server.New(mockStore, cfg)
	srv.Routes(cfg.ContentDir)

	t.Run("No Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/person", nil)
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Invalid Session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/person", nil)
		req.AddCookie(&http.Cookie{Name: "navalplan_session", Value: "invalid-token"})

		mockStore.On("GetSession", mock.Anything, "invalid-token").Return(nil, nil)

		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Valid Session", func(t *testing.T) {
		token := "valid-token"
		personID := int64(1)

		req := httptest.NewRequest("GET", "/api/v1/person", nil)
		req.AddCookie(&http.Cookie{Name: "navalplan_session", Value: token})

		mockStore.On("GetSession", mock.Anything, token).Return(&model.Session{Token: token, PersonID: personID}, nil)
		mockStore.On("GetPersonByID", mock.Anything, personID).Return(&model.Person{ID: personID, Name: "Test User"}, nil)

		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("System API Key", func(t *testing.T) {
		srv.SystemAPIKey = "test-system-key"

		req := httptest.NewRequest("POST", "/api/v1/discovery/mine?month=all", nil)
		req.Header.Set("Authorization", "Bearer test-system-key")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})
}

func TestStaticAndSPARouting(t *testing.T) {
	tmpDir := t.TempDir()

	indexContent := "<html>Index</html>"
	err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644)
	assert.NoError(t, err)

	cssContent := "body { color: red; }"
	err = os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte(cssContent), 0644)
	assert.NoError(t, err)

	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: tmpDir,
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)
	srv.Routes(cfg.ContentDir)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{"Root returns index", "/", http.StatusOK, indexContent},
		{"SPA route returns index", "/dashboard", http.StatusOK, indexContent},
		{"Deep SPA route returns index", "/users/123", http.StatusOK, indexContent},
		{"Discover route returns index", "/discover", http.StatusOK, indexContent},
		{"Discover month slug route returns index", "/discover/may", http.StatusOK, indexContent},
		{"Discover month number route returns index", "/discover/5", http.StatusOK, indexContent},
		{"Static file returns content", "/style.css", http.StatusOK, cssContent},
		{"Missing static file returns 404", "/missing.css", http.StatusNotFound, "404 page not found\n"},
		{"API 404 returns 404", "/api/v1/unknown", http.StatusNotFound, "404 page not found\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			srv.Mux.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
			if tc.expectedBody != "" {
				assert.Equal(t, tc.expectedBody, w.Body.String())
			}
		})
	}
}
