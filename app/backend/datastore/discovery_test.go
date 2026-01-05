package datastore

import (
	"context"
	"regexp"
	"testing"
	"time"

	"app/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestDiscovery(t *testing.T) {
	ctx := context.Background()

	t.Run("UpsertRegion", func(t *testing.T) {
		db, mock := mockDB(t)
		defer db.Close()

		region := &models.SailingRegion{
			Name:     "Chesapeake Bay",
			Geometry: models.RawJSON(`{"type": "Polygon"}`),
			Type:     "Coastal",
		}

		rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(1, time.Now())
		mock.ExpectQuery(`INSERT INTO sailing_regions`).WillReturnRows(rows)

		err := db.UpsertRegion(ctx, region)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), region.ID)
	})

	t.Run("UpsertSeasonality", func(t *testing.T) {
		db, mock := mockDB(t)
		defer db.Close()

		s := &models.RegionSeasonality{
			RegionID:         1,
			Month:            1,
			SuitabilityScore: 95,
		}

		rows := sqlmock.NewRows([]string{"id", "created_at"}).AddRow(100, time.Now())
		mock.ExpectQuery(`INSERT INTO region_seasonality`).WillReturnRows(rows)

		err := db.UpsertSeasonality(ctx, s)
		assert.NoError(t, err)
		assert.Equal(t, int64(100), s.ID)
	})

	t.Run("ListRegionsByMonth", func(t *testing.T) {
		db, mock := mockDB(t)
		defer db.Close()

		rows := sqlmock.NewRows([]string{"id", "name", "suitability_score", "is_hidden_gem", "tier", "summary", "deep_cut_reasoning", "avg_wind_speed_knots", "avg_temp_c"}).
			AddRow(1, "BVI", 90, false, "Standard", "Nice", "", 15, 25).
			AddRow(2, "Chesapeake", 80, true, "Hidden Gem", "Warm", "Hidden", 12, 28)

		query := `SELECT r.*, s.suitability_score, s.is_hidden_gem, s.tier, s.summary, s.deep_cut_reasoning, s.avg_wind_speed_knots, s.avg_temp_c FROM sailing_regions r JOIN region_seasonality s ON r.id = s.region_id WHERE s.month = $1 ORDER BY r.name ASC`
		mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(5).WillReturnRows(rows)

		regions, err := db.ListRegionsByMonth(ctx, 5)
		assert.NoError(t, err)
		assert.Len(t, regions, 2)
		assert.Equal(t, "BVI", regions[0].Name)
		assert.Equal(t, 90, regions[0].SuitabilityScore)
		assert.True(t, regions[1].IsHiddenGem)
		assert.Equal(t, "Standard", regions[0].Tier)
	})
}