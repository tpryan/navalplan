package store

import (
	"context"

	"app/internal/model"
)

// ListVoyageRecommendations retrieves all recommendations for a given voyage.
func (db *DB) ListVoyageRecommendations(ctx context.Context, voyageID int64) ([]model.VoyageRecommendation, error) {
	var recommendations []model.VoyageRecommendation
	query := `
		SELECT id, voyage_id, name, type, latitude, longitude, radius_miles, COALESCE(url, '') as url, geometry, description, COALESCE(reasoning, '') as reasoning, reference_links, created_at
		FROM voyage_recommendation 
		WHERE voyage_id = $1 
		ORDER BY created_at DESC`

	err := db.SelectContext(ctx, &recommendations, query, voyageID)
	return recommendations, err
}

// CreateVoyageRecommendation inserts a new recommendation into the database.
func (db *DB) CreateVoyageRecommendation(ctx context.Context, r *model.VoyageRecommendation) error {
	query := `
		INSERT INTO voyage_recommendation (voyage_id, name, type, latitude, longitude, radius_miles, url, geometry, description, reasoning, reference_links)
		VALUES (:voyage_id, :name, :type, :latitude, :longitude, :radius_miles, :url, :geometry, :description, :reasoning, :reference_links)
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, r)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&r.ID, &r.CreatedAt)
	}
	return nil
}

// DeleteVoyageRecommendations removes all recommendations for a given voyage.
func (db *DB) DeleteVoyageRecommendations(ctx context.Context, voyageID int64) error {
	query := `DELETE FROM voyage_recommendation WHERE voyage_id = $1`
	_, err := db.ExecContext(ctx, query, voyageID)
	return err
}
