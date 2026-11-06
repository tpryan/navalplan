package store

import (
	"context"
	"encoding/json"
	"time"

	"app/internal/model"
)

func (db *DB) GetBriefing(ctx context.Context, dayID int64) (*model.Briefing, error) {
	var b model.Briefing
	query := `
		SELECT id, day_id, weather_summary, sun_phase, tides, facilities,
		       COALESCE(pilot_notes, '{}'::jsonb) AS pilot_notes,
		       COALESCE(safety_alerts, '{}'::jsonb) AS safety_alerts,
		       safety_alerts_updated_at, weather_last_updated, created_at
		FROM briefing
		WHERE day_id = $1`
	err := db.GetContext(ctx, &b, query, dayID)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) ListVoyageBriefings(ctx context.Context, voyageID int64) ([]model.Briefing, error) {
	briefings := []model.Briefing{}
	query := `
		SELECT b.id, b.day_id, b.weather_summary, b.sun_phase, b.tides, b.facilities,
		       COALESCE(b.pilot_notes, '{}'::jsonb) AS pilot_notes,
		       COALESCE(b.safety_alerts, '{}'::jsonb) AS safety_alerts,
		       b.safety_alerts_updated_at, b.weather_last_updated, b.created_at
		FROM briefing b
		JOIN voyage_day d ON b.day_id = d.id
		WHERE d.voyage_id = $1
	`
	err := db.SelectContext(ctx, &briefings, query, voyageID)
	if err != nil {
		return nil, err
	}
	return briefings, nil
}

func (db *DB) GetNearbyBriefing(ctx context.Context, lat, lng float64) (*model.Briefing, error) {
	// 2.0 NM (~3.7 km) radius: bounding box of ~0.035° lat / ~0.045° lng, plus exact Haversine distance filter (<= 2.0 NM).
	const latTol = 0.035
	const lngTol = 0.045
	const maxDistNM = 2.0

	var b model.Briefing
	query := `
		SELECT b.id, b.day_id, b.weather_summary, b.sun_phase, b.tides, b.facilities,
		       COALESCE(b.pilot_notes, '{}'::jsonb) AS pilot_notes,
		       COALESCE(b.safety_alerts, '{}'::jsonb) AS safety_alerts,
		       b.safety_alerts_updated_at, b.weather_last_updated, b.created_at
		FROM briefing b
		JOIN voyage_day d ON b.day_id = d.id
		WHERE d.end_latitude BETWEEN $1::float - $3::float AND $1::float + $3::float
		  AND d.end_longitude BETWEEN $2::float - $4::float AND $2::float + $4::float
		  AND (3440.065 * 2 * ASIN(SQRT(
		      POWER(SIN(RADIANS(d.end_latitude - $1::float) / 2), 2) +
		      COS(RADIANS($1::float)) * COS(RADIANS(d.end_latitude)) *
		      POWER(SIN(RADIANS(d.end_longitude - $2::float) / 2), 2)
		  ))) <= $5::float
		ORDER BY b.created_at DESC
		LIMIT 1
	`
	err := db.GetContext(ctx, &b, query, lat, lng, latTol, lngTol, maxDistNM)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) CreateBriefing(ctx context.Context, b *model.Briefing) error {
	query := `
		INSERT INTO briefing (day_id, weather_summary, sun_phase, tides, facilities, pilot_notes)
		VALUES (:day_id, :weather_summary, :sun_phase, :tides, :facilities, :pilot_notes)
		ON CONFLICT (day_id) DO UPDATE SET
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

func (db *DB) ListDaysInWindow(ctx context.Context, windowDays int) ([]model.VoyageDay, error) {
	days := []model.VoyageDay{}
	query := `
		SELECT d.id, d.voyage_id, d.target_date, d.day_type,
		       COALESCE(d.start_location_name, '') AS start_location_name,
		       COALESCE(d.start_precise_location, '') AS start_precise_location,
		       d.start_latitude, d.start_longitude,
		       COALESCE(d.end_location_name, '') AS end_location_name,
		       COALESCE(d.end_precise_location, '') AS end_precise_location,
		       d.end_latitude, d.end_longitude,
		       d.search_radius, d.search_radius_unit, COALESCE(d.notes, '') AS notes, d.created_at
		FROM voyage_day d
		JOIN voyage v ON d.voyage_id = v.id
		WHERE d.target_date >= CURRENT_DATE
		  AND d.target_date <= CURRENT_DATE + ($1 * interval '1 day')
		ORDER BY d.target_date ASC, d.id ASC
	`
	err := db.SelectContext(ctx, &days, query, windowDays)
	if err != nil {
		return nil, err
	}
	return days, nil
}

func (db *DB) ListAllFutureDays(ctx context.Context) ([]model.VoyageDay, error) {
	days := []model.VoyageDay{}
	query := `
		SELECT d.id, d.voyage_id, d.target_date, d.day_type,
		       COALESCE(d.start_location_name, '') AS start_location_name,
		       COALESCE(d.start_precise_location, '') AS start_precise_location,
		       d.start_latitude, d.start_longitude,
		       COALESCE(d.end_location_name, '') AS end_location_name,
		       COALESCE(d.end_precise_location, '') AS end_precise_location,
		       d.end_latitude, d.end_longitude,
		       d.search_radius, d.search_radius_unit, COALESCE(d.notes, '') AS notes, d.created_at
		FROM voyage_day d
		JOIN voyage v ON d.voyage_id = v.id
		WHERE d.target_date >= CURRENT_DATE
		ORDER BY d.target_date ASC, d.id ASC
	`
	err := db.SelectContext(ctx, &days, query)
	if err != nil {
		return nil, err
	}
	return days, nil
}

func (db *DB) UpsertSafetyAlerts(ctx context.Context, dayID int64, alerts model.RawJSON) error {
	now := time.Now().UTC()
	query := `
		INSERT INTO briefing (day_id, safety_alerts, safety_alerts_updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (day_id) DO UPDATE SET
			safety_alerts = EXCLUDED.safety_alerts,
			safety_alerts_updated_at = EXCLUDED.safety_alerts_updated_at
	`
	_, err := db.ExecContext(ctx, query, dayID, string(alerts), now)
	return err
}

func (db *DB) UpsertWeatherBriefing(ctx context.Context, dayID int64, weather model.WeatherSummary) error {
	data, err := json.Marshal(weather)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	query := `
		INSERT INTO briefing (day_id, weather_summary, weather_last_updated)
		VALUES ($1, $2, $3)
		ON CONFLICT (day_id) DO UPDATE SET
			weather_summary = EXCLUDED.weather_summary,
			weather_last_updated = EXCLUDED.weather_last_updated
	`
	_, err = db.ExecContext(ctx, query, dayID, string(data), now)
	return err
}
