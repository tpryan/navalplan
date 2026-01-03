package datastore

import (
	"app/models"
)

// ListVoyages retrieves all voyages for a given person, ordered by start date descending.
func (db *DB) ListVoyages(personID int64) ([]models.Voyage, error) {
	var voyages []models.Voyage
	query := `SELECT * FROM voyage WHERE person_id = $1 ORDER BY start_date DESC`
	err := db.Select(&voyages, query, personID)
	return voyages, err
}

// CreateVoyage inserts a new voyage into the database.
func (db *DB) CreateVoyage(v *models.Voyage) error {
	query := `
		INSERT INTO voyage (person_id, title, start_date, end_date, location_name, latitude, longitude, search_radius, search_radius_unit)
		VALUES (:person_id, :title, :start_date, :end_date, :location_name, :latitude, :longitude, :search_radius, :search_radius_unit)
		RETURNING id, created_at`

	rows, err := db.NamedQuery(query, v)
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
func (db *DB) UpdateVoyage(v *models.Voyage) error {
	query := `
		UPDATE voyage
		SET title = :title, start_date = :start_date, end_date = :end_date,
		    location_name = :location_name, latitude = :latitude, longitude = :longitude
		WHERE id = :id`
	_, err := db.NamedExec(query, v)
	return err
}

// UpdateVoyageSharing updates the sharing status and token of a voyage.
func (db *DB) UpdateVoyageSharing(id int64, shareToken *string, isPublic bool) error {
	query := `UPDATE voyage SET share_token = $1, is_public = $2 WHERE id = $3`
	_, err := db.Exec(query, shareToken, isPublic, id)
	return err
}

// GetVoyageByToken retrieves a public voyage using its share token.
func (db *DB) GetVoyageByToken(token string) (*models.Voyage, error) {
	var v models.Voyage
	query := `SELECT * FROM voyage WHERE share_token = $1 AND is_public = true`
	err := db.Get(&v, query, token)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// GetVoyage retrieves a voyage by its ID.
func (db *DB) GetVoyage(id int64) (*models.Voyage, error) {
	var v models.Voyage
	query := `SELECT * FROM voyage WHERE id = $1`
	err := db.Get(&v, query, id)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DeleteVoyage removes a voyage from the database.
func (db *DB) DeleteVoyage(id int64) error {
	query := `DELETE FROM voyage WHERE id = $1`
	_, err := db.Exec(query, id)
	return err
}
