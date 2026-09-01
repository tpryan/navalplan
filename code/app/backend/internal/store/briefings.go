package store

import (
	"context"
	"encoding/json"
	"time"

	"app/internal/model"
)

func (db *DB) GetBriefing(ctx context.Context, stopID int64) (*model.Briefing, error) {
	var b model.Briefing
	query := `SELECT * FROM briefing WHERE stop_id = $1`
	err := db.GetContext(ctx, &b, query, stopID)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) ListVoyageBriefings(ctx context.Context, voyageID int64) ([]model.Briefing, error) {
	briefings := []model.Briefing{}
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

func (db *DB) GetNearbyBriefing(ctx context.Context, lat, lng float64) (*model.Briefing, error) {
	const tolerance = 0.005

	var b model.Briefing
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

func (db *DB) CreateBriefing(ctx context.Context, b *model.Briefing) error {
	query := `
		INSERT INTO briefing (stop_id, weather_summary, sun_phase, tides, facilities, pilot_notes)
		VALUES (:stop_id, :weather_summary, :sun_phase, :tides, :facilities, :pilot_notes)
		ON CONFLICT (stop_id) DO UPDATE SET
			weather_summary = EXCLUDED.weather_summary,
			sun_phase = EXCLUDED.sun_phase,
			tides = EXCLUDED.tides,
			facilities = EXCLUDED.facilities,
			pilot_notes = EXCLUDED.pilot_notes,
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

func (db *DB) ListStopsInWindow(ctx context.Context, days int) ([]model.Stop, error) {
	stops := []model.Stop{}
	query := `
		SELECT s.*
		FROM stop s
		JOIN voyage v ON s.voyage_id = v.id
		WHERE s.target_date >= CURRENT_DATE 
		  AND s.target_date <= CURRENT_DATE + ($1 * interval '1 day')
		ORDER BY s.target_date ASC
	`
	err := db.SelectContext(ctx, &stops, query, days)
	if err != nil {
		return nil, err
	}
	return stops, nil
}

func (db *DB) ListAllFutureStops(ctx context.Context) ([]model.Stop, error) {
	stops := []model.Stop{}
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

func (db *DB) UpsertSafetyAlerts(ctx context.Context, stopID int64, alerts model.RawJSON) error {
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

func (db *DB) UpsertWeatherBriefing(ctx context.Context, stopID int64, weather model.WeatherSummary) error {
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
