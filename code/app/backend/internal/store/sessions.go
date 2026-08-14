package store

import (
	"context"
	"database/sql"
	"time"

	"app/internal/model"
)

func (db *DB) CreateSession(ctx context.Context, token string, personID int64, expiresAt time.Time) error {
	query := `
		INSERT INTO session (token, person_id, expires_at)
		VALUES ($1, $2, $3)`
	_, err := db.ExecContext(ctx, query, token, personID, expiresAt)
	return err
}

func (db *DB) GetSession(ctx context.Context, token string) (*model.Session, error) {
	var session model.Session
	query := `SELECT * FROM session WHERE token = $1 AND expires_at > NOW()`
	err := db.GetContext(ctx, &session, query, token)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (db *DB) DeleteSession(ctx context.Context, token string) error {
	query := `DELETE FROM session WHERE token = $1`
	_, err := db.ExecContext(ctx, query, token)
	return err
}
