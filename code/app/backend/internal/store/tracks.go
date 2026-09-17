package store

import (
	"context"

	"app/internal/model"
)

const trackColumns = `
	id, voyage_id, voyage_stop_id, kind, name, file_name,
	start_time, end_time, distance_nm,
	CAST(duration_interval AS text) AS duration_interval,
	max_speed_kts, avg_speed_kts,
	geojson, simplified_geojson, raw_gpx, debrief,
	created_at, updated_at
`

// CreateVoyageTrack inserts a new voyage track record.
func (db *DB) CreateVoyageTrack(ctx context.Context, t *model.VoyageTrack) error {
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

	rows, err := db.NamedQueryContext(ctx, query, t)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	}
	return nil
}

// GetVoyageTrack retrieves a single track by its UUID.
func (db *DB) GetVoyageTrack(ctx context.Context, id string) (*model.VoyageTrack, error) {
	var t model.VoyageTrack
	query := `SELECT ` + trackColumns + ` FROM voyage_track WHERE id = $1`
	if err := db.GetContext(ctx, &t, query, id); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListVoyageTracks retrieves all tracks for a voyage ordered by creation time.
func (db *DB) ListVoyageTracks(ctx context.Context, voyageID int64) ([]model.VoyageTrack, error) {
	var tracks []model.VoyageTrack
	query := `SELECT ` + trackColumns + ` FROM voyage_track WHERE voyage_id = $1 ORDER BY created_at ASC`
	if err := db.SelectContext(ctx, &tracks, query, voyageID); err != nil {
		return nil, err
	}
	return tracks, nil
}

// ListStopTracks retrieves all tracks associated with a specific stop.
func (db *DB) ListStopTracks(ctx context.Context, stopID int64) ([]model.VoyageTrack, error) {
	var tracks []model.VoyageTrack
	query := `SELECT ` + trackColumns + ` FROM voyage_track WHERE voyage_stop_id = $1 ORDER BY created_at ASC`
	if err := db.SelectContext(ctx, &tracks, query, stopID); err != nil {
		return nil, err
	}
	return tracks, nil
}

// DeleteVoyageTrack deletes a track by its UUID.
func (db *DB) DeleteVoyageTrack(ctx context.Context, id string) error {
	query := `DELETE FROM voyage_track WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}

// UpdateVoyageTrackDebrief updates the debrief JSON column for a track.
func (db *DB) UpdateVoyageTrackDebrief(ctx context.Context, id string, debrief model.RawJSON) error {
	query := `UPDATE voyage_track SET debrief = $2, updated_at = NOW() WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id, debrief)
	return err
}
