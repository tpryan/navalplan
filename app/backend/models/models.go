package models

import (
	"database/sql/driver"
	"errors"
	"time"
)

// Person represents a user in the system.
type Person struct {
	ID         int64     `json:"id" db:"id"`
	GoogleID   string    `json:"google_id" db:"google_id"`
	Email      string    `json:"email" db:"email"`
	Name       string    `json:"name" db:"name"`
	PictureURL *string   `json:"picture_url" db:"picture_url"`
	IsAdmin    bool      `json:"is_admin" db:"is_admin"`
	InvitedBy  *int64    `json:"-" db:"invited_by"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// Invitation represents an email invite.
type Invitation struct {
	Email     string    `json:"email" db:"email"`
	InvitedBy *int64    `json:"invited_by" db:"invited_by"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Session represents a user session.
type Session struct {
	Token     string    `json:"token" db:"token"`
	PersonID  int64     `json:"person_id" db:"person_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
}

// Voyage represents a planned trip.
type Voyage struct {
	ID               int64     `json:"id" db:"id"`
	PersonID         int64     `json:"person_id" db:"person_id"`
	Title            string    `json:"title" db:"title"`
	StartDate        time.Time `json:"start_date" db:"start_date"`
	EndDate          time.Time `json:"end_date" db:"end_date"`
	LocationName     *string   `json:"location_name" db:"location_name"`
	PreciseLocation  *string   `json:"precise_location" db:"precise_location"`
	Latitude         *float64  `json:"latitude" db:"latitude"`
	Longitude        *float64  `json:"longitude" db:"longitude"`
	SearchRadius     int       `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string    `json:"search_radius_unit" db:"search_radius_unit"`
	ShareToken       *string   `json:"share_token" db:"share_token"`
	IsPublic         bool      `json:"is_public" db:"is_public"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// Stop represents a specific stop or waypoint within a voyage.
type Stop struct {
	ID               int64     `json:"id" db:"id"`
	VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
	TargetDate       time.Time `json:"target_date" db:"target_date"`
	LocationName     string    `json:"location_name" db:"location_name"`
	PreciseLocation  string    `json:"precise_location" db:"precise_location"`
	Latitude         float64   `json:"latitude" db:"latitude"`
	Longitude        float64   `json:"longitude" db:"longitude"`
	SearchRadius     int       `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string    `json:"search_radius_unit" db:"search_radius_unit"`
	Notes            string    `json:"notes" db:"notes"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// Briefing contains researched information about a stop, such as weather and tides.
type Briefing struct {
	ID             int64     `json:"id" db:"id"`
	StopID         int64     `json:"stop_id" db:"stop_id"`
	WeatherSummary RawJSON   `json:"weather_summary" db:"weather_summary"`
	SunPhase       RawJSON   `json:"sun_phase" db:"sun_phase"`
	Tides          RawJSON   `json:"tides" db:"tides"`
	Facilities     RawJSON   `json:"facilities" db:"facilities"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// VoyageGuide contains comprehensive researched information about the entire voyage.
type VoyageGuide struct {
	ID               int64     `json:"id" db:"id"`
	VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
	Summary          string    `json:"summary" db:"summary"`
	SailingSeason    RawJSON   `json:"sailing_season" db:"sailing_season"`
	Hazards          RawJSON   `json:"hazards" db:"hazards"`
	Hubs             RawJSON   `json:"hubs" db:"hubs"`
	CharterInfo      RawJSON   `json:"charter_info" db:"charter_info"`
	Airports         RawJSON   `json:"airports" db:"airports"`
	CountryInfo      RawJSON   `json:"country_info" db:"country_info"`
	Currencies       RawJSON   `json:"currencies" db:"currencies"`
	PointsOfInterest RawJSON   `json:"points_of_interest" db:"points_of_interest"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// VoyageMap stores the static map snapshot for a voyage.
type VoyageMap struct {
	VoyageID  int64     `json:"voyage_id" db:"voyage_id"`
	ImageData []byte    `json:"-" db:"image_data"` // Don't expose raw bytes in JSON default
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// SailingRegion represents a geographic area known for sailing.
type SailingRegion struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Geometry  RawJSON   `json:"geometry" db:"geometry"` // GeoJSON Polygon
	Type      string    `json:"type" db:"type"`         // e.g. "Coastal", "Island Group"
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// RegionSeasonality stores monthly suitability intelligence for a sailing region.
type RegionSeasonality struct {
	ID                int64     `json:"id" db:"id"`
	RegionID          int64     `json:"region_id" db:"region_id"`
	Month             int       `json:"month" db:"month"` // 1-12
	SuitabilityScore  int       `json:"suitability_score" db:"suitability_score"`
	IsHiddenGem       bool      `json:"is_hidden_gem" db:"is_hidden_gem"`
	Tier              string    `json:"tier" db:"tier"`
	Summary           string    `json:"summary" db:"summary"`
	DeepCutReasoning  string    `json:"deep_cut_reasoning" db:"deep_cut_reasoning"`
	AvgWindSpeedKnots int       `json:"avg_wind_speed_knots" db:"avg_wind_speed_knots"`
	AvgTempC          int       `json:"avg_temp_c" db:"avg_temp_c"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}

// RegionWithSeasonality combines region details with its monthly data.
type RegionWithSeasonality struct {
	SailingRegion
	SuitabilityScore  int    `json:"suitability_score" db:"suitability_score"`
	IsHiddenGem       bool   `json:"is_hidden_gem" db:"is_hidden_gem"`
	Tier              string `json:"tier" db:"tier"`
	Summary           string `json:"summary" db:"summary"`
	DeepCutReasoning  string `json:"deep_cut_reasoning" db:"deep_cut_reasoning"`
	AvgWindSpeedKnots int    `json:"avg_wind_speed_knots" db:"avg_wind_speed_knots"`
	AvgTempC          int    `json:"avg_temp_c" db:"avg_temp_c"`
}

// VoyageRecommendation represents an AI-generated suggestion for a place to stay.
type VoyageRecommendation struct {
	ID          string    `json:"id" db:"id"`
	VoyageID    int64     `json:"voyage_id" db:"voyage_id"`
	Name        string    `json:"name" db:"name"`
	Type        string    `json:"type" db:"type"` // "Anchorage", "Mooring", "Marina"
	Latitude    float64   `json:"latitude" db:"latitude"`
	Longitude   float64   `json:"longitude" db:"longitude"`
	Geometry    RawJSON   `json:"geometry" db:"geometry"` // GeoJSON Polygon for "blob" visualization
	Description string    `json:"description" db:"description"`
	Reasoning   string    `json:"reasoning" db:"reasoning"` // Why the agent chose this
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// RawJSON is a helper for JSONB columns
type RawJSON []byte

// MarshalJSON returns the JSON encoding of r.
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalJSON sets *r to a copy of data.
func (r *RawJSON) UnmarshalJSON(data []byte) error {
	*r = append((*r)[0:0], data...)
	return nil
}

// Value implements the driver.Valuer interface.
func (r RawJSON) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	return string(r), nil
}

// Scan implements the sql.Scanner interface.
func (r *RawJSON) Scan(value interface{}) error {
	if value == nil {
		*r = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*r = append((*r)[0:0], v...)
	case string:
		*r = append((*r)[0:0], []byte(v)...)
	default:
		return errors.New("type assertion to []byte failed")
	}
	return nil
}
