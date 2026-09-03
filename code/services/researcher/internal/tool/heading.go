package tool

import (
	"fmt"
	"math"
)

// HeadingRequest represents the input parameters for the CalculateHeading tool.
type HeadingRequest struct {
	FromLatitude     float64  `json:"from_latitude"`
	FromLongitude    float64  `json:"from_longitude"`
	ToLatitude       float64  `json:"to_latitude"`
	ToLongitude      float64  `json:"to_longitude"`
	FromLocation     string   `json:"from_location,omitempty"`
	ToLocation       string   `json:"to_location,omitempty"`
	WindDirectionDeg *float64 `json:"wind_direction_deg,omitempty"`
	WindSpeedKts     *float64 `json:"wind_speed_kts,omitempty"`
}

// HeadingResponse represents the structured result of the CalculateHeading tool.
type HeadingResponse struct {
	HeadingDegrees           float64  `json:"heading_degrees"`
	CardinalDirection        string   `json:"cardinal_direction"`
	DistanceNM               float64  `json:"distance_nm"`
	ReciprocalHeadingDegrees float64  `json:"reciprocal_heading_degrees"`
	RelativeWindAngle        *float64 `json:"relative_wind_angle,omitempty"`
	WindRelation             string   `json:"wind_relation,omitempty"`
	IsAdverseWind            bool     `json:"is_adverse_wind"`
	AdverseWindSeverity      string   `json:"adverse_wind_severity"`
	Summary                  string   `json:"summary"`
}

// CalculateHeadingResult performs spherical trigonometry and navigation analysis.
func CalculateHeadingResult(req HeadingRequest) *HeadingResponse {
	heading := CalculateBearing(req.FromLatitude, req.FromLongitude, req.ToLatitude, req.ToLongitude)
	heading = math.Round(heading*10) / 10
	cardinal := DegreesToDirection(heading)
	distNM := CalculateDistanceNM(req.FromLatitude, req.FromLongitude, req.ToLatitude, req.ToLongitude)
	distNM = math.Round(distNM*10) / 10
	reciprocal := CalculateReciprocalHeading(heading)
	reciprocal = math.Round(reciprocal*10) / 10

	resp := &HeadingResponse{
		HeadingDegrees:           heading,
		CardinalDirection:        cardinal,
		DistanceNM:               distNM,
		ReciprocalHeadingDegrees: reciprocal,
		AdverseWindSeverity:      "none",
	}

	fromDesc := req.FromLocation
	if fromDesc == "" {
		fromDesc = fmt.Sprintf("(%.4f, %.4f)", req.FromLatitude, req.FromLongitude)
	}
	toDesc := req.ToLocation
	if toDesc == "" {
		toDesc = fmt.Sprintf("(%.4f, %.4f)", req.ToLatitude, req.ToLongitude)
	}

	if req.WindDirectionDeg != nil && req.WindSpeedKts != nil {
		windDir := math.Mod(*req.WindDirectionDeg, 360)
		if windDir < 0 {
			windDir += 360
		}
		windSpeed := *req.WindSpeedKts
		relAngle := RelativeWindAngle(heading, windDir)
		relAngle = math.Round(relAngle*10) / 10
		resp.RelativeWindAngle = &relAngle

		switch {
		case relAngle <= 45:
			resp.WindRelation = "headwind"
			if windSpeed > 15 {
				resp.IsAdverseWind = true
				resp.AdverseWindSeverity = "warning"
			} else if windSpeed >= 5 {
				resp.IsAdverseWind = true
				resp.AdverseWindSeverity = "info"
			}
		case relAngle <= 135:
			resp.WindRelation = "beam_reach"
		default:
			resp.WindRelation = "tailwind"
		}

		windDesc := fmt.Sprintf("Wind from %.0f° at %.1f kt (%s, %.0f° off course)", windDir, windSpeed, resp.WindRelation, relAngle)
		if resp.IsAdverseWind {
			resp.Summary = fmt.Sprintf("Course from %s to %s is %.0f° (%s) for %.1f NM. %s — Adverse Wind (%s).",
				fromDesc, toDesc, heading, cardinal, distNM, windDesc, resp.AdverseWindSeverity)
		} else {
			resp.Summary = fmt.Sprintf("Course from %s to %s is %.0f° (%s) for %.1f NM. %s.",
				fromDesc, toDesc, heading, cardinal, distNM, windDesc)
		}
	} else {
		resp.Summary = fmt.Sprintf("Course from %s to %s is %.0f° (%s) for %.1f NM (reciprocal %.0f°).",
			fromDesc, toDesc, heading, cardinal, distNM, reciprocal)
	}

	return resp
}

// CalculateBearing calculates the initial bearing in degrees (0-360) from (lat1, lon1) to (lat2, lon2).
func CalculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	toDeg := func(r float64) float64 { return r * 180 / math.Pi }

	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	deltaLambda := toRad(lon2 - lon1)

	y := math.Sin(deltaLambda) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(deltaLambda)
	theta := math.Atan2(y, x)

	bearing := math.Mod(toDeg(theta)+360, 360)
	return bearing
}

// CalculateDistanceNM calculates great-circle distance in nautical miles using Haversine formula.
func CalculateDistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(d float64) float64 { return d * math.Pi / 180 }

	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	deltaPhi := toRad(lat2 - lat1)
	deltaLambda := toRad(lon2 - lon1)

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	const earthRadiusNM = 3440.065
	return earthRadiusNM * c
}

// CalculateReciprocalHeading returns the reverse bearing (deg + 180) mod 360.
func CalculateReciprocalHeading(deg float64) float64 {
	return math.Mod(deg+180, 360)
}

// RelativeWindAngle returns the angular difference (0 to 180 degrees) between course heading
// and the direction the wind is coming from.
func RelativeWindAngle(courseDeg, windDirDeg float64) float64 {
	diff := math.Mod(math.Abs(courseDeg-windDirDeg), 360)
	if diff > 180 {
		diff = 360 - diff
	}
	return diff
}
