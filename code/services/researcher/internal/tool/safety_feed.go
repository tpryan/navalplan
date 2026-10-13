package tool

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Navigational-warnings feed for GetSafetyAlerts.
//
// v1 reads active broadcast warnings from the NGA Maritime Safety Information
// (MSI) API and surfaces those near a stop. It is OFF by default and only makes
// a network call when NAVALPLAN_SAFETY_ALERTS_ENABLED is truthy, matching a
// cautious, read-only rollout. Any failure (disabled, network, parse) degrades
// gracefully to "no data" so the lookout audit is never blocked or seeded with
// fabricated hazards.
//
// NOTE: the NGA MSI broadcast-warn JSON schema below is based on the public API
// shape and MUST be verified against the live endpoint before enabling in
// production. The parser is exercised by unit tests against a saved sample.
const defaultSafetyAlertsURL = "https://msi.nga.mil/api/publications/broadcast-warn"

// defaultSafetyRadiusNM is used when the caller does not specify a radius.
// Broadcast warnings cover large areas, so the default is generous.
const defaultSafetyRadiusNM = 300.0

const safetyCacheTTL = time.Hour

// broadcastWarn is a single NGA MSI broadcast warning record.
type broadcastWarn struct {
	MsgYear   int    `json:"msgYear"`
	MsgNumber int    `json:"msgNumber"`
	NavArea   string `json:"navArea"`
	Subregion string `json:"subregion"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	IssueDate string `json:"issueDate"`
	Positions []struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"positions"`
}

type broadcastWarnResponse struct {
	BroadcastWarn []broadcastWarn `json:"broadcast-warn"`
}

type safetyCacheEntry struct {
	hazards []string
	level   string
	at      time.Time
}

var (
	safetyCacheMu sync.Mutex
	safetyCache   = map[string]safetyCacheEntry{}
)

// safetyAlertsEnabled reports whether the live feed is turned on.
func safetyAlertsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NAVALPLAN_SAFETY_ALERTS_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func safetyAlertsURL() string {
	if u := strings.TrimSpace(os.Getenv("NAVALPLAN_SAFETY_ALERTS_URL")); u != "" {
		return u
	}
	return defaultSafetyAlertsURL
}

// haversineNM returns the great-circle distance between two points in nautical miles.
func haversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNM = 3440.065
	rlat1 := lat1 * math.Pi / 180
	rlat2 := lat2 * math.Pi / 180
	dlat := (lat2 - lat1) * math.Pi / 180
	dlon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(rlat1)*math.Cos(rlat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return earthRadiusNM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

var securityKeywords = []string{"pirac", "pirate", "armed robbery", "armed attack", "hijack", "security", "hostile", "kidnap"}

// parseBroadcastWarnings filters active broadcast warnings to those with at
// least one position within radiusNM of (lat, lon) and returns concise hazard
// strings plus a derived security level. Cancelled/inactive warnings and those
// with no in-range position are skipped. It is a pure function for unit testing.
func parseBroadcastWarnings(data []byte, lat, lon, radiusNM float64) ([]string, string) {
	if radiusNM <= 0 {
		radiusNM = defaultSafetyRadiusNM
	}
	var resp broadcastWarnResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "unknown"
	}

	var hazards []string
	security := false
	for _, w := range resp.BroadcastWarn {
		// Only active warnings. NGA uses status "A" for active; treat empty as active.
		if w.Status != "" && !strings.EqualFold(w.Status, "A") {
			continue
		}
		inRange := false
		for _, p := range w.Positions {
			if haversineNM(lat, lon, p.Latitude, p.Longitude) <= radiusNM {
				inRange = true
				break
			}
		}
		if !inRange {
			continue
		}
		text := strings.TrimSpace(w.Text)
		if text == "" {
			continue
		}
		label := text
		if w.NavArea != "" && w.MsgNumber != 0 {
			label = "NAVAREA " + w.NavArea + " " + itoa(w.MsgNumber) + "/" + itoa(w.MsgYear) + ": " + text
		}
		hazards = append(hazards, label)

		lower := strings.ToLower(text)
		for _, kw := range securityKeywords {
			if strings.Contains(lower, kw) {
				security = true
				break
			}
		}
	}

	level := "normal"
	if security {
		level = "elevated"
	} else if len(hazards) > 0 {
		level = "advisories"
	}
	return hazards, level
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// fetchSafetyAlerts returns navigational warnings near a location, using a short
// per-area cache. It only performs network I/O when the feed is enabled; any
// error yields an empty result with "unknown" level so the caller can fall back
// gracefully.
func fetchSafetyAlerts(ctx context.Context, lat, lon, radiusNM float64) ([]string, string) {
	if !safetyAlertsEnabled() {
		return nil, "unknown"
	}
	if radiusNM <= 0 {
		radiusNM = defaultSafetyRadiusNM
	}

	// Cache per ~1-degree cell to avoid refetching for nearby stops.
	key := itoa(int(math.Round(lat))) + "," + itoa(int(math.Round(lon)))
	safetyCacheMu.Lock()
	if e, ok := safetyCache[key]; ok && time.Since(e.at) < safetyCacheTTL {
		safetyCacheMu.Unlock()
		return e.hazards, e.level
	}
	safetyCacheMu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, safetyAlertsURL(), nil)
	if err != nil {
		return nil, "unknown"
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "unknown"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "unknown"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, "unknown"
	}

	hazards, level := parseBroadcastWarnings(body, lat, lon, radiusNM)

	safetyCacheMu.Lock()
	safetyCache[key] = safetyCacheEntry{hazards: hazards, level: level, at: time.Now()}
	safetyCacheMu.Unlock()

	return hazards, level
}
