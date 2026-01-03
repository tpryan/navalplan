package datastore

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestCreateSession(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	token := "tok123"
	personID := int64(1)
	expiresAt := time.Now().Add(time.Hour)
	query := `INSERT INTO session (token, person_id, expires_at) VALUES ($1, $2, $3)`

	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(token, personID, expiresAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.CreateSession(context.Background(), token, personID, expiresAt)
	assert.NoError(t, err)
}

func TestGetSession(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	token := "tok123"
	query := `SELECT * FROM session WHERE token = $1 AND expires_at > NOW()`
	rows := sqlmock.NewRows([]string{"token", "person_id"}).AddRow(token, 1)

	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(token).WillReturnRows(rows)

	session, err := db.GetSession(context.Background(), token)
	assert.NoError(t, err)
	assert.NotNil(t, session)
	assert.Equal(t, token, session.Token)
}

func TestDeleteSession(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	token := "tok123"
	query := `DELETE FROM session WHERE token = $1`

	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(token).WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.DeleteSession(context.Background(), token)
	assert.NoError(t, err)
}
