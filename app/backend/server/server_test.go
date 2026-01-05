package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app/config"
	"app/datastore"
	"app/models"
	"app/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore for Server tests
type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]models.Voyage, error) {
	args := m.Called(personID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.Voyage), args.Error(1)
}
func (m *MockStore) CreateVoyage(ctx context.Context, v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}
func (m *MockStore) UpdateVoyage(ctx context.Context, v *models.Voyage) error {
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
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
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
	args := m.Called(ctx, id) // Use ctx in mock matching if strict, or match anything
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
	args := m.Called(ctx, token)
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

var _ datastore.Store = (*MockStore)(nil)

func TestServerHealth(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)
	assert.NotNil(t, srv)

	// Register routes
	srv.Routes(cfg.ContentDir)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "OK", w.Body.String())
}

func TestServerRoutes(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)

	// Register routes
	srv.Routes(cfg.ContentDir)

	// Verify API route prefix exists
	req := httptest.NewRequest("GET", "/api/v1/voyages", nil)
	w := httptest.NewRecorder()

	// We expect the handler to run (which might return something or error, but not 404)
	// In the real handler, it calls DB, but our mock returns nil/nil.
	// ListVoyages expects returns. Let's make the mock strict if we were testing handlers,
	// but here we just want to ensure routing is wired.
	// Actually, ListVoyages will try to encode `nil` which is valid JSON "null".

	srv.Mux.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestAuthMiddleware(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{ContentDir: "."}
	srv, _ := server.New(mockStore, cfg)
	srv.Routes(cfg.ContentDir)

	// We need to define a route that uses the middleware.
	// The existing /api/v1/person uses it.
	
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

		mockStore.On("GetSession", mock.Anything, token).Return(&models.Session{Token: token, PersonID: personID}, nil)
		mockStore.On("GetPersonByID", mock.Anything, personID).Return(&models.Person{ID: personID, Name: "Test User"}, nil)
		
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestStaticAndSPARouting(t *testing.T) {
	// Setup temporary static dir
	tmpDir := t.TempDir()

	// Create index.html
	indexContent := "<html>Index</html>"
	err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644)
	assert.NoError(t, err)

	// Create style.css
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
