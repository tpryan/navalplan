package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/maps/places/apiv1/placespb"
	"github.com/charmbracelet/log"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/latlng"
	"google.golang.org/grpc/metadata"

	places "cloud.google.com/go/maps/places/apiv1"
)

type PlacesArgs struct {
	Query     string  `json:"query" description:"Text query (e.g. 'restaurants', 'marinas')."`
	Latitude  float64 `json:"latitude" description:"Latitude for location bias."`
	Longitude float64 `json:"longitude" description:"Longitude for location bias."`
	Radius    float64 `json:"radius" description:"Search radius in meters. Default 5000."`
	OpenNow   bool    `json:"open_now" description:"If true, only return places currently open."`
	MinRating float64 `json:"min_rating" description:"Minimum rating (1.0 - 5.0)."`
}

type PlaceResult struct {
	Name            string   `json:"name"`
	Address         string   `json:"address"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	Rating          float64  `json:"rating"`
	UserRatingCount int32    `json:"user_rating_count"`
	BusinessStatus  string   `json:"business_status"`
	Types           []string `json:"types"`
	WebsiteURI      string   `json:"website_uri,omitempty"`
}

type PlacesResponse struct {
	Places          []PlaceResult `json:"places"`
	DebugDurationMS int64         `json:"debug_duration_ms"`
	Error           string        `json:"error,omitempty"`
}

func NewPlacesTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "find_places_nearby",
		Description: "Finds places (e.g. marinas, restaurants) near a location using Google Maps Text Search. Returns specific locations with Lat/Lng.",
	}, func(ctx tool.Context, args PlacesArgs) (PlacesResponse, error) {
		return FindPlaces(args)
	})
}

func FindPlaces(args PlacesArgs) (PlacesResponse, error) {
	start := time.Now()
	log.Debugf("tool:find_places_nearby Query='%s' at %f, %f (r=%f)", args.Query, args.Latitude, args.Longitude, args.Radius)

	ctx := context.Background()

	var clientOpts []option.ClientOption
	if key := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY"); key != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(key))
	}

	c, err := places.NewClient(ctx, clientOpts...)
	if err != nil {
		return PlacesResponse{Error: fmt.Sprintf("Failed to create Places client: %v", err)}, nil
	}
	defer c.Close()

	// Default radius if 0
	radius := args.Radius
	if radius <= 0 {
		radius = 5000 // 5km default
	}

	centerPoint := &latlng.LatLng{
		Latitude:  args.Latitude,
		Longitude: args.Longitude,
	}

	circleArea := &placespb.Circle{
		Center: centerPoint,
		Radius: radius,
	}

	locationBias := &placespb.SearchTextRequest_LocationBias{
		Type: &placespb.SearchTextRequest_LocationBias_Circle{
			Circle: circleArea,
		},
	}

	// Define fields to return (FieldMask)
	// Basic fields + location + rating + website
	fieldsToRequest := []string{
		"places.displayName",
		"places.formattedAddress",
		"places.location",
		"places.rating",
		"places.userRatingCount",
		"places.businessStatus",
		"places.types",
		"places.websiteUri",
	}
	fieldMaskHeader := strings.Join(fieldsToRequest, ",")

	req := &placespb.SearchTextRequest{
		TextQuery:    args.Query,
		LocationBias: locationBias,
		OpenNow:      args.OpenNow,
		MinRating:    args.MinRating,
	}

	// Append FieldMask to context
	ctx = metadata.AppendToOutgoingContext(ctx, "x-goog-fieldmask", fieldMaskHeader)
	// Add API Key explicitly if needed, but usually ADC or env var handles it.
	// The client library should pick up GOOGLE_APPLICATION_CREDENTIALS or use API Key from options if provided.
	// For ADK/Gemini projects, often GEMINI_API_KEY is for Gemini, but Maps might need GOOGLE_MAPS_API_KEY.
	// The `maps.NewClient` uses `option.WithAPIKey` if we pass it.
	// Let's check if we need to pass the API Key explicitly.
	// The user mentioned "Updated environment variables to use GOOGLE_MAPS_API_KEY".
	// The standard `places.NewClient` uses default credential chain.
	// If `GOOGLE_MAPS_API_KEY` is set, we should probably use it.

	// However, `places.NewClient` from `cloud.google.com/go/maps/places/apiv1` is a gRPC client.
	// It usually expects `GOOGLE_APPLICATION_CREDENTIALS` (Service Account) OR an API Key.
	// Let's assume the environment is set up correctly or we might need to modify `NewClient`.
	// Since I cannot change the `NewClient` call inside `FindPlaces` easily without passing options,
	// I will check if I can pass options.

	// Re-creating client with options if key exists.
	// But `places.NewClient` takes `...option.ClientOption`.
	// I need to import "google.golang.org/api/option".

	// Wait, I can't easily add imports to a file I'm writing in one go unless I include them.
	// I'll assume standard auth for now. If it fails, I'll fix it.
	// Actually, for Maps Platform, API Key is common.
	// Let's rely on standard auth first.

	resp, err := c.SearchText(ctx, req)
	if err != nil {
		log.Errorf("SearchText failed: %v", err)
		return PlacesResponse{Error: fmt.Sprintf("SearchText API failed: %v", err)}, nil
	}

	var results []PlaceResult
	for _, p := range resp.Places {
		lat := 0.0
		lng := 0.0
		if p.Location != nil {
			lat = p.Location.Latitude
			lng = p.Location.Longitude
		}

		name := ""
		if p.DisplayName != nil {
			name = p.DisplayName.Text
		}

		results = append(results, PlaceResult{
			Name:            name,
			Address:         p.FormattedAddress,
			Latitude:        lat,
			Longitude:       lng,
			Rating:          float64(p.Rating),
			UserRatingCount: p.GetUserRatingCount(),
			BusinessStatus:  p.BusinessStatus.String(),
			Types:           p.Types,
			WebsiteURI:      p.WebsiteUri,
		})
	}

	return PlacesResponse{
		Places:          results,
		DebugDurationMS: time.Since(start).Milliseconds(),
	}, nil
}
