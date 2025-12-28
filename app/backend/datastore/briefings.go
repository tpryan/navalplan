package datastore

import (
	"app/models"
)

func (db *DB) GetBriefing(stopID int64) (*models.Briefing, error) {
	var b models.Briefing
	query := `SELECT * FROM briefing WHERE stop_id = $1`
	err := db.Get(&b, query, stopID)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) CreateBriefing(b *models.Briefing) error {
	query := `
		INSERT INTO briefing (stop_id, weather_summary, tides, facilities)
		VALUES (:stop_id, :weather_summary, :tides, :facilities)
		ON CONFLICT (stop_id) DO UPDATE SET
			weather_summary = EXCLUDED.weather_summary,
			tides = EXCLUDED.tides,
			facilities = EXCLUDED.facilities,
			created_at = NOW()
		RETURNING id, created_at`
	
	rows, err := db.NamedQuery(query, b)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&b.ID, &b.CreatedAt)
	}
	return nil
}
