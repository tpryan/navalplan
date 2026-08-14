package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"app/internal/model"
)

func (db *DB) ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]model.Stop, error) {
	stops := []model.Stop{}
	query := `
		SELECT id, voyage_id, target_date, stop_type, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes, created_at
		FROM stop
		WHERE voyage_id = $1
		ORDER BY target_date ASC`

	if limit > 0 {
		query += " LIMIT $2 OFFSET $3"
		err := db.SelectContext(ctx, &stops, query, voyageID, limit, offset)
		return stops, err
	}

	err := db.SelectContext(ctx, &stops, query, voyageID)
	return stops, err
}

func (db *DB) CreateStop(ctx context.Context, s *model.Stop) error {
	query := `
		INSERT INTO stop (voyage_id, target_date, stop_type, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes)
		VALUES (:voyage_id, :target_date, :stop_type, :location_name, :precise_location, :latitude, :longitude, :search_radius, :search_radius_unit, :notes)
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, s)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&s.ID, &s.CreatedAt)
	}
	return nil
}

func (db *DB) GetStop(ctx context.Context, id int64) (*model.Stop, error) {
	var s model.Stop
	query := `SELECT * FROM stop WHERE id = $1`
	err := db.GetContext(ctx, &s, query, id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) UpdateStop(ctx context.Context, s *model.Stop) error {
	query := `
		UPDATE stop
		SET target_date = :target_date, stop_type = :stop_type, location_name = :location_name, precise_location = :precise_location, latitude = :latitude,
		    longitude = :longitude, search_radius = :search_radius, search_radius_unit = :search_radius_unit, notes = :notes
		WHERE id = :id`
	_, err := db.NamedExecContext(ctx, query, s)
	return err
}

func (db *DB) ListLandfallStops(ctx context.Context, voyageID int64) ([]model.Stop, error) {
	stops := []model.Stop{}
	query := `
		SELECT id, voyage_id, target_date, stop_type, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes, created_at
		FROM stop
		WHERE voyage_id = $1 AND stop_type = $2
		ORDER BY target_date ASC`
	err := db.SelectContext(ctx, &stops, query, voyageID, model.StopTypeLandfall)
	return stops, err
}

func (db *DB) DeletePassagePoints(ctx context.Context, voyageID int64) error {
	query := `DELETE FROM stop WHERE voyage_id = $1 AND stop_type = $2`
	_, err := db.ExecContext(ctx, query, voyageID, model.StopTypePassagePoint)
	return err
}

func (db *DB) GetStopByDate(ctx context.Context, voyageID int64, date time.Time) (*model.Stop, error) {
	var s model.Stop
	query := `
		SELECT id, voyage_id, target_date, stop_type, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes, created_at
		FROM stop
		WHERE voyage_id = $1 AND target_date = $2`
	err := db.GetContext(ctx, &s, query, voyageID, date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (db *DB) DeleteStop(ctx context.Context, id int64) error {
	query := `DELETE FROM stop WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}
