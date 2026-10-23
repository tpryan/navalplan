package tool

import (
	"log/slog"

	"google.golang.org/adk/v2/agent"
)

// Session state keys set by the backend when it creates a stop research
// session. agenttool copies parent state into sub-agent sessions, so they are
// visible to facility_discovery's tool calls too.
const (
	stateStopLatitude       = "stop_latitude"
	stateStopLongitude      = "stop_longitude"
	stateAreaRadiusMeters   = "area_radius_meters"
	StateKeyBriefingWeather = "briefing_weather"
	StateKeyBriefingTides   = "briefing_tides"
	StateKeyBriefingSun     = "briefing_sun"
	StateKeyVerifiedPlaces  = "verified_places"
)

// setSessionState safely writes a key-value pair to the ADK session state if
// available, recovering from mock contexts that panic on State().
func setSessionState(ctx agent.Context, key string, val any) {
	if ctx == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	st := ctx.State()
	if st == nil {
		return
	}
	if err := st.Set(key, val); err != nil {
		slog.WarnContext(ctx, "Failed to set session state", "key", key, "error", err)
	}
}

// getSessionState safely reads a key from the ADK session state if available.
func getSessionState(ctx agent.Context, key string) (val any, ok bool) {
	if ctx == nil {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			val, ok = nil, false
		}
	}()
	st := ctx.State()
	if st == nil {
		return nil, false
	}
	v, err := st.Get(key)
	if err != nil || v == nil {
		return nil, false
	}
	return v, true
}

// areaSearchRadius reads the voyage-wide area search radius (in meters) from
// session state when running Local Pilot / area discovery.
func areaSearchRadius(ctx agent.Context) (radiusMeters float64, ok bool) {
	if ctx == nil {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			radiusMeters, ok = 0, false
		}
	}()
	st := ctx.State()
	if st == nil {
		return 0, false
	}
	raw, err := st.Get(stateAreaRadiusMeters)
	if err != nil {
		return 0, false
	}
	r, okR := toFloat(raw)
	if !okR || r <= 0 {
		return 0, false
	}
	return r, true
}

// anchorOverrideNM is how far a place search may be centred from the stop
// before it is pulled back onto the stop. Stops use a few-NM radius, so a
// search centred more than 1 NM away is almost always the LLM substituting
// the named city's centre for the stop coordinates.
const anchorOverrideNM = 1.0

// stopAnchor reads the stop coordinates from session state. It returns false
// when no anchor is set or the context cannot provide state.
func stopAnchor(ctx agent.Context) (lat, lng float64, ok bool) {
	if ctx == nil {
		return 0, 0, false
	}
	// ADK tool/mock contexts without an initialized session state can panic on
	// ctx.State(); recover so stateless/test invocations degrade gracefully.
	defer func() {
		if recover() != nil {
			lat, lng, ok = 0, 0, false
		}
	}()
	st := ctx.State()
	if st == nil {
		return 0, 0, false
	}
	rawLat, errLat := st.Get(stateStopLatitude)
	rawLng, errLng := st.Get(stateStopLongitude)
	if errLat != nil || errLng != nil {
		return 0, 0, false
	}
	lat, okLat := toFloat(rawLat)
	lng, okLng := toFloat(rawLng)
	if !okLat || !okLng || (lat == 0 && lng == 0) {
		return 0, 0, false
	}
	return lat, lng, true
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// anchorCoords returns the coordinates to search around: the requested ones,
// or the stop's when the request is more than anchorOverrideNM away.
func anchorCoords(reqLat, reqLng, stopLat, stopLng float64) (float64, float64, bool) {
	distNM := haversineDistance(reqLat, reqLng, stopLat, stopLng) / 1852.0
	if distNM > anchorOverrideNM {
		return stopLat, stopLng, true
	}
	return reqLat, reqLng, false
}

// anchorRequest applies the stop anchor (if any) to a place search request
// and logs when it overrides the LLM-supplied coordinates.
func anchorRequest(ctx agent.Context, req PlacesRequest) PlacesRequest {
	stopLat, stopLng, ok := stopAnchor(ctx)
	if !ok {
		return req
	}
	lat, lng, overridden := anchorCoords(req.Latitude, req.Longitude, stopLat, stopLng)
	if overridden {
		slog.WarnContext(ctx, "Place search centred away from the stop; re-anchoring on stop coordinates",
			"query", req.Query,
			"requested_lat", req.Latitude, "requested_lng", req.Longitude,
			"stop_lat", stopLat, "stop_lng", stopLng)
		req.Latitude, req.Longitude = lat, lng
	}
	return req
}
