package store

import (
	"context"
	"database/sql"
	"time"

	"app/internal/model"
)

var ErrInvitationNotFound = sql.ErrNoRows

func (db *DB) GetInvitation(ctx context.Context, email string) (*model.Invitation, error) {
	var invitation model.Invitation
	query := `SELECT * FROM invitation WHERE email = $1`
	err := db.GetContext(ctx, &invitation, query, email)
	if err == sql.ErrNoRows {
		return nil, ErrInvitationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &invitation, nil
}

func (db *DB) CreateInvitation(ctx context.Context, email string, invitedBy int64, isAdmin bool, expiresAt *time.Time) error {
	query := `INSERT INTO invitation (email, invited_by, is_admin, expires_at) VALUES ($1, $2, $3, $4)`
	_, err := db.ExecContext(ctx, query, email, invitedBy, isAdmin, expiresAt)
	return err
}

func (db *DB) DeleteInvitation(ctx context.Context, email string) error {
	query := `DELETE FROM invitation WHERE email = $1`
	_, err := db.ExecContext(ctx, query, email)
	return err
}

func (db *DB) ListInvitations(ctx context.Context) ([]model.Invitation, error) {
	var invitations []model.Invitation
	query := `SELECT * FROM invitation ORDER BY created_at DESC`
	err := db.SelectContext(ctx, &invitations, query)
	return invitations, err
}

// expiryFilter returns the SQL predicate selecting either expired or active
// (non-expired, including permanent) accounts.
func expiryFilter(expired bool) string {
	if expired {
		return `expires_at IS NOT NULL AND expires_at <= NOW()`
	}
	return `(expires_at IS NULL OR expires_at > NOW())`
}

// ListPeople returns a page of people, filtered to either expired or active
// accounts based on the expired flag.
func (db *DB) ListPeople(ctx context.Context, expired bool, limit, offset int) ([]model.Person, error) {
	var people []model.Person
	query := `SELECT * FROM person WHERE ` + expiryFilter(expired) + ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	err := db.SelectContext(ctx, &people, query, limit, offset)
	return people, err
}

// CountPeople counts people in either the expired or active set.
func (db *DB) CountPeople(ctx context.Context, expired bool) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM person WHERE ` + expiryFilter(expired)
	err := db.GetContext(ctx, &count, query)
	return count, err
}
