package datastore

import (
	"app/models"
)

func (db *DB) GetVoyageGuide(voyageID int64) (*models.VoyageGuide, error) {
	var g models.VoyageGuide
	query := `SELECT * FROM voyage_guide WHERE voyage_id = $1`
	err := db.Get(&g, query, voyageID)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (db *DB) CreateVoyageGuide(g *models.VoyageGuide) error {
	query := `
		INSERT INTO voyage_guide (voyage_id, summary, sailing_season, hazards, hubs, charter_info)
		VALUES (:voyage_id, :summary, :sailing_season, :hazards, :hubs, :charter_info)
		ON CONFLICT (voyage_id) DO UPDATE SET
			summary = EXCLUDED.summary,
			sailing_season = EXCLUDED.sailing_season,
			hazards = EXCLUDED.hazards,
			hubs = EXCLUDED.hubs,
			charter_info = EXCLUDED.charter_info,
			created_at = NOW()
		RETURNING id, created_at`

	rows, err := db.NamedQuery(query, g)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&g.ID, &g.CreatedAt)
	}
	return nil
}
