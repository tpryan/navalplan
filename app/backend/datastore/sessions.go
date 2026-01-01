package datastore

import (
	"database/sql"
	"time"

	"app/models"
)

func (db *DB) CreateSession(token string, personID int64, expiresAt time.Time) error {
	query := `
		INSERT INTO session (token, person_id, expires_at)
		VALUES ($1, $2, $3)`
	_, err := db.Exec(query, token, personID, expiresAt)
	return err
}

func (db *DB) GetSession(token string) (*models.Session, error) {
	var session models.Session
	query := `SELECT * FROM session WHERE token = $1 AND expires_at > NOW()`
	err := db.Get(&session, query, token)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (db *DB) DeleteSession(token string) error {
	query := `DELETE FROM session WHERE token = $1`
	_, err := db.Exec(query, token)
	return err
}
