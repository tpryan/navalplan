package datastore

import (
	"app/models"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestGetVoyageGuide(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	voyageID := int64(1)
	query := `SELECT * FROM voyage_guide WHERE voyage_id = $1`
	rows := sqlmock.NewRows([]string{"id", "voyage_id", "summary"}).
		AddRow(10, voyageID, "Test Summary")

	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(voyageID).WillReturnRows(rows)

	g, err := db.GetVoyageGuide(voyageID)
	assert.NoError(t, err)
	assert.NotNil(t, g)
	assert.Equal(t, voyageID, g.VoyageID)
}

func TestCreateVoyageGuide(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	g := &models.VoyageGuide{
		VoyageID: 1,
		Summary:  "Summary",
	}

	// sqlx.NamedQuery expands arguments.
	// For "pgx" driver, it likely expands to $1, $2, etc.
	// We need to match the expanded query.
	// Since regex matching is used, we can be a bit loose or precise.
	// The query uses :param syntax which sqlx replaces.

	// Note: sqlmock with sqlx NamedQuery can be tricky.
	// sqlx prepares the query by replacing :names with $1, $2... and ordering args.
	// We'll match the base structure.

	query := `INSERT INTO voyage_guide`

	rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(100, time.Now())

	// We expect the query to contain the INSERT statement and return rows
	mock.ExpectQuery(query).
		WillReturnRows(rows)

	err := db.CreateVoyageGuide(g)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), g.ID)
}
