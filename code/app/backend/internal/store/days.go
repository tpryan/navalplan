package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"app/internal/model"
)

const dayColumns = `
	id, voyage_id, target_date, day_type,
	COALESCE(start_location_name, '') AS start_location_name,
	COALESCE(start_precise_location, '') AS start_precise_location,
	start_latitude, start_longitude,
	COALESCE(end_location_name, '') AS end_location_name,
	COALESCE(end_precise_location, '') AS end_precise_location,
	end_latitude, end_longitude,
	search_radius, search_radius_unit,
	COALESCE(notes, '') AS notes,
	created_at
`

func (db *DB) ListDays(ctx context.Context, voyageID int64, limit, offset int) ([]model.VoyageDay, error) {
	days := []model.VoyageDay{}
	query := `SELECT ` + dayColumns + ` FROM voyage_day WHERE voyage_id = $1 ORDER BY target_date ASC, id ASC`
	if limit > 0 {
		query += " LIMIT $2 OFFSET $3"
		err := db.SelectContext(ctx, &days, query, voyageID, limit, offset)
		return days, err
	}
	err := db.SelectContext(ctx, &days, query, voyageID)
	return days, err
}

func (db *DB) CreateDay(ctx context.Context, d *model.VoyageDay) error {
	query := `
		INSERT INTO voyage_day (
			voyage_id, target_date, day_type,
			start_location_name, start_precise_location, start_latitude, start_longitude,
			end_location_name, end_precise_location, end_latitude, end_longitude,
			search_radius, search_radius_unit, notes
		)
		VALUES (
			:voyage_id, :target_date, :day_type,
			:start_location_name, :start_precise_location, :start_latitude, :start_longitude,
			:end_location_name, :end_precise_location, :end_latitude, :end_longitude,
			:search_radius, :search_radius_unit, :notes
		)
		RETURNING id, created_at`
	rows, err := db.NamedQueryContext(ctx, query, d)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return rows.Scan(&d.ID, &d.CreatedAt)
	}
	return nil
}

func (db *DB) GetDay(ctx context.Context, id int64) (*model.VoyageDay, error) {
	var d model.VoyageDay
	query := `SELECT ` + dayColumns + ` FROM voyage_day WHERE id = $1`
	err := db.GetContext(ctx, &d, query, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (db *DB) UpdateDay(ctx context.Context, d *model.VoyageDay) error {
	query := `
		UPDATE voyage_day
		SET target_date = :target_date,
		    day_type = :day_type,
		    start_location_name = :start_location_name,
		    start_precise_location = :start_precise_location,
		    start_latitude = :start_latitude,
		    start_longitude = :start_longitude,
		    end_location_name = :end_location_name,
		    end_precise_location = :end_precise_location,
		    end_latitude = :end_latitude,
		    end_longitude = :end_longitude,
		    search_radius = :search_radius,
		    search_radius_unit = :search_radius_unit,
		    notes = :notes
		WHERE id = :id`
	_, err := db.NamedExecContext(ctx, query, d)
	return err
}

func (db *DB) ListLandfallDays(ctx context.Context, voyageID int64) ([]model.VoyageDay, error) {
	days := []model.VoyageDay{}
	query := `SELECT ` + dayColumns + ` FROM voyage_day WHERE voyage_id = $1 AND day_type != $2 ORDER BY target_date ASC, id ASC`
	err := db.SelectContext(ctx, &days, query, voyageID, model.DayTypePassagePoint)
	return days, err
}

func (db *DB) DeletePassagePoints(ctx context.Context, voyageID int64) error {
	query := `DELETE FROM voyage_day WHERE voyage_id = $1 AND day_type = $2`
	_, err := db.ExecContext(ctx, query, voyageID, model.DayTypePassagePoint)
	return err
}

func (db *DB) GetDayByDate(ctx context.Context, voyageID int64, date time.Time) (*model.VoyageDay, error) {
	var d model.VoyageDay
	query := `SELECT ` + dayColumns + ` FROM voyage_day WHERE voyage_id = $1 AND target_date = $2`
	err := db.GetContext(ctx, &d, query, voyageID, date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (db *DB) GetPreviousDay(ctx context.Context, voyageID int64, beforeDate time.Time) (*model.VoyageDay, error) {
	var d model.VoyageDay
	query := `SELECT ` + dayColumns + ` FROM voyage_day WHERE voyage_id = $1 AND target_date < $2 ORDER BY target_date DESC, id DESC LIMIT 1`
	err := db.GetContext(ctx, &d, query, voyageID, beforeDate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (db *DB) DeleteDay(ctx context.Context, id int64) error {
	query := `DELETE FROM voyage_day WHERE id = $1`
	_, err := db.ExecContext(ctx, query, id)
	return err
}
