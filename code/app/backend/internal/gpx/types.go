package gpx

import (
	"encoding/xml"
	"time"
)

// GPX represents the root <gpx> element.
type GPX struct {
	XMLName   xml.Name  `xml:"gpx"`
	Version   string    `xml:"version,attr"`
	Creator   string    `xml:"creator,attr"`
	Metadata  *Metadata `xml:"metadata"`
	Waypoints []Wpt     `xml:"wpt"`
	Routes    []Rte     `xml:"rte"`
	Tracks    []Trk     `xml:"trk"`
}

// Metadata contains GPX metadata.
type Metadata struct {
	Name string     `xml:"name"`
	Desc string     `xml:"desc"`
	Time *time.Time `xml:"time"`
}

// Wpt represents a single waypoint.
type Wpt struct {
	Lat  float64    `xml:"lat,attr"`
	Lon  float64    `xml:"lon,attr"`
	Ele  *float64   `xml:"ele"`
	Time *time.Time `xml:"time"`
	Name string     `xml:"name"`
	Desc string     `xml:"desc"`
}

// Rte represents a planned route.
type Rte struct {
	Name        string  `xml:"name"`
	Desc        string  `xml:"desc"`
	Number      *int    `xml:"number"`
	RoutePoints []RtePt `xml:"rtept"`
}

// RtePt represents a point along a planned route.
type RtePt struct {
	Lat  float64    `xml:"lat,attr"`
	Lon  float64    `xml:"lon,attr"`
	Ele  *float64   `xml:"ele"`
	Time *time.Time `xml:"time"`
	Name string     `xml:"name"`
	Desc string     `xml:"desc"`
}

// Trk represents a recorded GPS track.
type Trk struct {
	Name     string   `xml:"name"`
	Desc     string   `xml:"desc"`
	Segments []TrkSeg `xml:"trkseg"`
}

// TrkSeg represents a continuous track segment.
type TrkSeg struct {
	TrackPoints []TrkPt `xml:"trkpt"`
}

// TrkPt represents a track fix.
type TrkPt struct {
	Lat    float64    `xml:"lat,attr"`
	Lon    float64    `xml:"lon,attr"`
	Ele    *float64   `xml:"ele"`
	Time   *time.Time `xml:"time"`
	Speed  *float64   `xml:"speed"`  // meters/sec in GPX 1.1
	Course *float64   `xml:"course"` // degrees 0-360
	Name   string     `xml:"name"`
}

// Point represents a normalized navigation coordinate point with telemetry.
type Point struct {
	Lat       float64    `json:"lat"`
	Lng       float64    `json:"lng"`
	Ele       *float64   `json:"ele,omitempty"`
	Time      *time.Time `json:"time,omitempty"`
	SpeedKts  *float64   `json:"speed_kts,omitempty"`
	CourseDeg *float64   `json:"course_deg,omitempty"`
}

// ParsedTrack holds the processed track metrics and geometries.
type ParsedTrack struct {
	Name              string     `json:"name"`
	Kind              string     `json:"kind"` // "planned" or "recorded"
	Points            []Point    `json:"points"`
	StartTime         *time.Time `json:"start_time,omitempty"`
	EndTime           *time.Time `json:"end_time,omitempty"`
	DistanceNM        float64    `json:"distance_nm"`
	DurationInterval  string     `json:"duration_interval,omitempty"`
	MaxSpeedKts       float64    `json:"max_speed_kts"`
	AvgSpeedKts       float64    `json:"avg_speed_kts"`
	GeoJSON           []byte     `json:"geojson"`
	SimplifiedGeoJSON []byte     `json:"simplified_geojson"`
}

// GeoJSONFeature represents a GeoJSON LineString Feature.
type GeoJSONFeature struct {
	Type       string                 `json:"type"`
	Geometry   GeoJSONGeometry        `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

// GeoJSONGeometry represents LineString geometry coordinates.
type GeoJSONGeometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"` // [[lng, lat], ...]
}
