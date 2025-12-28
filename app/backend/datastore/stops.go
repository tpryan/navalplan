package datastore

import (
	"app/models"
)

func (db *DB) ListStops(voyageID int64) ([]models.Stop, error) {
	stops := []models.Stop{}
	query := `SELECT * FROM stop WHERE voyage_id = $1 ORDER BY target_date ASC`
	err := db.Select(&stops, query, voyageID)
	return stops, err
}

func (db *DB) CreateStop(s *models.Stop) error {
	query := `
		INSERT INTO stop (voyage_id, target_date, location_name, latitude, longitude, search_radius, search_radius_unit, notes)
		VALUES (:voyage_id, :target_date, :location_name, :latitude, :longitude, :search_radius, :search_radius_unit, :notes)
		RETURNING id, created_at`
	
	rows, err := db.NamedQuery(query, s)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&s.ID, &s.CreatedAt)
	}
	return nil
}

func (db *DB) GetStop(id int64) (*models.Stop, error) {
	var s models.Stop
	query := `SELECT * FROM stop WHERE id = $1`
	err := db.Get(&s, query, id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) UpdateStop(s *models.Stop) error {
	query := `
		UPDATE stop 
		SET target_date = :target_date, location_name = :location_name, latitude = :latitude, 
		    longitude = :longitude, search_radius = :search_radius, search_radius_unit = :search_radius_unit, notes = :notes
		WHERE id = :id`
	_, err := db.NamedExec(query, s)
	return err
}

func (db *DB) DeleteStop(id int64) error {
	query := `DELETE FROM stop WHERE id = $1`
	_, err := db.Exec(query, id)
	return err
}
