package tool

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	places "cloud.google.com/go/maps/places/apiv1"
	"cloud.google.com/go/maps/places/apiv1/placespb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/latlng"
	"google.golang.org/grpc/metadata"
)

// haversineDistance calculates the distance between two points in meters.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // Earth radius in meters
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dphi := (lat2 - lat1) * math.Pi / 180
	dlambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dphi/2)*math.Sin(dphi/2) +
		math.Cos(phi1)*math.Cos(phi2)*
			math.Sin(dlambda/2)*math.Sin(dlambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

// PlacesArgs defines the arguments for the find_places_nearby tool.
type PlacesArgs struct {
	Query     string  `json:"query" description:"Text query (e.g. 'restaurants', 'marinas')."`
	Latitude  float64 `json:"latitude" description:"Latitude for location bias (required)."`
	Longitude float64 `json:"longitude" description:"Longitude for location bias (required)."`
	Radius    float64 `json:"radius,omitempty" description:"Search radius in meters. Default 5000, max 50000."`
	OpenNow   bool    `json:"open_now,omitempty" description:"If true, only return places currently open."`
	MinRating float64 `json:"min_rating,omitempty" description:"Minimum rating (1.0 - 5.0)."`
}

// PlaceResult represents a single place found by the search.
type PlaceResult struct {
	Name            string   `json:"name"`
	Address         string   `json:"address"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	DistanceMeters  float64  `json:"distance_meters"`
	Rating          float64  `json:"rating"`
	UserRatingCount int32    `json:"user_rating_count"`
	BusinessStatus  string   `json:"business_status"`
	Types           []string `json:"types"`
	WebsiteURI      string   `json:"website_uri,omitempty"`
}

// PlacesResponse defines the response structure for the find_places_nearby tool.
type PlacesResponse struct {
	Places          []PlaceResult `json:"places"`
	DebugDurationMS int64         `json:"debug_duration_ms"`
}

// PlacesClient defines the interface for the Google Places API client.
type PlacesClient interface {
	SearchText(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error)
	SearchNearby(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error)
	Close() error
}

// PlacesProvider implements the find_places_nearby tool using the Google Maps Places API.
type PlacesProvider struct {
	client PlacesClient
}

// Close closes the underlying client connection.
func (p *PlacesProvider) Close() error {
	return p.client.Close()
}

// NewPlacesTool creates a new ADK tool for searching places nearby.
func NewPlacesTool(ctx context.Context, apiKey string) (tool.Tool, *PlacesProvider, error) {
	var clientOpts []option.ClientOption
	if apiKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(apiKey))
	}

	c, err := places.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("creating Places client: %w", err)
	}

	p := &PlacesProvider{client: c}

	t, err := functiontool.New(functiontool.Config{
		Name:        "find_places_nearby",
		Description: "Finds places (e.g. marinas, restaurants) near a location using Google Maps Text Search. Returns specific locations with Lat/Lng.",
	}, p.FindPlaces)

	return t, p, err
}

// offsetLatLng returns a point offset by distanceMeters along bearingDeg (0=N, 90=E, 180=S, 270=W).
func offsetLatLng(lat, lng, distanceMeters, bearingDeg float64) (float64, float64) {
	const R = 6371000.0
	angDist := distanceMeters / R
	brng := bearingDeg * math.Pi / 180.0
	lat1 := lat * math.Pi / 180.0
	lng1 := lng * math.Pi / 180.0

	lat2 := math.Asin(math.Sin(lat1)*math.Cos(angDist) + math.Cos(lat1)*math.Sin(angDist)*math.Cos(brng))
	lng2 := lng1 + math.Atan2(math.Sin(brng)*math.Sin(angDist)*math.Cos(lat1), math.Cos(angDist)-math.Sin(lat1)*math.Sin(lat2))
	return lat2 * 180.0 / math.Pi, lng2 * 180.0 / math.Pi
}

func (p *PlacesProvider) FindPlaces(ctx agent.Context, args PlacesArgs) (PlacesResponse, error) {
	start := time.Now()

	// Default radius if 0; if under 100, assume miles/NM passed and convert to meters.
	requestedRadius := args.Radius
	if requestedRadius <= 0 {
		requestedRadius = 5000 // 5km default
	} else if requestedRadius < 100 {
		requestedRadius = requestedRadius * 1852 // Convert NM/miles to meters
	}
	// When running in voyage-wide area mode, enforce at least the session's area
	// radius even if the sub-agent passed the 1850m stop focus radius.
	if areaRadius, ok := areaSearchRadius(ctx); ok && areaRadius > requestedRadius {
		requestedRadius = areaRadius
	}
	apiRadius := requestedRadius
	if apiRadius > 50000 {
		apiRadius = 50000 // Google Places API maximum limit is 50,000 meters per Circle
	}

	centerPoint := &latlng.LatLng{
		Latitude:  args.Latitude,
		Longitude: args.Longitude,
	}

	circleArea := &placespb.Circle{
		Center: centerPoint,
		Radius: apiRadius,
	}

	locationBias := &placespb.SearchTextRequest_LocationBias{
		Type: &placespb.SearchTextRequest_LocationBias_Circle{
			Circle: circleArea,
		},
	}

	// Define fields to return (FieldMask) — Pro tier only (no Enterprise fields)
	fieldsToRequest := []string{
		"places.displayName",
		"places.formattedAddress",
		"places.location",
		"places.businessStatus",
		"places.types",
		"places.googleMapsUri",
	}
	fieldMaskHeader := strings.Join(fieldsToRequest, ",")

	// Append FieldMask to context
	callCtx := metadata.AppendToOutgoingContext(ctx, "x-goog-fieldmask", fieldMaskHeader)

	// Search strategy by requestedRadius:
	// - <= 10 km: amenity categories use Nearby Search (circle-restricted, DISTANCE ranked)
	//   so results are the closest places of that kind rather than prominent city-wide matches.
	// - > 10 km: Text Search with RELEVANCE ranking.
	// - > 15 km: for core hub/overnight categories (marina, anchorage, mooring), also probe
	//   4 cardinal sectors (N, E, S, W) so open-water centers cover all surrounding coasts.
	var placesList []*placespb.Place
	usedNearby := false
	if requestedRadius <= 10000 {
		if includedTypes := nearbyTypesForQuery(args.Query); len(includedTypes) > 0 {
			nearbyResp, err := p.client.SearchNearby(callCtx, &placespb.SearchNearbyRequest{
				IncludedTypes:  includedTypes,
				MaxResultCount: 20,
				LocationRestriction: &placespb.SearchNearbyRequest_LocationRestriction{
					Type: &placespb.SearchNearbyRequest_LocationRestriction_Circle{Circle: circleArea},
				},
				RankPreference: placespb.SearchNearbyRequest_DISTANCE,
			})
			if err == nil {
				placesList = nearbyResp.Places
				usedNearby = true
			}
			// On error (e.g. an unsupported type), fall back to Text Search below.
		}
	}

	// Marine features (anchorages, moorings, coves) and wide-area searches use Text Search.
	if !usedNearby {
		rankPref := placespb.SearchTextRequest_DISTANCE
		if requestedRadius > 10000 {
			rankPref = placespb.SearchTextRequest_RELEVANCE
		}
		req := &placespb.SearchTextRequest{
			TextQuery:      args.Query,
			LocationBias:   locationBias,
			OpenNow:        args.OpenNow,
			MaxResultCount: 20,
			RankPreference: rankPref,
		}
		resp, err := p.client.SearchText(callCtx, req)
		if err != nil {
			return PlacesResponse{}, fmt.Errorf("%w: SearchText API: %w", ErrAPIUnavailable, err)
		}
		placesList = append(placesList, resp.Places...)

		// For wide-area searches (> 15 km / ~8 NM) on core categories (marina,
		// anchorage, mooring), also probe 4 cardinal sector points (N, E, S, W) at
		// 55% of the requested radius in parallel so open-water centers cover every
		// shore inside the circle without 8-way overlap.
		if requestedRadius > 15000 && (args.Latitude != 0 || args.Longitude != 0) && shouldSectorFanOut(args.Query) {
			sectorOffset := requestedRadius * 0.55
			sectorBiasRadius := requestedRadius * 0.55
			if sectorBiasRadius > 50000 {
				sectorBiasRadius = 50000
			}
			sectorNearbyTypes := wideAreaNearbyTypes(args.Query)
			bearings := []float64{0, 90, 180, 270}
			sectorResults := make([][]*placespb.Place, len(bearings))
			var wg sync.WaitGroup
			for i, brng := range bearings {
				wg.Add(1)
				go func(idx int, b float64) {
					defer wg.Done()
					sLat, sLng := offsetLatLng(args.Latitude, args.Longitude, sectorOffset, b)
					sCircle := &placespb.Circle{
						Center: &latlng.LatLng{Latitude: sLat, Longitude: sLng},
						Radius: sectorBiasRadius,
					}
					if len(sectorNearbyTypes) > 0 {
						if nResp, nErr := p.client.SearchNearby(callCtx, &placespb.SearchNearbyRequest{
							IncludedTypes:  sectorNearbyTypes,
							MaxResultCount: 20,
							LocationRestriction: &placespb.SearchNearbyRequest_LocationRestriction{
								Type: &placespb.SearchNearbyRequest_LocationRestriction_Circle{Circle: sCircle},
							},
							RankPreference: placespb.SearchNearbyRequest_DISTANCE,
						}); nErr == nil && nResp != nil && len(nResp.Places) > 0 {
							sectorResults[idx] = nResp.Places
							return
						}
					}
					sReq := &placespb.SearchTextRequest{
						TextQuery: args.Query,
						LocationBias: &placespb.SearchTextRequest_LocationBias{
							Type: &placespb.SearchTextRequest_LocationBias_Circle{Circle: sCircle},
						},
						OpenNow:        args.OpenNow,
						MaxResultCount: 20,
						RankPreference: placespb.SearchTextRequest_RELEVANCE,
					}
					if sResp, sErr := p.client.SearchText(callCtx, sReq); sErr == nil && sResp != nil {
						sectorResults[idx] = sResp.Places
					}
				}(i, brng)
			}
			wg.Wait()
			for _, sr := range sectorResults {
				placesList = append(placesList, sr...)
			}
		}
	}

	var results []PlaceResult
	seen := make(map[string]bool)
	for _, pt := range placesList {
		lat := 0.0
		lng := 0.0
		if pt.Location != nil {
			lat = pt.Location.Latitude
			lng = pt.Location.Longitude
		}

		// Filter against the full requested radius (not the 50km API circle cap)
		// so outer rims of 30-60 NM circles are preserved.
		filterRadius := requestedRadius

		dist := 0.0
		if lat != 0 && lng != 0 {
			dist = haversineDistance(args.Latitude, args.Longitude, lat, lng)
			if dist > filterRadius {
				continue
			}
		}

		name := ""
		if pt.DisplayName != nil {
			name = pt.DisplayName.Text
		}

		dedupKey := fmt.Sprintf("%s|%.4f|%.4f", strings.ToLower(strings.TrimSpace(name)), lat, lng)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		results = append(results, PlaceResult{
			Name:           name,
			Address:        pt.FormattedAddress,
			Latitude:       lat,
			Longitude:      lng,
			DistanceMeters: dist,
			BusinessStatus: pt.BusinessStatus.String(),
			Types:          pt.Types,
			WebsiteURI:     pt.GoogleMapsUri,
		})
	}

	return PlacesResponse{
		Places:          results,
		DebugDurationMS: time.Since(start).Milliseconds(),
	}, nil
}
