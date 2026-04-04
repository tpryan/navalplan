package datastore

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func mockDB(t *testing.T) (*DB, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}

	// sqlx needs to know the driver to handle named queries correctly in some cases,
	// but for standard mocks "sqlmock" driver is usually fine if we don't use driver-specific syntax too heavily.
	// However, sqlx binds parameters differently ($1 vs ?) depending on driver.
	// Postgres uses $1. sqlmock defaults to ?.
	// We might need to handle this. sqlmock matches regex, so $1 is just text in the query.

	sqlxDB := sqlx.NewDb(db, "pgx")
	return &DB{sqlxDB}, mock
}
