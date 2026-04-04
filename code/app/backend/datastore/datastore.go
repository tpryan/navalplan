package datastore

import (
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// DB wraps the sqlx.DB connection.
type DB struct {
	*sqlx.DB
}

// New creates a new database connection using the provided DSN.
func New(dsn string) (*DB, error) {
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &DB{db}, nil
}
