package datastore

import (
	"context"

	"app/models"
)

func (db *DB) GetBriefing(ctx context.Context, stopID int64) (*models.Briefing, error) {
	var b models.Briefing
	query := `SELECT * FROM briefing WHERE stop_id = $1`
	err := db.GetContext(ctx, &b, query, stopID)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) ListVoyageBriefings(ctx context.Context, voyageID int64) ([]models.Briefing, error) {
	var briefings []models.Briefing
	query := `
		SELECT b.*
		FROM briefing b
		JOIN stop s ON b.stop_id = s.id
		WHERE s.voyage_id = $1
	`
	err := db.SelectContext(ctx, &briefings, query, voyageID)
	if err != nil {
		return nil, err
	}
	return briefings, nil
}

func (db *DB) GetNearbyBriefing(ctx context.Context, lat, lng float64) (*models.Briefing, error) {
	// 0.005 degrees is approximately 555 meters at the equator, sufficient for "nearby" check
	const tolerance = 0.005

	var b models.Briefing
	query := `
		SELECT b.*
		FROM briefing b
		JOIN stop s ON b.stop_id = s.id
		WHERE s.latitude BETWEEN $1::float - $3::float AND $1::float + $3::float
		  AND s.longitude BETWEEN $2::float - $3::float AND $2::float + $3::float
		ORDER BY b.created_at DESC
		LIMIT 1
	`
	err := db.GetContext(ctx, &b, query, lat, lng, tolerance)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) CreateBriefing(ctx context.Context, b *models.Briefing) error {
	query := `
		INSERT INTO briefing (stop_id, weather_summary, sun_phase, tides, facilities)
		VALUES (:stop_id, :weather_summary, :sun_phase, :tides, :facilities)
		ON CONFLICT (stop_id) DO UPDATE SET
			weather_summary = EXCLUDED.weather_summary,
			sun_phase = EXCLUDED.sun_phase,
			tides = EXCLUDED.tides,
			facilities = EXCLUDED.facilities,
			created_at = NOW()
		RETURNING id, created_at`

	rows, err := db.NamedQueryContext(ctx, query, b)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Scan(&b.ID, &b.CreatedAt)
	}
	return nil
}
