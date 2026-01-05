package datastore

import (
	"context"
	"fmt"

	"app/models"
)

// ListRegionsByMonth returns all regions that have seasonality data for the given month.
func (db *DB) ListRegionsByMonth(ctx context.Context, month int) ([]models.RegionWithSeasonality, error) {
	var regions []models.RegionWithSeasonality
	query := `
		SELECT 
			r.*,
			s.suitability_score,
			s.is_hidden_gem,
			s.tier,
			s.summary,
			s.deep_cut_reasoning,
			s.avg_wind_speed_knots,
			s.avg_temp_c
		FROM sailing_regions r
		JOIN region_seasonality s ON r.id = s.region_id
		WHERE s.month = $1
		ORDER BY r.name ASC`

	err := db.SelectContext(ctx, &regions, query, month)
	if err != nil {
		return nil, fmt.Errorf("failed to list regions by month: %w", err)
	}
	return regions, nil
}

// GetRegionDetails returns a region and its seasonality data for a specific month.
func (db *DB) GetRegionDetails(ctx context.Context, regionID int, month int) (*models.SailingRegion, *models.RegionSeasonality, error) {
	var region models.SailingRegion
	err := db.GetContext(ctx, &region, "SELECT * FROM sailing_regions WHERE id = $1", regionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get region: %w", err)
	}

	var seasonality models.RegionSeasonality
	err = db.GetContext(ctx, &seasonality, "SELECT * FROM region_seasonality WHERE region_id = $1 AND month = $2", regionID, month)
	if err != nil {
		return &region, nil, fmt.Errorf("failed to get seasonality: %w", err)
	}

	return &region, &seasonality, nil
}

// UpsertRegion inserts or updates a sailing region.
func (db *DB) UpsertRegion(ctx context.Context, region *models.SailingRegion) error {
	query := `
		INSERT INTO sailing_regions (name, geometry, type)
		VALUES (:name, :geometry, :type)
		ON CONFLICT (name) DO UPDATE SET
			geometry = EXCLUDED.geometry,
			type = EXCLUDED.type
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, region)
	if err != nil {
		return fmt.Errorf("failed to upsert region: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&region.ID, &region.CreatedAt); err != nil {
			return fmt.Errorf("failed to scan upserted region: %w", err)
		}
	}

	return nil
}

// DeleteSeasonalityForMonth removes all seasonality entries for a specific month.
// This is used to clear old data before re-mining.
func (db *DB) DeleteSeasonalityForMonth(ctx context.Context, month int) error {
	query := `DELETE FROM region_seasonality WHERE month = $1`
	_, err := db.ExecContext(ctx, query, month)
	if err != nil {
		return fmt.Errorf("failed to delete seasonality for month %d: %w", month, err)
	}
	return nil
}

// UpsertSeasonality inserts or updates monthly seasonality data for a region.
func (db *DB) UpsertSeasonality(ctx context.Context, s *models.RegionSeasonality) error {
	query := `
		INSERT INTO region_seasonality (
			region_id, month, suitability_score, is_hidden_gem, tier, summary, 
			deep_cut_reasoning, avg_wind_speed_knots, avg_temp_c
		)
		VALUES (
			:region_id, :month, :suitability_score, :is_hidden_gem, :tier, :summary, 
			:deep_cut_reasoning, :avg_wind_speed_knots, :avg_temp_c
		)
		ON CONFLICT (region_id, month) DO UPDATE SET
			suitability_score = EXCLUDED.suitability_score,
			is_hidden_gem = EXCLUDED.is_hidden_gem,
			tier = EXCLUDED.tier,
			summary = EXCLUDED.summary,
			deep_cut_reasoning = EXCLUDED.deep_cut_reasoning,
			avg_wind_speed_knots = EXCLUDED.avg_wind_speed_knots,
			avg_temp_c = EXCLUDED.avg_temp_c
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, s)
	if err != nil {
		return fmt.Errorf("failed to upsert seasonality: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&s.ID, &s.CreatedAt); err != nil {
			return fmt.Errorf("failed to scan upserted seasonality: %w", err)
		}
	}

	return nil
}
