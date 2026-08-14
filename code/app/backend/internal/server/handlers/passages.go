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

	"app/internal/model"
)

const passageLocationName = "Passage Leg (Extrapolated)"

func truncateToDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func daysBetween(a, b time.Time) int {
	return int(math.Round(truncateToDay(b).Sub(truncateToDay(a)).Hours() / 24))
}

func (h *Handler) InterpolatePassagePoints(ctx context.Context, voyageID int64) ([]model.Stop, error) {
	landfalls, err := h.DB.ListLandfallStops(ctx, voyageID)
	if err != nil {
		return nil, fmt.Errorf("list landfall stops: %w", err)
	}

	if err := h.DB.DeletePassagePoints(ctx, voyageID); err != nil {
		return nil, fmt.Errorf("clear passage points: %w", err)
	}

	if len(landfalls) < 2 {
		return nil, nil
	}

	var created []model.Stop

	for i := 0; i < len(landfalls)-1; i++ {
		start := landfalls[i]
		end := landfalls[i+1]

		total := daysBetween(start.TargetDate, end.TargetDate)
		if total <= 1 {
			continue
		}

		for k := 1; k < total; k++ {
			frac := float64(k) / float64(total)
			lat := start.Latitude + frac*(end.Latitude-start.Latitude)
			lon := start.Longitude + frac*(end.Longitude-start.Longitude)

			lat, lon = h.snapToWater(ctx, lat, lon, start.Latitude, start.Longitude, end.Latitude, end.Longitude)

			date := truncateToDay(start.TargetDate).AddDate(0, 0, k)

			existing, err := h.DB.GetStopByDate(ctx, voyageID, date)
			if err != nil {
				return nil, fmt.Errorf("check stop on %s: %w", date.Format("2006-01-02"), err)
			}
			if existing != nil {
				continue
			}

			point := &model.Stop{
				VoyageID:         voyageID,
				TargetDate:       date,
				StopType:         model.StopTypePassagePoint,
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

			if err := h.DB.CreateStop(ctx, point); err != nil {
				return nil, fmt.Errorf("create passage point on %s: %w", date.Format("2006-01-02"), err)
			}
			created = append(created, *point)
		}
	}

	slog.InfoContext(ctx, "Interpolated passage points", "voyage_id", voyageID, "count", len(created))
	return created, nil
}

func (h *Handler) researchPassagePoints(points []model.Stop) {
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

var waterProbeRadiiNM = []float64{3, 6, 10, 16, 24}

func (h *Handler) snapToWater(ctx context.Context, lat, lng, startLat, startLng, endLat, endLng float64) (float64, float64) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	water, err := isWaterPoint(ctx, lat, lng)
	if err != nil {
		slog.WarnContext(ctx, "Water check unavailable; keeping interpolated point", "err", err)
		return lat, lng
	}
	if water {
		return lat, lng
	}

	routeBearing := initialBearing(startLat, startLng, endLat, endLng)
	for _, r := range waterProbeRadiiNM {
		for _, side := range []float64{90, -90} {
			cLat, cLng := offsetPoint(lat, lng, routeBearing+side, r)
			ok, err := isWaterPoint(ctx, cLat, cLng)
			if err != nil {
				return lat, lng
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

var landFeatureTypes = map[string]bool{
	"street_address": true,
	"route":          true,
	"premise":        true,
	"subpremise":     true,
	"intersection":   true,
}

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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
		return true, nil
	case "OK":
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

func initialBearing(lat1, lng1, lat2, lng2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δλ := (lng2 - lng1) * math.Pi / 180
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

func offsetPoint(lat, lng, bearingDeg, distNM float64) (float64, float64) {
	latOff := (distNM / 60.0) * math.Cos(bearingDeg*math.Pi/180)
	lngOff := (distNM / 60.0) * math.Sin(bearingDeg*math.Pi/180) / math.Cos(lat*math.Pi/180)
	return lat + latOff, lng + lngOff
}
