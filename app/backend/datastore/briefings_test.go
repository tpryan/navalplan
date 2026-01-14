package datastore

import (
	"app/models"
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestGetBriefing(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "stop_id"}).AddRow(1, 10)
	query := `SELECT * FROM briefing WHERE stop_id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(10).WillReturnRows(rows)

	b, err := db.GetBriefing(context.Background(), 10)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), b.ID)
}

func TestCreateBriefing(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	b := &models.Briefing{
		StopID: 10,
	}

	query := `INSERT INTO briefing`
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(1, time.Now())
	mock.ExpectQuery(query).WillReturnRows(rows)

	err := db.CreateBriefing(context.Background(), b)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), b.ID)
}

func TestListVoyageBriefings(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "stop_id"}).
		AddRow(1, 10).
		AddRow(2, 11)

	query := `
		SELECT b.*
		FROM briefing b
		JOIN stop s ON b.stop_id = s.id
		WHERE s.voyage_id = $1
	`
	// Match whitespace flexibility
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(99).WillReturnRows(rows)

	briefings, err := db.ListVoyageBriefings(context.Background(), 99)
	assert.NoError(t, err)
	assert.Len(t, briefings, 2)
	assert.Equal(t, int64(1), briefings[0].ID)
	assert.Equal(t, int64(2), briefings[1].ID)
}

func TestGetNearbyBriefing(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "stop_id"}).AddRow(5, 50)

	lat, lng := 10.0, 20.0
	tolerance := 0.005

	query := `
		SELECT b.*
		FROM briefing b
		JOIN stop s ON b.stop_id = s.id
		WHERE s.latitude BETWEEN $1 - $3 AND $1 + $3
		  AND s.longitude BETWEEN $2 - $3 AND $2 + $3
		ORDER BY b.created_at DESC
		LIMIT 1
	`

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(lat, lng, tolerance).
		WillReturnRows(rows)

	b, err := db.GetNearbyBriefing(context.Background(), lat, lng)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), b.ID)
}
