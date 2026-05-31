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

func TestListStops(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "location_name", "voyage_id"}).
		AddRow(1, "Stop 1", 1).
		AddRow(2, "Stop 2", 1)

	// We used explicit columns in implementation
	query := `SELECT id, voyage_id, target_date, stop_type, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes, created_at FROM stop WHERE voyage_id = $1 ORDER BY target_date ASC`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1).WillReturnRows(rows)

	stops, err := db.ListStops(context.Background(), 1, 0, 0)
	assert.NoError(t, err)
	assert.Len(t, stops, 2)
}

func TestCreateStop(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	s := &models.Stop{
		VoyageID:     1,
		LocationName: "New Stop",
		TargetDate:   time.Now(),
		Latitude:     48.0,
		Longitude:    -123.0,
	}

	query := `INSERT INTO stop`
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(10, time.Now())
	mock.ExpectQuery(query).WillReturnRows(rows)

	err := db.CreateStop(context.Background(), s)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), s.ID)
}

func TestGetStop(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "location_name"}).AddRow(1, "Stop 1")
	query := `SELECT * FROM stop WHERE id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1).WillReturnRows(rows)

	s, err := db.GetStop(context.Background(), 1)
	assert.NoError(t, err)
	assert.Equal(t, "Stop 1", s.LocationName)
}

func TestUpdateStop(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	s := &models.Stop{
		ID:           1,
		LocationName: "Updated Stop",
		TargetDate:   time.Now(),
	}

	// NamedExec uses query with params. sqlmock will see ? or $1
	query := `UPDATE stop SET`
	mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.UpdateStop(context.Background(), s)
	assert.NoError(t, err)
}

func TestDeleteStop(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	query := `DELETE FROM stop WHERE id = $1`
	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.DeleteStop(context.Background(), 1)
	assert.NoError(t, err)
}
