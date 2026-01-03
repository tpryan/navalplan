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
