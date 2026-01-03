package datastore

import (
	"context"
	"database/sql"

	"app/models"
)

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

func (db *DB) CreatePerson(ctx context.Context, googleID, email, name, pictureURL string) (*models.Person, error) {
	query := `
		INSERT INTO "person" (google_id, email, name, picture_url)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`

	var person models.Person
	person.GoogleID = googleID
	person.Email = email
	person.Name = name
	person.PictureURL = pictureURL

	err := db.QueryRowContext(ctx, query, googleID, email, name, pictureURL).Scan(&person.ID, &person.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) UpdatePersonName(ctx context.Context, id int64, name string) error {
	query := `UPDATE "person" SET name = $1 WHERE id = $2`
	_, err := db.ExecContext(ctx, query, name, id)
	return err
}
