package datastore

import (
	"context"

	"app/models"
)

// ListVoyages retrieves all voyages for a given person, ordered by start date descending.
func (db *DB) ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]models.Voyage, error) {
	var voyages []models.Voyage
	// Explicit selection for performance (Issue #5)
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, share_token, is_public, created_at
		FROM voyage 
		WHERE person_id = $1 
		ORDER BY start_date DESC`

	if limit > 0 {
		query += " LIMIT $2 OFFSET $3"
		err := db.SelectContext(ctx, &voyages, query, personID, limit, offset)
		return voyages, err
	}

	err := db.SelectContext(ctx, &voyages, query, personID)
	return voyages, err
}

// CreateVoyage inserts a new voyage into the database.
func (db *DB) CreateVoyage(ctx context.Context, v *models.Voyage) error {
	query := `
		INSERT INTO voyage (person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, search_radius, search_radius_unit)
		VALUES (:person_id, :title, :start_date, :end_date, :location_name, :precise_location, :latitude, :longitude, :search_radius, :search_radius_unit)
		RETURNING id, created_at`

	// Using NamedQueryContext requires a bit more work if sqlx version is old, but standard sqlx has it.
	// If sqlx doesn't have NamedQueryContext directly accessible easily on *DB, we can use PrepareNamedContext.
	// Checking godocs for sqlx: NamedQueryContext exists on *DB.
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

// UpdateVoyage updates an existing voyage.
func (db *DB) UpdateVoyage(ctx context.Context, v *models.Voyage) error {
	query := `
		UPDATE voyage
		SET title = :title, start_date = :start_date, end_date = :end_date,
		    location_name = :location_name, precise_location = :precise_location, latitude = :latitude, longitude = :longitude
		WHERE id = :id`
	_, err := db.NamedExecContext(ctx, query, v)
	return err
}

// UpdateVoyageSharing updates the sharing status and token of a voyage.
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

// GetVoyageByToken retrieves a public voyage using its share token.
func (db *DB) GetVoyageByToken(ctx context.Context, token string) (*models.Voyage, error) {
	var v models.Voyage
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, 
		       search_radius, search_radius_unit, share_token, is_public, created_at
		FROM voyage 
		WHERE share_token = $1 AND is_public = true`
	err := db.GetContext(ctx, &v, query, token)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetVoyage retrieves a voyage by its ID.
func (db *DB) GetVoyage(ctx context.Context, id int64) (*models.Voyage, error) {
	var v models.Voyage
	query := `
		SELECT id, person_id, title, start_date, end_date, location_name, precise_location, latitude, longitude, 
		       search_radius, search_radius_unit, share_token, is_public, created_at
		FROM voyage 
		WHERE id = $1`
	err := db.GetContext(ctx, &v, query, id)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DeleteVoyage removes a voyage from the database.
func (db *DB) DeleteVoyage(ctx context.Context, id int64) error {
	query := `DELETE FROM voyage WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}
