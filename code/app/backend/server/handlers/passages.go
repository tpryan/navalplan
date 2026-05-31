package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"

	"app/models"
)

// passageLocationName is the placeholder name applied to extrapolated at-sea positions.
const passageLocationName = "Passage Leg (Extrapolated)"

// truncateToDay normalizes a timestamp to midnight UTC so date math ignores time-of-day.
func truncateToDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// daysBetween returns the whole number of calendar days from a to b.
func daysBetween(a, b time.Time) int {
	return int(math.Round(truncateToDay(b).Sub(truncateToDay(a)).Hours() / 24))
}

// InterpolatePassagePoints recomputes the at-sea passage points for a voyage. Whenever the
// timeline or landfall stops change, this fills any multi-day gaps between consecutive
// landfalls with linearly interpolated "passage_point" stops so the researcher loop can
// fetch offshore weather for days spent under way.
//
// It returns the freshly created passage points so the caller can trigger research for them.
func (h *Handler) InterpolatePassagePoints(ctx context.Context, voyageID int64) ([]models.Stop, error) {
	// 1. Query existing landfalls in chronological order.
	landfalls, err := h.DB.ListLandfallStops(ctx, voyageID)
	if err != nil {
		return nil, fmt.Errorf("list landfall stops: %w", err)
	}

	// Clear any previously extrapolated points so stale legs don't linger after edits.
	if err := h.DB.DeletePassagePoints(ctx, voyageID); err != nil {
		return nil, fmt.Errorf("clear passage points: %w", err)
	}

	if len(landfalls) < 2 {
		// Nothing to bridge between fewer than two known positions.
		return nil, nil
	}

	var created []models.Stop

	// 2. Detect chronological gaps between successive landfalls.
	for i := 0; i < len(landfalls)-1; i++ {
		start := landfalls[i]
		end := landfalls[i+1]

		total := daysBetween(start.TargetDate, end.TargetDate)
		if total <= 1 {
			// Consecutive days (or same day) — no passage in between.
			continue
		}

		// 3. Calculate intermediary vector steps for each missing day k (0 < k < total).
		for k := 1; k < total; k++ {
			frac := float64(k) / float64(total)
			lat := start.Latitude + frac*(end.Latitude-start.Latitude)
			lon := start.Longitude + frac*(end.Longitude-start.Longitude)

			// The straight rhumb line between two coastal stops can clip a headland or
			// island, leaving the extrapolated position on dry land. Nudge it to open
			// water so the weather/tide lookup reflects a real at-sea position.
			lat, lon = h.snapToWater(ctx, lat, lon, start.Latitude, start.Longitude, end.Latitude, end.Longitude)

			date := truncateToDay(start.TargetDate).AddDate(0, 0, k)

			// Defensive: never clobber an existing stop on this date.
			existing, err := h.DB.GetStopByDate(ctx, voyageID, date)
			if err != nil {
				return nil, fmt.Errorf("check stop on %s: %w", date.Format("2006-01-02"), err)
			}
			if existing != nil {
				continue
			}

			point := &models.Stop{
				VoyageID:         voyageID,
				TargetDate:       date,
				StopType:         models.StopTypePassagePoint,
				LocationName:     passageLocationName,
				Latitude:         lat,
				Longitude:        lon,
				SearchRadius:     start.SearchRadius,
				SearchRadiusUnit: start.SearchRadiusUnit,
			}
			if point.SearchRadius <= 0 {
				point.SearchRadius = 60
			}
			if point.SearchRadiusUnit == "" {
				point.SearchRadiusUnit = "nm"
			}

			// 4. Upsert the passage record.
			if err := h.DB.CreateStop(ctx, point); err != nil {
				return nil, fmt.Errorf("create passage point on %s: %w", date.Format("2006-01-02"), err)
			}
			created = append(created, *point)
		}
	}

	slog.InfoContext(ctx, "Interpolated passage points", "voyage_id", voyageID, "count", len(created))
	return created, nil
}

// researchPassagePoints triggers the async research loop for the given passage points so
// offshore weather data is pulled without blocking the request.
func (h *Handler) researchPassagePoints(points []models.Stop) {
	for _, p := range points {
		stop := p
		jobKey := fmt.Sprintf("stop:%d", stop.ID)
		if !h.tryClaimJob(jobKey) {
			continue
		}
		sessionID := fmt.Sprintf("passage_%d_%d", stop.ID, time.Now().Unix())
		h.ensureProgressChannel(sessionID, 25*time.Minute)
		go h.performStopResearch(&stop, sessionID, jobKey)
	}
}

// waterProbeRadiiNM are the offsets, in nautical miles, tried when nudging a land-bound
// passage point toward open water. Probed nearest-first so the result moves as little as
// possible from the interpolated position.
var waterProbeRadiiNM = []float64{3, 6, 10, 16, 24}

// snapToWater returns a position guaranteed (best-effort) to be on open water. If the
// interpolated point is already over water it is returned unchanged. Otherwise the point
// is nudged perpendicular to the route's rhumb line (the open sea typically lies to one
// side of a clipped headland) at increasing distances, returning the nearest water hit.
//
// Water detection uses reverse geocoding: open-water coordinates resolve only to a Plus
// Code, while land resolves to addressable features (street_address, route, premise, …).
// If geocoding is unavailable or no water is found, the original point is returned so the
// feature degrades gracefully rather than failing the whole interpolation.
func (h *Handler) snapToWater(ctx context.Context, lat, lng, startLat, startLng, endLat, endLng float64) (float64, float64) {
	// Bound the whole snap so a slow geocoder can't stall the extend request.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	water, err := isWaterPoint(ctx, lat, lng)
	if err != nil {
		// Geocoding unavailable (e.g. key restrictions) — keep the linear estimate.
		slog.WarnContext(ctx, "Water check unavailable; keeping interpolated point", "err", err)
		return lat, lng
	}
	if water {
		return lat, lng
	}

	// Perpendicular to the route bearing is the most likely direction to open sea.
	routeBearing := initialBearing(startLat, startLng, endLat, endLng)
	for _, r := range waterProbeRadiiNM {
		for _, side := range []float64{90, -90} {
			cLat, cLng := offsetPoint(lat, lng, routeBearing+side, r)
			ok, err := isWaterPoint(ctx, cLat, cLng)
			if err != nil {
				return lat, lng // bail to original on error
			}
			if ok {
				slog.InfoContext(ctx, "Snapped passage point to water", "offset_nm", r)
				return cLat, cLng
			}
		}
	}

	slog.WarnContext(ctx, "No open water found near interpolated point; keeping it as-is")
	return lat, lng
}

// landFeatureTypes are reverse-geocode result types that only exist on land. Open-water
// coordinates never resolve to these; they fall back to a Plus Code.
var landFeatureTypes = map[string]bool{
	"street_address": true,
	"route":          true,
	"premise":        true,
	"subpremise":     true,
	"intersection":   true,
}

// isWaterPoint reports whether a coordinate is over open water, per reverse geocoding.
func isWaterPoint(ctx context.Context, lat, lng float64) (bool, error) {
	apiKey := os.Getenv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if apiKey == "" {
		return false, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY not set")
	}

	endpoint := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?latlng=%f,%f&key=%s",
		lat, lng, url.QueryEscape(apiKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result struct {
		Status  string `json:"status"`
		Results []struct {
			Types []string `json:"types"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	switch result.Status {
	case "ZERO_RESULTS":
		// No feature at all — open water.
		return true, nil
	case "OK":
		// Land if any returned feature is an addressable land type.
		for _, r := range result.Results {
			for _, t := range r.Types {
				if landFeatureTypes[t] {
					return false, nil
				}
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("geocode status: %s", result.Status)
	}
}

// initialBearing returns the initial great-circle bearing from point 1 to point 2, in degrees.
func initialBearing(lat1, lng1, lat2, lng2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δλ := (lng2 - lng1) * math.Pi / 180
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

// offsetPoint returns the coordinate reached by traveling distNM nautical miles from
// (lat,lng) along the given compass bearing (degrees).
func offsetPoint(lat, lng, bearingDeg, distNM float64) (float64, float64) {
	latOff := (distNM / 60.0) * math.Cos(bearingDeg*math.Pi/180)
	lngOff := (distNM / 60.0) * math.Sin(bearingDeg*math.Pi/180) / math.Cos(lat*math.Pi/180)
	return lat + latOff, lng + lngOff
}
