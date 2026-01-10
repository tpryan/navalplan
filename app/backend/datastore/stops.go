package datastore

import (
	"context"

	"app/models"
)

func (db *DB) ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]models.Stop, error) {
	stops := []models.Stop{}
	// Explicit columns to avoid over-fetching
	query := `
		SELECT id, voyage_id, target_date, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes, created_at
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

func (db *DB) CreateStop(ctx context.Context, s *models.Stop) error {
	query := `
		INSERT INTO stop (voyage_id, target_date, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit, notes)
		VALUES (:voyage_id, :target_date, :location_name, :precise_location, :latitude, :longitude, :search_radius, :search_radius_unit, :notes)
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

func (db *DB) GetStop(ctx context.Context, id int64) (*models.Stop, error) {
	var s models.Stop
	query := `SELECT * FROM stop WHERE id = $1`
	err := db.GetContext(ctx, &s, query, id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) UpdateStop(ctx context.Context, s *models.Stop) error {
	query := `
		UPDATE stop 
		SET target_date = :target_date, location_name = :location_name, precise_location = :precise_location, latitude = :latitude, 
		    longitude = :longitude, search_radius = :search_radius, search_radius_unit = :search_radius_unit, notes = :notes
		WHERE id = :id`
	_, err := db.NamedExecContext(ctx, query, s)
	return err
}

func (db *DB) DeleteStop(ctx context.Context, id int64) error {
	query := `DELETE FROM stop WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}
