package store_test

import (
	"context"
	"strings"
	"testing"

	"app/internal/model"
	"app/internal/store"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestCreateVoyageTrack_NamedQuery(t *testing.T) {
	tests := []struct {
		name                    string
		track                   model.VoyageTrack
		disallowColonSubstrings []string
	}{
		{
			name: "valid track binds without unexpanded colons in statement",
			track: model.VoyageTrack{
				VoyageID: 7,
				Kind:     "planned",
				Name:     "Test Track",
			},
			disallowColonSubstrings: []string{":interval", ":duration_interval"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := `
				INSERT INTO voyage_track (
					voyage_id, voyage_stop_id, kind, name, file_name,
					start_time, end_time, distance_nm, duration_interval,
					max_speed_kts, avg_speed_kts, geojson, simplified_geojson,
					raw_gpx, debrief, created_at, updated_at
				) VALUES (
					:voyage_id, :voyage_stop_id, :kind, :name, :file_name,
					:start_time, :end_time, :distance_nm,
					CAST(NULLIF(:duration_interval, '') AS interval),
					:max_speed_kts, :avg_speed_kts, :geojson, :simplified_geojson,
					:raw_gpx, :debrief, NOW(), NOW()
				)
				RETURNING id, created_at, updated_at`

			boundQuery, args, err := sqlx.Named(query, tt.track)
			if err != nil {
				t.Fatalf("unexpected error binding query: %v", err)
			}
			assert.NotEmpty(t, boundQuery)
			assert.NotEmpty(t, args)

			rebound := sqlx.Rebind(sqlx.DOLLAR, boundQuery)
			for _, disallowed := range tt.disallowColonSubstrings {
				assert.False(t, strings.Contains(rebound, disallowed), "rebound query must not contain %s", disallowed)
			}
			assert.Contains(t, rebound, "CAST(NULLIF(")
			assert.Contains(t, rebound, "AS interval)")
		})
	}
}

func TestCreateVoyageTrack_DB(t *testing.T) {
	dsn := "postgres://navalplan_user:navalplan_pass@localhost:5433/navalplan?sslmode=disable"
	db, err := store.New(dsn)
	if err != nil {
		t.Skip("skipping db integration test: database not reachable")
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skip("skipping db integration test: database ping failed")
	}

	var voyageID int64
	err = db.GetContext(ctx, &voyageID, "SELECT id FROM voyage LIMIT 1")
	if err != nil || voyageID == 0 {
		t.Skip("no voyages found, skipping live db test")
	}

	emptyInterval := ""
	validInterval := "02:30:00"

	tests := []struct {
		name     string
		interval *string
	}{
		{name: "nil duration interval", interval: nil},
		{name: "empty string duration interval", interval: &emptyInterval},
		{name: "valid interval string", interval: &validInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := &model.VoyageTrack{
				VoyageID:          voyageID,
				Kind:              "planned",
				Name:              "Integration Test Track - " + tt.name,
				DurationInterval:  tt.interval,
				GeoJSON:           model.RawJSON(`{"type":"FeatureCollection","features":[]}`),
				SimplifiedGeoJSON: model.RawJSON(`{"type":"FeatureCollection","features":[]}`),
			}

			err = db.CreateVoyageTrack(ctx, track)
			assert.NoError(t, err)
			assert.NotEmpty(t, track.ID)

			if track.ID != "" {
				_ = db.DeleteVoyageTrack(ctx, track.ID)
			}
		})
	}
}
