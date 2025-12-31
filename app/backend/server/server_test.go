package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

func (m *MockStore) ListVoyages(userID int64) ([]models.Voyage, error) {
	return nil, nil
}
func (m *MockStore) CreateVoyage(v *models.Voyage) error { return nil }
func (m *MockStore) UpdateVoyage(v *models.Voyage) error { return nil }
func (m *MockStore) GetVoyage(id int64) (*models.Voyage, error) { return nil, nil }
func (m *MockStore) UpdateVoyageSharing(id int64, shareToken *string, isPublic bool) error { return nil }
func (m *MockStore) GetVoyageByToken(token string) (*models.Voyage, error) { return nil, nil }
func (m *MockStore) DeleteVoyage(id int64) error { return nil }

func (m *MockStore) ListStops(voyageID int64) ([]models.Stop, error) { return nil, nil }
func (m *MockStore) CreateStop(s *models.Stop) error { return nil }
func (m *MockStore) GetStop(id int64) (*models.Stop, error) { return nil, nil }
func (m *MockStore) UpdateStop(s *models.Stop) error { return nil }
func (m *MockStore) DeleteStop(id int64) error { return nil }

func (m *MockStore) GetBriefing(stopID int64) (*models.Briefing, error) { return nil, nil }
func (m *MockStore) CreateBriefing(b *models.Briefing) error { return nil }
func (m *MockStore) GetVoyageGuide(voyageID int64) (*models.VoyageGuide, error) { return nil, nil }
func (m *MockStore) CreateVoyageGuide(g *models.VoyageGuide) error { return nil }

var _ datastore.Store = (*MockStore)(nil)

func TestServerHealth(t *testing.T) {
	mockStore := new(MockStore)
	srv, err := server.New(mockStore)
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
	srv, err := server.New(mockStore)
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
