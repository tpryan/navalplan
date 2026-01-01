package datastore

import (
	"database/sql"

	"app/models"
)

func (db *DB) FindPersonByGoogleID(googleID string) (*models.Person, error) {
	var person models.Person
	query := `SELECT * FROM "person" WHERE google_id = $1`
	err := db.Get(&person, query, googleID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) GetPersonByID(id int64) (*models.Person, error) {
	var person models.Person
	query := `SELECT * FROM "person" WHERE id = $1`
	err := db.Get(&person, query, id)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) CreatePerson(googleID, email, name, pictureURL string) (*models.Person, error) {
	query := `
		INSERT INTO "person" (google_id, email, name, picture_url)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`
	
	var person models.Person
	person.GoogleID = googleID
	person.Email = email
	person.Name = name
	person.PictureURL = pictureURL

	err := db.QueryRow(query, googleID, email, name, pictureURL).Scan(&person.ID, &person.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &person, nil
}

func (db *DB) UpdatePersonName(id int64, name string) error {
	query := `UPDATE "person" SET name = $1 WHERE id = $2`
	_, err := db.Exec(query, name, id)
	return err
}
