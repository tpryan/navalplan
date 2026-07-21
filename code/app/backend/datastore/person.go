package datastore

import (
	"context"
	"database/sql"

	"app/models"
)

func (db *DB) FindPersonByEmail(ctx context.Context, email string) (*models.Person, error) {
	var person models.Person
	query := `SELECT * FROM "person" WHERE email = $1`
	err := db.GetContext(ctx, &person, query, email)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) FindPersonByGoogleID(ctx context.Context, googleID string) (*models.Person, error) {
	var person models.Person
	query := `SELECT * FROM "person" WHERE google_id = $1`
	err := db.GetContext(ctx, &person, query, googleID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) GetPersonByID(ctx context.Context, id int64) (*models.Person, error) {
	var person models.Person
	query := `SELECT * FROM "person" WHERE id = $1`
	err := db.GetContext(ctx, &person, query, id)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string, invitedBy *int64, isAdmin bool) (*models.Person, error) {
	query := `
		INSERT INTO "person" (google_id, email, name, picture_url, invited_by, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, is_admin`

	var person models.Person
	person.GoogleID = googleID
	person.Email = email
	person.Name = name
	person.PictureURL = pictureURL
	person.InvitedBy = invitedBy
	person.IsAdmin = isAdmin

	err := db.QueryRowContext(ctx, query, googleID, email, name, pictureURL, invitedBy, isAdmin).Scan(&person.ID, &person.CreatedAt, &person.IsAdmin)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) SetAdminStatus(ctx context.Context, id int64, isAdmin bool) error {
	query := `UPDATE "person" SET is_admin = $1 WHERE id = $2`
	_, err := db.ExecContext(ctx, query, isAdmin, id)
	return err
}

func (db *DB) UpdatePersonName(ctx context.Context, id int64, name string) error {
	query := `UPDATE "person" SET name = $1 WHERE id = $2`
	_, err := db.ExecContext(ctx, query, name, id)
	return err
}
