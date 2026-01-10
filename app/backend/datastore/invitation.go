package datastore

import (
	"app/models"
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
	if err != nil {
		return nil, err
	}
	return &invitation, nil
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

func (db *DB) ListPeople(ctx context.Context, limit, offset int) ([]models.Person, error) {
	var people []models.Person
	query := `SELECT * FROM person ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	err := db.SelectContext(ctx, &people, query, limit, offset)
	return people, err
}

func (db *DB) CountPeople(ctx context.Context) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM person`
	err := db.GetContext(ctx, &count, query)
	return count, err
}
