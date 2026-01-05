package datastore

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestFindPersonByGoogleID(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	googleID := "12345"
	query := `SELECT * FROM "person" WHERE google_id = $1`
	rows := sqlmock.NewRows([]string{"id", "google_id", "email", "name"}).
		AddRow(1, googleID, "test@example.com", "Test User")

	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(googleID).WillReturnRows(rows)

	person, err := db.FindPersonByGoogleID(context.Background(), googleID)
	assert.NoError(t, err)
	assert.NotNil(t, person)
	assert.Equal(t, googleID, person.GoogleID)
}

func TestGetPersonByID(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	id := int64(1)
	query := `SELECT * FROM "person" WHERE id = $1`
	rows := sqlmock.NewRows([]string{"id", "google_id"}).AddRow(id, "123")

	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id).WillReturnRows(rows)

	person, err := db.GetPersonByID(context.Background(), id)
	assert.NoError(t, err)
	assert.NotNil(t, person)
	assert.Equal(t, id, person.ID)
}

func TestCreatePerson(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	googleID := "g123"
	email := "test@example.com"
	name := "Tester"
	pic := "http://pic.url"

	query := `INSERT INTO "person" (google_id, email, name, picture_url) VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(1, time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(googleID, email, name, &pic).
		WillReturnRows(rows)

	person, err := db.CreatePerson(context.Background(), googleID, email, name, &pic)
	assert.NoError(t, err)
	assert.NotNil(t, person)
	assert.Equal(t, int64(1), person.ID)
	assert.Equal(t, email, person.Email)
}

func TestUpdatePersonName(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	id := int64(1)
	newName := "New Name"
	query := `UPDATE "person" SET name = $1 WHERE id = $2`

	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(newName, id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.UpdatePersonName(context.Background(), id, newName)
	assert.NoError(t, err)
}
