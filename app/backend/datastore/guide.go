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
		INSERT INTO voyage_guide (voyage_id, summary, sailing_season, hazards, hubs, charter_info, airports, country_info, currencies, points_of_interest)
		VALUES (:voyage_id, :summary, :sailing_season, :hazards, :hubs, :charter_info, :airports, :country_info, :currencies, :points_of_interest)
		ON CONFLICT (voyage_id) DO UPDATE SET
			summary = EXCLUDED.summary,
			sailing_season = EXCLUDED.sailing_season,
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
