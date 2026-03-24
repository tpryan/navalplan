package datastore

import (
	"context"

	"app/models"
)

func (db *DB) GetVoyageGuide(ctx context.Context, voyageID int64) (*models.VoyageGuide, error) {
	var g models.VoyageGuide
	query := `SELECT * FROM voyage_guide WHERE voyage_id = $1`
	err := db.GetContext(ctx, &g, query, voyageID)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (db *DB) CreateVoyageGuide(ctx context.Context, g *models.VoyageGuide) error {
	query := `
		INSERT INTO voyage_guide (voyage_id, summary, sailing_season, security_safety, hazards, hubs, charter_info, airports, country_info, currencies, points_of_interest)
		VALUES (:voyage_id, :summary, :sailing_season, :security_safety, :hazards, :hubs, :charter_info, :airports, :country_info, :currencies, :points_of_interest)
		ON CONFLICT (voyage_id) DO UPDATE SET
			summary = EXCLUDED.summary,
			sailing_season = EXCLUDED.sailing_season,
			security_safety = EXCLUDED.security_safety,
			hazards = EXCLUDED.hazards,
			hubs = EXCLUDED.hubs,
			charter_info = EXCLUDED.charter_info,
			airports = EXCLUDED.airports,
			country_info = EXCLUDED.country_info,
			currencies = EXCLUDED.currencies,
			points_of_interest = EXCLUDED.points_of_interest,
			created_at = NOW()
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, g)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&g.ID, &g.CreatedAt)
	}
	// If no rows returned, it might mean no update/insert happened, which shouldn't happen with RETURNING
	return nil
}

// SaveVoyageMap saves or updates the map snapshot for a voyage.
func (db *DB) SaveVoyageMap(ctx context.Context, voyageID int64, data []byte) error {
	query := `
		INSERT INTO voyage_map (voyage_id, image_data, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (voyage_id) DO UPDATE
		SET image_data = EXCLUDED.image_data, created_at = NOW()`
	_, err := db.ExecContext(ctx, query, voyageID, data)
	return err
}

// GetVoyageMap retrieves the map snapshot for a voyage.
func (db *DB) GetVoyageMap(ctx context.Context, voyageID int64) ([]byte, error) {
	var data []byte
	query := `SELECT image_data FROM voyage_map WHERE voyage_id = $1`
	err := db.GetContext(ctx, &data, query, voyageID)
	return data, err
}
