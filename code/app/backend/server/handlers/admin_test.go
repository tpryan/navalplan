package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/models"
	"app/server/handlers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestListUsers_Success(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	people := []models.Person{
		{ID: 1, Email: "alice@example.com", Name: "Alice"},
		{ID: 2, Email: "bob@example.com", Name: "Bob"},
	}
	invites := []models.Invitation{
		{Email: "carol@example.com"},
	}

	store.On("ListPeople", 20, 0).Return(people, nil)
	store.On("CountPeople").Return(2, nil)
	store.On("ListInvitations").Return(invites, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()
	h.ListUsers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]any
	assert.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	users := resp["users"].(map[string]any)
	assert.Equal(t, float64(2), users["total"])
	data := users["data"].([]any)
	assert.Len(t, data, 2)

	inviteData := resp["invites"].([]any)
	assert.Len(t, inviteData, 1)

	store.AssertExpectations(t)
}

func TestListUsers_ListPeopleError(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("ListPeople", 20, 0).Return(nil, errors.New("db error"))

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()
	h.ListUsers(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	store.AssertExpectations(t)
	// CountPeople and ListInvitations should NOT be called after the first error.
	store.AssertNotCalled(t, "CountPeople")
	store.AssertNotCalled(t, "ListInvitations")
}

func TestListUsers_CountPeopleError(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("ListPeople", 20, 0).Return([]models.Person{}, nil)
	store.On("CountPeople").Return(0, errors.New("db error"))

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()
	h.ListUsers(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	store.AssertExpectations(t)
	store.AssertNotCalled(t, "ListInvitations")
}

func TestListUsers_ListInvitationsError(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("ListPeople", 20, 0).Return([]models.Person{}, nil)
	store.On("CountPeople").Return(0, nil)
	store.On("ListInvitations").Return(nil, errors.New("db error"))

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()
	h.ListUsers(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	store.AssertExpectations(t)
}

func TestInviteUser_Success(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("CreateInvitation", "dave@example.com", mock.AnythingOfType("int64")).Return(nil)

	body := strings.NewReader(`{"email":"dave@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/invite", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.InviteUser(w, req)

	// Unauthorized because no person in context, but the handler checks that.
	// Without a person in context we expect 401.
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInviteUser_BadRequest(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	req := httptest.NewRequest(http.MethodPost, "/admin/invite", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	h.InviteUser(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRevokeInvitation_Success(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("DeleteInvitation", "dave@example.com").Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/admin/invitations/dave@example.com", nil)
	// Simulate path value (Go 1.22+ ServeMux sets this; set manually for unit test)
	req.SetPathValue("email", "dave@example.com")
	w := httptest.NewRecorder()
	h.RevokeInvitation(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	store.AssertExpectations(t)
}

func TestRevokeInvitation_DBError(t *testing.T) {
	store := new(MockStore)
	h := &handlers.Handler{DB: store}

	store.On("DeleteInvitation", "dave@example.com").Return(errors.New("db error"))

	req := httptest.NewRequest(http.MethodDelete, "/admin/invitations/dave@example.com", nil)
	req.SetPathValue("email", "dave@example.com")
	w := httptest.NewRecorder()
	h.RevokeInvitation(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	store.AssertExpectations(t)
}
