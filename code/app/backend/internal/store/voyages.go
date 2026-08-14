package store

import (
	"context"
	"time"

	"app/internal/model"
)

func (db *DB) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]model.Voyage, error) {
	var voyages []model.Voyage
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, 
		       search_radius, search_radius_unit, share_token, is_public, checkin_latitude, checkin_longitude, checkin_location, checkin_at, created_at
		FROM voyage 
		WHERE person_id = $1 
		ORDER BY created_at DESC`

	if limit > 0 {
		query += " LIMIT $2 OFFSET $3"
		err := db.SelectContext(ctx, &voyages, query, personID, limit, offset)
		return voyages, err
	}

	err := db.SelectContext(ctx, &voyages, query, personID)
	return voyages, err
}

func (db *DB) CreateVoyage(ctx context.Context, v *model.Voyage) error {
	query := `
		INSERT INTO voyage (person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit)
		VALUES (:person_id, :title, :start_date, :end_date, :location_name, :precise_location, :latitude, :longitude, :search_radius, :search_radius_unit)
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, v)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&v.ID, &v.CreatedAt)
	}
	return nil
}

func (db *DB) UpdateVoyage(ctx context.Context, v *model.Voyage) error {
	query := `
		UPDATE voyage
		SET title = :title, start_date = :start_date, end_date = :end_date,
		    location_name = :location_name, precise_location = :precise_location, latitude = :latitude, longitude = :longitude,
		    search_radius = :search_radius, search_radius_unit = :search_radius_unit
		WHERE id = :id`
	_, err := db.NamedExecContext(ctx, query, v)
	return err
}

func (db *DB) UpdateVoyageSharing(ctx context.Context, id int64, enable bool) (string, error) {
	if enable {
		var token string
		query := `UPDATE voyage SET share_token = generate_voyage_share_token(), is_public = true WHERE id = $1 RETURNING share_token`
		err := db.QueryRowContext(ctx, query, id).Scan(&token)
		return token, err
	} else {
		query := `UPDATE voyage SET share_token = NULL, is_public = false WHERE id = $1`
		_, err := db.ExecContext(ctx, query, id)
		return "", err
	}
}

func (db *DB) GetVoyageByToken(ctx context.Context, token string) (*model.Voyage, error) {
	var v model.Voyage
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, 
		       search_radius, search_radius_unit, share_token, is_public, checkin_latitude, checkin_longitude, checkin_location, checkin_at, created_at
		FROM voyage 
		WHERE share_token = $1 AND is_public = true`
	err := db.GetContext(ctx, &v, query, token)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (db *DB) GetVoyage(ctx context.Context, id int64) (*model.Voyage, error) {
	var v model.Voyage
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, 
		       search_radius, search_radius_unit, share_token, is_public, checkin_latitude, checkin_longitude, checkin_location, checkin_at, created_at
		FROM voyage 
		WHERE id = $1`
	err := db.GetContext(ctx, &v, query, id)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (db *DB) UpdateVoyageDates(ctx context.Context, id int64, start, end *time.Time) error {
	query := `UPDATE voyage SET start_date = $1, end_date = $2 WHERE id = $3`
	_, err := db.ExecContext(ctx, query, start, end, id)
	return err
}

func (db *DB) UpdateVoyageConfig(ctx context.Context, id int64, radius int, unit string) error {
	query := `UPDATE voyage SET search_radius = $1, search_radius_unit = $2 WHERE id = $3`
	_, err := db.ExecContext(ctx, query, radius, unit, id)
	return err
}

func (db *DB) UpdateVoyageCheckin(ctx context.Context, id int64, lat, lng float64, location string) error {
	query := `UPDATE voyage SET checkin_latitude = $1, checkin_longitude = $2, checkin_location = $3, checkin_at = NOW() WHERE id = $4`
	_, err := db.ExecContext(ctx, query, lat, lng, location, id)
	return err
}

func (db *DB) DeleteVoyage(ctx context.Context, id int64) error {
	query := `DELETE FROM voyage WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}
