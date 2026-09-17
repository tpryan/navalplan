package model

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
	IsAdmin   bool      `json:"is_admin" db:"is_admin"`
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
	ID               int64      `json:"id" db:"id"`
	PersonID         int64      `json:"person_id" db:"person_id"`
	Title            string     `json:"title" db:"title"`
	StartDate        *time.Time `json:"start_date" db:"start_date"`
	EndDate          *time.Time `json:"end_date" db:"end_date"`
	LocationName     *string    `json:"location_name" db:"location_name"`
	PreciseLocation  *string    `json:"precise_location" db:"precise_location"`
	Latitude         *float64   `json:"latitude" db:"latitude"`
	Longitude        *float64   `json:"longitude" db:"longitude"`
	SearchRadius     int        `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string     `json:"search_radius_unit" db:"search_radius_unit"`
	ShareToken       *string    `json:"share_token" db:"share_token"`
	IsPublic         bool       `json:"is_public" db:"is_public"`
	CheckinLatitude  *float64   `json:"checkin_latitude" db:"checkin_latitude"`
	CheckinLongitude *float64   `json:"checkin_longitude" db:"checkin_longitude"`
	CheckinLocation  *string    `json:"checkin_location" db:"checkin_location"`
	CheckinAt        *time.Time `json:"checkin_at" db:"checkin_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}

// Stop type constants distinguish stationary landfalls from at-sea passage points.
const (
	StopTypeLandfall     = "landfall"      // anchorage, mooring, or dock
	StopTypePassagePoint = "passage_point" // at-sea tracking position (extrapolated)
)

// Stop represents a specific stop or waypoint within a voyage.
type Stop struct {
	ID               int64     `json:"id" db:"id"`
	VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
	TargetDate       time.Time `json:"target_date" db:"target_date"`
	StopType         string    `json:"stop_type" db:"stop_type"` // "landfall" or "passage_point"
	LocationName     string    `json:"location_name" db:"location_name"`
	PreciseLocation  string    `json:"precise_location" db:"precise_location"`
	Latitude         float64   `json:"latitude" db:"latitude"`
	Longitude        float64   `json:"longitude" db:"longitude"`
	SearchRadius     int       `json:"search_radius" db:"search_radius"`
	SearchRadiusUnit string    `json:"search_radius_unit" db:"search_radius_unit"`
	Notes            string    `json:"notes" db:"notes"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// WeatherSummary holds structured weather forecast data for a stop.
type WeatherSummary struct {
	Summary          string    `json:"summary"`
	Condition        string    `json:"condition"`
	TempMinF         float64   `json:"temp_min_f"`
	TempMaxF         float64   `json:"temp_max_f"`
	WindSpeedKt      float64   `json:"wind_speed_kt"`
	WindDirection    string    `json:"wind_direction"`
	HourlyWind       []float64 `json:"hourly_wind,omitempty"`
	HourlyWindDir    []string  `json:"hourly_wind_dir,omitempty"`
	HourlyConditions []string  `json:"hourly_conditions,omitempty"`
	HourlyTemp       []float64 `json:"hourly_temp,omitempty"`
	HourlyGusts      []float64 `json:"hourly_gusts,omitempty"`
	HourlyPrecip     []float64 `json:"hourly_precip,omitempty"`
	HourlyWaveHeight []float64 `json:"hourly_wave_height,omitempty"`
	HourlyWavePeriod []float64 `json:"hourly_wave_period,omitempty"`
	HourlyWaveDir    []float64 `json:"hourly_wave_dir,omitempty"`
	WaveHeightFt     float64   `json:"wave_height_ft"`
	WaveDirection    float64   `json:"wave_direction"`
	WavePeriod       float64   `json:"wave_period"`
	DebugDurationMs  int64     `json:"debug_duration_ms"`
}

// Briefing contains researched information about a stop, such as weather and tides.
type Briefing struct {
	ID                    int64      `json:"id" db:"id"`
	StopID                int64      `json:"stop_id" db:"stop_id"`
	WeatherSummary        RawJSON    `json:"weather_summary" db:"weather_summary"`
	SunPhase              RawJSON    `json:"sun_phase" db:"sun_phase"`
	Tides                 RawJSON    `json:"tides" db:"tides"`
	Facilities            RawJSON    `json:"facilities" db:"facilities"`
	PilotNotes            RawJSON    `json:"pilot_notes,omitempty" db:"pilot_notes"`
	SafetyAlerts          RawJSON    `json:"safety_alerts" db:"safety_alerts"`
	SafetyAlertsUpdatedAt *time.Time `json:"safety_alerts_updated_at" db:"safety_alerts_updated_at"`
	WeatherLastUpdated    *time.Time `json:"weather_last_updated" db:"weather_last_updated"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
}

// VoyageGuide contains comprehensive researched information about the entire voyage.
type VoyageGuide struct {
	ID               int64     `json:"id" db:"id"`
	VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
	Summary          string    `json:"summary" db:"summary"`
	SailingSeason    RawJSON   `json:"sailing_season" db:"sailing_season"`
	SecuritySafety   RawJSON   `json:"security_safety" db:"security_safety"`
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

// ProgressEvent is a real-time status update broadcast during async research operations.
type ProgressEvent struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// VoyageRecommendation represents an AI-generated suggestion for a place to stay.
type VoyageRecommendation struct {
	ID          string    `json:"id" db:"id"`
	VoyageID    int64     `json:"voyage_id" db:"voyage_id"`
	Name        string    `json:"name" db:"name"`
	Type        string    `json:"type" db:"type"` // "Anchorage", "Mooring", "Marina"
	Latitude    float64   `json:"latitude" db:"latitude"`
	Longitude   float64   `json:"longitude" db:"longitude"`
	RadiusMiles float64   `json:"radius_miles" db:"radius_miles"`
	URL         string    `json:"url" db:"url"`
	Geometry    RawJSON   `json:"geometry" db:"geometry"` // GeoJSON Polygon for "blob" visualization
	Description string    `json:"description" db:"description"`
	Reasoning   string    `json:"reasoning" db:"reasoning"`             // Why the agent chose this
	References  RawJSON   `json:"reference_links" db:"reference_links"` // Links to more information
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// PilotReport aggregates voyage info, the voyage guide, and all area recommendations.
type PilotReport struct {
	Voyage          *Voyage                `json:"voyage"`
	Guide           *VoyageGuide           `json:"guide,omitempty"`
	Recommendations []VoyageRecommendation `json:"recommendations"`
	MapURL          string                 `json:"map_url,omitempty"`
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

// TrackKind represents whether a track is planned or recorded.
type TrackKind string

const (
	TrackKindPlanned  TrackKind = "planned"
	TrackKindRecorded TrackKind = "recorded"
)

// VoyageTrack represents a planned route or recorded GPS track.
type VoyageTrack struct {
	ID                string     `json:"id" db:"id"`
	VoyageID          int64      `json:"voyage_id" db:"voyage_id"`
	VoyageStopID      *int64     `json:"voyage_stop_id,omitempty" db:"voyage_stop_id"`
	Kind              string     `json:"kind" db:"kind"`
	Name              string     `json:"name" db:"name"`
	FileName          *string    `json:"file_name,omitempty" db:"file_name"`
	StartTime         *time.Time `json:"start_time,omitempty" db:"start_time"`
	EndTime           *time.Time `json:"end_time,omitempty" db:"end_time"`
	DistanceNM        *float64   `json:"distance_nm,omitempty" db:"distance_nm"`
	DurationInterval  *string    `json:"duration_interval,omitempty" db:"duration_interval"`
	MaxSpeedKts       *float64   `json:"max_speed_kts,omitempty" db:"max_speed_kts"`
	AvgSpeedKts       *float64   `json:"avg_speed_kts,omitempty" db:"avg_speed_kts"`
	GeoJSON           RawJSON    `json:"geojson" db:"geojson"`
	SimplifiedGeoJSON RawJSON    `json:"simplified_geojson" db:"simplified_geojson"`
	RawGPX            *string    `json:"raw_gpx,omitempty" db:"raw_gpx"`
	Debrief           RawJSON    `json:"debrief,omitempty" db:"debrief"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

// TrackDebrief encapsulates comparative analysis metrics and AI observations.
type TrackDebrief struct {
	TrackID            string   `json:"track_id"`
	TrackName          string   `json:"track_name"`
	RecordedDistanceNM float64  `json:"recorded_distance_nm"`
	PlannedDistanceNM  float64  `json:"planned_distance_nm"`
	DistanceDeltaNM    float64  `json:"distance_delta_nm"`
	RecordedDuration   string   `json:"recorded_duration"`
	PlannedDuration    string   `json:"planned_duration"`
	AvgSpeedKts        float64  `json:"avg_speed_kts"`
	MaxSpeedKts        float64  `json:"max_speed_kts"`
	Summary            string   `json:"summary"`
	TackingEfficiency  string   `json:"tacking_efficiency"`
	WeatherImpact      string   `json:"weather_impact"`
	Observations       []string `json:"observations"`
}
