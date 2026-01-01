package models

import (
	"time"
)

type Person struct {
	ID         int64     `json:"id" db:"id"`
	GoogleID   string    `json:"google_id" db:"google_id"`
	Email      string    `json:"email" db:"email"`
	Name       string    `json:"name" db:"name"`
	PictureURL string    `json:"picture_url" db:"picture_url"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type Session struct {
	Token     string    `json:"token" db:"token"`
	PersonID  int64     `json:"person_id" db:"person_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
}

type Voyage struct {
	ID               int64      `json:"id" db:"id"`
	PersonID         int64      `json:"person_id" db:"person_id"`
	Title            string     `json:"title" db:"title"`
	StartDate        time.Time  `json:"start_date" db:"start_date"`
	EndDate          time.Time  `json:"end_date" db:"end_date"`
	LocationName     *string    `json:"location_name" db:"location_name"`
	Latitude         *float64   `json:"latitude" db:"latitude"`
	Longitude        *float64   `json:"longitude" db:"longitude"`
	SearchRadius     int        `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string     `json:"search_radius_unit" db:"search_radius_unit"`
	GoogleDocID      *string    `json:"google_doc_id" db:"google_doc_id"`
	LastExportedAt   *time.Time `json:"last_exported_at" db:"last_exported_at"`
	ShareToken       *string    `json:"share_token" db:"share_token"`
	IsPublic         bool       `json:"is_public" db:"is_public"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}

type Stop struct {
	ID               int64     `json:"id" db:"id"`
	VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
	TargetDate       time.Time `json:"target_date" db:"target_date"`
	LocationName     string    `json:"location_name" db:"location_name"`
	Latitude         float64   `json:"latitude" db:"latitude"`
	Longitude        float64   `json:"longitude" db:"longitude"`
	SearchRadius     int       `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string    `json:"search_radius_unit" db:"search_radius_unit"`
	Notes            string    `json:"notes" db:"notes"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

type Briefing struct {
	ID             int64     `json:"id" db:"id"`
	StopID         int64     `json:"stop_id" db:"stop_id"`
	WeatherSummary RawJSON   `json:"weather_summary" db:"weather_summary"`
	SunPhase       RawJSON   `json:"sun_phase" db:"sun_phase"`
	Tides          RawJSON   `json:"tides" db:"tides"`
	Facilities     RawJSON   `json:"facilities" db:"facilities"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type VoyageGuide struct {
	ID            int64     `json:"id" db:"id"`
	VoyageID      int64     `json:"voyage_id" db:"voyage_id"`
	Summary       string    `json:"summary" db:"summary"`
	SailingSeason RawJSON   `json:"sailing_season" db:"sailing_season"`
	Hazards       RawJSON   `json:"hazards" db:"hazards"`
	Hubs          RawJSON   `json:"hubs" db:"hubs"`
	CharterInfo   RawJSON   `json:"charter_info" db:"charter_info"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// RawJSON is a helper for JSONB columns
type RawJSON []byte

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return r, nil
}

func (r *RawJSON) UnmarshalJSON(data []byte) error {
	*r = append((*r)[0:0], data...)
	return nil
}
