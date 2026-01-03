package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func (m *MockStore) ListVoyages(personID int64) ([]models.Voyage, error) {
	args := m.Called(personID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.Voyage), args.Error(1)
}
func (m *MockStore) CreateVoyage(v *models.Voyage) error {
	args := m.Called(v)
	return args.Error(0)
}
func (m *MockStore) UpdateVoyage(v *models.Voyage) error {
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
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
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
func (m *MockStore) ListVoyageBriefings(voyageID int64) ([]models.Briefing, error) {
	args := m.Called(voyageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.Briefing), args.Error(1)
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
	args := m.Called(ctx, id) // Use ctx in mock matching if strict, or match anything
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
	args := m.Called(ctx, token)
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

func TestServerHealth(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)
	assert.NotNil(t, srv)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Router.ServeHTTP(w, req)

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

	// Verify API route prefix exists
	req := httptest.NewRequest("GET", "/api/v1/voyages", nil)
	w := httptest.NewRecorder()

	// We expect the handler to run (which might return something or error, but not 404)
	// In the real handler, it calls DB, but our mock returns nil/nil.
	// ListVoyages expects returns. Let's make the mock strict if we were testing handlers,
	// but here we just want to ensure routing is wired.
	// Actually, ListVoyages will try to encode `nil` which is valid JSON "null".

	srv.Router.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestAuthMiddleware(t *testing.T) {
	mockStore := new(MockStore)
	cfg := &config.Config{ContentDir: "."}
	srv, _ := server.New(mockStore, cfg)

	// We need to define a route that uses the middleware.
	// The existing /api/v1/person uses it.
	
	t.Run("No Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/person", nil)
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Invalid Session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/person", nil)
		req.AddCookie(&http.Cookie{Name: "navalplan_session", Value: "invalid-token"})
		
		mockStore.On("GetSession", mock.Anything, "invalid-token").Return(nil, nil)

		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, req)
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
		srv.Router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
