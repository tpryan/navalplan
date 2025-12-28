package datastore

import (
	"app/models"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestListVoyages(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "title", "user_id", "start_date", "end_date", "search_radius", "search_radius_unit", "created_at"}).
		AddRow(1, "Voyage 1", 1, time.Now(), time.Now(), 60, "nm", time.Now()).
		AddRow(2, "Voyage 2", 1, time.Now(), time.Now(), 60, "nm", time.Now())

	query := `SELECT * FROM voyage WHERE user_id = $1 ORDER BY start_date DESC`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1).WillReturnRows(rows)

	voyages, err := db.ListVoyages(1)
	assert.NoError(t, err)
	assert.Len(t, voyages, 2)
	assert.Equal(t, "Voyage 1", voyages[0].Title)
}

func TestCreateVoyage(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	v := &models.Voyage{
		UserID:           1,
		Title:            "New Voyage",
		StartDate:        time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:          time.Date(2025, 7, 14, 0, 0, 0, 0, time.UTC),
		SearchRadius:     60,
		SearchRadiusUnit: "nm",
	}

	// NamedQuery in sqlx is tricky to mock exactly because it parses the query.
	// sqlmock sees the *result* of the parse (parameters replaced with ? or $1).
	// We use a regex to match the core parts.
	query := `INSERT INTO voyage`
	
	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(10, time.Now())
	
	mock.ExpectQuery(query).WillReturnRows(rows)

	err := db.CreateVoyage(v)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), v.ID)
}

func TestGetVoyage(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "title"}).AddRow(1, "My Voyage")
	query := `SELECT * FROM voyage WHERE id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1).WillReturnRows(rows)

	v, err := db.GetVoyage(1)
	assert.NoError(t, err)
	assert.Equal(t, "My Voyage", v.Title)
}

func TestUpdateVoyageSharing(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	query := `UPDATE voyage SET share_token = $1, is_public = $2 WHERE id = $3`
	token := "token"
	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(token, true, 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.UpdateVoyageSharing(1, &token, true)
	assert.NoError(t, err)
}

func TestGetVoyageByToken(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "title"}).AddRow(1, "Public Voyage")
	query := `SELECT * FROM voyage WHERE share_token = $1 AND is_public = true`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("abc").WillReturnRows(rows)

	v, err := db.GetVoyageByToken("abc")
	assert.NoError(t, err)
	assert.Equal(t, "Public Voyage", v.Title)
}

func TestDeleteVoyage(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	query := `DELETE FROM voyage WHERE id = $1`
	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.DeleteVoyage(1)
	assert.NoError(t, err)
}
