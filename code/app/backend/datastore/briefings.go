package datastore

import (
	"context"
	"encoding/json"
	"time"

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
	briefings := []models.Briefing{}
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

func (db *DB) ListAllFutureStops(ctx context.Context) ([]models.Stop, error) {
	stops := []models.Stop{}
	query := `
		SELECT s.*
		FROM stop s
		JOIN voyage v ON s.voyage_id = v.id
		WHERE s.target_date >= CURRENT_DATE
		ORDER BY s.target_date ASC
	`
	err := db.SelectContext(ctx, &stops, query)
	if err != nil {
		return nil, err
	}
	return stops, nil
}

func (db *DB) UpsertSafetyAlerts(ctx context.Context, stopID int64, alerts models.RawJSON) error {
	now := time.Now().UTC()
	query := `
		INSERT INTO briefing (stop_id, safety_alerts, safety_alerts_updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (stop_id) DO UPDATE SET
			safety_alerts = EXCLUDED.safety_alerts,
			safety_alerts_updated_at = EXCLUDED.safety_alerts_updated_at
	`
	_, err := db.ExecContext(ctx, query, stopID, string(alerts), now)
	return err
}

func (db *DB) UpsertWeatherBriefing(ctx context.Context, stopID int64, weather models.WeatherSummary) error {
	data, err := json.Marshal(weather)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	query := `
		INSERT INTO briefing (stop_id, weather_summary, weather_last_updated)
		VALUES ($1, $2, $3)
		ON CONFLICT (stop_id) DO UPDATE SET
			weather_summary = EXCLUDED.weather_summary,
			weather_last_updated = EXCLUDED.weather_last_updated
	`
	_, err = db.ExecContext(ctx, query, stopID, string(data), now)
	return err
}
