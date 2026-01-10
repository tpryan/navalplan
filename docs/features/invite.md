Here is the Product Requirement Document (PRD) and Technical Implementation Guide for adding the Invite-Only Authentication & Admin System to **NavalPlan**, modeled after the **NavalLog** implementation.

### 1. Product Requirement Document (PRD)

**Feature:** Invite-Only Authentication & Admin System
**Status:** Draft

#### 1.1 Overview

Currently, NavalPlan allows any user with a valid Google Account to sign in, automatically creating a `person` record. This feature restricts access to **invited users only**. New users cannot sign in unless their email address has been pre-authorized by an Administrator.

#### 1.2 Requirements

**Functional**

1. **Restricted Sign-up:** Users attempting to sign in with Google for the first time will be rejected unless their email exists in an `invitation` allowlist.
2. **Administrator Role:** Specific users can be designated as "Admins".
3. **Admin Interface:** Admins can view current users and invite new users by email.
4. **Invitation Flow:**
* Admin enters `user@example.com`.
* System stores this email.
* When `user@example.com` logs in via Google, the system recognizes the invite, creates their account, links the inviter, and allows access.



**Technical**

1. Modify `person` table to support `is_admin` and `invited_by`.
2. Create an `invitation` table.
3. Update `auth.go` to check the invitation list before creating new users.
4. Create Admin-only API endpoints for managing users and invites.

#### 1.3 Database Schema Changes

**Modify `person` Table**

```sql
ALTER TABLE person ADD COLUMN is_admin BOOLEAN DEFAULT FALSE;
ALTER TABLE person ADD COLUMN invited_by INTEGER REFERENCES person(id);

```

**Create `invitation` Table**

```sql
CREATE TABLE invitation (
    email VARCHAR(255) PRIMARY KEY,
    invited_by INTEGER REFERENCES person(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

```

---

### 2. Technical Implementation Guide

This guide details the specific code changes required in the `navalplan` repository.

#### Step 1: Database Migrations

Create a new migration file (e.g., `app/db/migrations/00000X_add_invitations.up.sql`) or update `schema.sql` if you are still in early development.

```sql
-- Add admin and invitation tracking to person
ALTER TABLE person ADD COLUMN is_admin BOOLEAN DEFAULT FALSE;
ALTER TABLE person ADD COLUMN invited_by INTEGER REFERENCES person(id);

-- Create invitation table
CREATE TABLE invitation (
    email VARCHAR(255) PRIMARY KEY,
    invited_by INTEGER REFERENCES person(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

```

#### Step 2: Datastore Updates (`app/backend/datastore`)

**2.1 Update `models/models.go**`
Add the new fields to the `Person` struct and create an `Invitation` struct.

```go
type Person struct {
    // ... existing fields ...
    IsAdmin   bool   `db:"is_admin" json:"is_admin"`
    InvitedBy *int64 `db:"invited_by" json:"-"`
}

type Invitation struct {
    Email     string    `db:"email" json:"email"`
    InvitedBy *int64    `db:"invited_by" json:"invited_by"`
    CreatedAt time.Time `db:"created_at" json:"created_at"`
}

```

**2.2 Update `CreatePerson` in `person.go**`
Modify the signature to accept the `invitedBy` ID.

```go
// app/backend/datastore/person.go

func (db *DB) CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string, invitedBy *int64) (*models.Person, error) {
	query := `
		INSERT INTO "person" (google_id, email, name, picture_url, invited_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, is_admin` // Return is_admin for completeness

	var person models.Person
	person.GoogleID = googleID
	person.Email = email
	person.Name = name
	person.PictureURL = pictureURL
    person.InvitedBy = invitedBy

	err := db.QueryRowContext(ctx, query, googleID, email, name, pictureURL, invitedBy).Scan(&person.ID, &person.CreatedAt, &person.IsAdmin)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

```

**2.3 Create `invitation.go**`
Add methods to manage invitations.

```go
// app/backend/datastore/invitation.go
package datastore

import (
	"app/backend/models" // Adjust import path based on your project structure
	"context"
	"database/sql"
)

var ErrInvitationNotFound = sql.ErrNoRows

func (db *DB) GetInvitation(ctx context.Context, email string) (*models.Invitation, error) {
	var invitation models.Invitation
	query := `SELECT * FROM invitation WHERE email = $1`
	err := db.GetContext(ctx, &invitation, query, email)
	if err == sql.ErrNoRows {
		return nil, ErrInvitationNotFound
	}
	return &invitation, err
}

func (db *DB) CreateInvitation(ctx context.Context, email string, invitedBy int64) error {
	query := `INSERT INTO invitation (email, invited_by) VALUES ($1, $2)`
	_, err := db.ExecContext(ctx, query, email, invitedBy)
	return err
}

func (db *DB) DeleteInvitation(ctx context.Context, email string) error {
	query := `DELETE FROM invitation WHERE email = $1`
	_, err := db.ExecContext(ctx, query, email)
	return err
}

func (db *DB) ListInvitations(ctx context.Context) ([]models.Invitation, error) {
	var invitations []models.Invitation
	query := `SELECT * FROM invitation ORDER BY created_at DESC`
	err := db.SelectContext(ctx, &invitations, query)
	return invitations, err
}

// Add ListPeople to support the Admin UI
func (db *DB) ListPeople(ctx context.Context) ([]models.Person, error) {
    var people []models.Person
    query := `SELECT * FROM person ORDER BY created_at DESC`
    err := db.SelectContext(ctx, &people, query)
    return people, err
}

```

#### Step 3: Update Authentication Logic (`app/backend/server/auth.go`)

Modify `oauthGoogleCallback` to enforce the invitation check.

```go
// Inside oauthGoogleCallback...

	// Find or Create Person
	person, err := s.DB.FindPersonByGoogleID(r.Context(), gUser.ID)
	if err != nil {
		log.Error("db find person", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if person == nil {
        // --- NEW LOGIC START ---
        // 1. Check for invitation
        invitation, err := s.DB.GetInvitation(r.Context(), gUser.Email)
        if err != nil {
            if err == datastore.ErrInvitationNotFound {
                 log.Info("uninvited user attempted to log in", "email", gUser.Email)
                 // Redirect to a static unauthorized page
                 http.Redirect(w, r, "/unauthorized.html", http.StatusTemporaryRedirect) 
                 return
            }
            log.Error("failed to get invitation", "error", err)
            http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
            return
        }

        // 2. Create Person with InvitedBy ID
		person, err = s.DB.CreatePerson(r.Context(), gUser.ID, gUser.Email, gUser.Name, &gUser.Picture, invitation.InvitedBy)
		if err != nil {
			log.Error("db create person", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

        // 3. Cleanup Invitation
        if err := s.DB.DeleteInvitation(r.Context(), gUser.Email); err != nil {
            log.Error("failed to delete invitation", "email", gUser.Email, "error", err)
        }
        // --- NEW LOGIC END ---
	}

    // ... existing session creation code ...

```

#### Step 4: Admin Handlers (`app/backend/server/handlers/admin.go`)

Create new handlers for the admin interface.

```go
package handlers

import (
	"encoding/json"
	"net/http"
    "app/backend/datastore"
    "app/backend/context" // wrapper for getting person from context
)

type AdminHandler struct {
    Store *datastore.DB // Or your Interface
}

// Structure for the UI
type AdminUserView struct {
    ID      int64  `json:"id,omitempty"`
    Email   string `json:"email"`
    Name    string `json:"name,omitempty"`
    IsAdmin bool   `json:"is_admin"`
    Invited bool   `json:"invited"`
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
    people, _ := h.Store.ListPeople(r.Context())
    invites, _ := h.Store.ListInvitations(r.Context())

    var view []AdminUserView
    
    for _, p := range people {
        view = append(view, AdminUserView{
            ID: p.ID, Email: p.Email, Name: p.Name, IsAdmin: p.IsAdmin, Invited: false,
        })
    }
    for _, i := range invites {
        view = append(view, AdminUserView{
            Email: i.Email, Invited: true,
        })
    }
    
    json.NewEncoder(w).Encode(map[string]interface{}{"users": view})
}

func (h *AdminHandler) InviteUser(w http.ResponseWriter, r *http.Request) {
    var req struct { Email string `json:"email"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Bad Request", http.StatusBadRequest)
        return
    }

    currentUser, _ := context.GetPersonFromContext(r) // Assuming you have this helper
    
    if err := h.Store.CreateInvitation(r.Context(), req.Email, currentUser.ID); err != nil {
        http.Error(w, "Failed to create invitation", http.StatusInternalServerError)
        return
    }
    
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"message": "invited"}`))
}

func (h *AdminHandler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
    // extract email from path (e.g., using Go 1.22 routing)
    email := r.PathValue("email") 
    h.Store.DeleteInvitation(r.Context(), email)
    w.WriteHeader(http.StatusOK)
}

```

#### Step 5: Middleware & Routes (`app/backend/server/server.go`)

1. **Middleware:** Create a `requireAdmin` middleware.
2. **Routes:** Register the new endpoints.

```go
// Middleware
func (s *Server) requireAdmin(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        person, err := s.getPersonFromContext(r) // Assuming you have a way to retrieve the user
        if err != nil || !person.IsAdmin {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// In RegisterRoutes or similar
func (s *Server) RegisterRoutes() {
    // ... existing routes ...
    
    adminHandler := &handlers.AdminHandler{Store: s.DB}
    
    s.Mux.Handle("GET /api/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(adminHandler.ListUsers))))
    s.Mux.Handle("POST /api/admin/invite", s.requireAuth(s.requireAdmin(http.HandlerFunc(adminHandler.InviteUser))))
    s.Mux.Handle("DELETE /api/admin/invite/{email}", s.requireAuth(s.requireAdmin(http.HandlerFunc(adminHandler.RevokeInvitation))))
}

```

#### Step 6: Bootstrapping

**Crucial Step:** Before deploying these changes to production, you must manually promote yourself to Admin, otherwise, no one can invite anyone.

Run this SQL command against your database:

```sql
UPDATE person SET is_admin = true WHERE email = 'your-email@gmail.com';

```