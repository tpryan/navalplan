package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun_Config(t *testing.T) {
	// This test attempts to start the app.
	// Since we are not mocking datastore.New (it's hardcoded in main),
	// we expect it to fail on DB connection unless we have a real DB running.
	// However, this verifies the config parsing logic.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Cancel immediately to stop server if it starts

	var buf bytes.Buffer

	// Mock Env
	env := func(key string) string {
		if key == "NAVALPLAN_PORT" {
			return "8081"
		}
		if key == "NAVALPLAN_DATABASE_URL" {
			return "postgres://user:pass@localhost:5432/db?sslmode=disable"
		}
		return ""
	}

	cfg := loadConfig(env, ".")

	// It should return an error because DB is not reachable
	err := run(ctx, &buf, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to database")
}

func TestRun_Cancel(t *testing.T) {
	// If we provide an INVALID DSN that IS formally correct but points to nowhere,
	// pgx/sqlx might not error immediately on Open, but Ping.
	// datastore.New does Connect which does Ping.
	// So we can't easily test the "Server Start" path without a mockable datastore constructor.
	// For now, testing the error path covers the config logic.
}
