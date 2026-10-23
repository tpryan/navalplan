package tool

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const StateKeyCandidateRegions = "candidate_regions"

type CandidateRegionsArgs struct {
	Month int `json:"month"`
}

type CanonicalRegion struct {
	Name                string         `json:"name"`
	MacroZone           string         `json:"macro_zone"`
	Type                string         `json:"type"`
	Tier                string         `json:"tier"`
	IsHiddenGem         bool           `json:"is_hidden_gem"`
	PrimeMonths         []int          `json:"prime_months"`
	DefaultScore        int            `json:"suitability_score"`
	AvgWindSpeedKnots   int            `json:"avg_wind_speed_knots"`
	AvgTempC            int            `json:"avg_temp_c"`
	BaselineNotes       string         `json:"baseline_notes"`
	Geometry            map[string]any `json:"geometry"`
}

type CandidateRegionsResponse struct {
	Month   int               `json:"month"`
	Regions []CanonicalRegion `json:"regions"`
	Digest  string            `json:"digest"`
}

type RegionsProvider struct{}

func NewCandidateRegionsTool() (tool.Tool, *RegionsProvider, error) {
	rp := &RegionsProvider{}
	t, err := functiontool.New(functiontool.Config{
		Name:        "GetCandidateRegionsForMonth",
		Description: "Returns canonical, non-overlapping global sailing regions and verified GeoJSON bounding polygons across all 6 global macro zones (North America, Caribbean/Atlantic, Europe/Med, Asia, Oceania, South America) and 4 tiers (Standard, Hidden Gem, Regional Favorite, Challenging) in season for the requested month (1-12).",
	}, rp.GetCandidateRegions)
	return t, rp, err
}

func (rp *RegionsProvider) Close() error {
	return nil
}

func (rp *RegionsProvider) GetCandidateRegions(ctx agent.Context, args CandidateRegionsArgs) (CandidateRegionsResponse, error) {
	month := args.Month
	if month < 1 || month > 12 {
		month = 6
	}
	var matched []CanonicalRegion
	var names []string
	for _, reg := range canonicalSailingRegions {
		if inMonthList(month, reg.PrimeMonths) {
			matched = append(matched, reg)
			names = append(names, fmt.Sprintf("%s (%s/%s)", reg.Name, reg.MacroZone, reg.Tier))
		}
	}
	resp := CandidateRegionsResponse{
		Month:   month,
		Regions: matched,
		Digest:  fmt.Sprintf("Month %d has %d canonical non-overlapping candidate regions: %s", month, len(matched), strings.Join(names, ", ")),
	}
	setSessionState(ctx, StateKeyCandidateRegions, resp)
	return resp, nil
}

func inMonthList(m int, list []int) bool {
	for _, x := range list {
		if x == m {
			return true
		}
	}
	return false
}

func rectPolygon(minLng, minLat, maxLng, maxLat float64) map[string]any {
	return map[string]any{
		"type": "Polygon",
		"coordinates": [][][]float64{
			{
				{minLng, minLat},
				{maxLng, minLat},
				{maxLng, maxLat},
				{minLng, maxLat},
				{minLng, minLat},
			},
		},
	}
}

var canonicalSailingRegions = []CanonicalRegion{
	{
		Name:              "British Virgin Islands",
		MacroZone:         "Caribbean / Atlantic",
		Type:              "Island Group",
		Tier:              "Standard",
		IsHiddenGem:       false,
		PrimeMonths:       []int{1, 2, 3, 4, 5, 11, 12},
		DefaultScore:      95,
		AvgWindSpeedKnots: 16,
		AvgTempC:          27,
		BaselineNotes:     "Steady Easterly trades, line-of-sight navigation inside the Sir Francis Drake Channel.",
		Geometry:          rectPolygon(-64.85, 18.30, -64.25, 18.80),
	},
	{
		Name:              "Grenada & The Grenadines",
		MacroZone:         "Caribbean / Atlantic",
		Type:              "Island Group",
		Tier:              "Hidden Gem",
		IsHiddenGem:       true,
		PrimeMonths:       []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		DefaultScore:      88,
		AvgWindSpeedKnots: 17,
		AvgTempC:          28,
		BaselineNotes:     "South of the primary hurricane belt (<12°N), offering year-round trade-wind cruising and summer hurricane-hole arbitrage.",
		Geometry:          rectPolygon(-61.85, 11.95, -61.10, 13.25),
	},
	{
		Name:              "Sea of Cortez (Baja California)",
		MacroZone:         "North America",
		Type:              "Coastal",
		Tier:              "Hidden Gem",
		IsHiddenGem:       true,
		PrimeMonths:       []int{3, 4, 5, 10, 11},
		DefaultScore:      90,
		AvgWindSpeedKnots: 14,
		AvgTempC:          26,
		BaselineNotes:     "Protected desert coves between La Paz and Loreto after winter Northers subside and before mid-summer heat.",
		Geometry:          rectPolygon(-111.50, 24.00, -109.80, 26.20),
	},
	{
		Name:              "San Juan Islands & Salish Sea",
		MacroZone:         "North America",
		Type:              "Island Group",
		Tier:              "Regional Favorite",
		IsHiddenGem:       false,
		PrimeMonths:       []int{6, 7, 8, 9},
		DefaultScore:      91,
		AvgWindSpeedKnots: 12,
		AvgTempC:          20,
		BaselineNotes:     "Rain-shadow summer cruising with deep fjords, strong tidal passes, and abundant state park moorings.",
		Geometry:          rectPolygon(-123.35, 48.35, -122.55, 48.85),
	},
	{
		Name:              "Maine Coast (Penobscot Bay)",
		MacroZone:         "North America",
		Type:              "Coastal",
		Tier:              "Hidden Gem",
		IsHiddenGem:       true,
		PrimeMonths:       []int{6, 7, 8, 9},
		DefaultScore:      89,
		AvgWindSpeedKnots: 13,
		AvgTempC:          21,
		BaselineNotes:     "Hundreds of granite island gunkholes and Afternoon SW sea breezes.",
		Geometry:          rectPolygon(-69.25, 43.85, -68.20, 44.50),
	},
	{
		Name:              "San Francisco Bay & Golden Gate",
		MacroZone:         "North America",
		Type:              "Coastal",
		Tier:              "Challenging",
		IsHiddenGem:       false,
		PrimeMonths:       []int{4, 5, 6, 7, 8, 9, 10},
		DefaultScore:      86,
		AvgWindSpeedKnots: 24,
		AvgTempC:          18,
		BaselineNotes:     "Thermal acceleration through The Slot (20-30 kts) paired with strong Golden Gate tidal currents.",
		Geometry:          rectPolygon(-122.60, 37.55, -122.15, 38.10),
	},
	{
		Name:              "Dalmatian Coast (Croatia)",
		MacroZone:         "Europe / Mediterranean",
		Type:              "Island Group",
		Tier:              "Standard",
		IsHiddenGem:       false,
		PrimeMonths:       []int{5, 6, 7, 8, 9, 10},
		DefaultScore:      94,
		AvgWindSpeedKnots: 14,
		AvgTempC:          25,
		BaselineNotes:     "Over 1,000 islands between Split and Dubrovnik with reliable afternoon Maestral thermal winds.",
		Geometry:          rectPolygon(15.80, 42.60, 17.80, 43.80),
	},
	{
		Name:              "Cyclades & Aegean Sea (Greece)",
		MacroZone:         "Europe / Mediterranean",
		Type:              "Island Group",
		Tier:              "Challenging",
		IsHiddenGem:       false,
		PrimeMonths:       []int{5, 6, 7, 8, 9, 10},
		DefaultScore:      89,
		AvgWindSpeedKnots: 22,
		AvgTempC:          26,
		BaselineNotes:     "Summer Meltemi winds blowing 20-30 kts across open island fetches.",
		Geometry:          rectPolygon(24.20, 36.60, 25.80, 37.90),
	},
	{
		Name:              "The Solent & English Channel",
		MacroZone:         "Europe / Mediterranean",
		Type:              "Coastal",
		Tier:              "Challenging",
		IsHiddenGem:       false,
		PrimeMonths:       []int{5, 6, 7, 8, 9},
		DefaultScore:      85,
		AvgWindSpeedKnots: 19,
		AvgTempC:          19,
		BaselineNotes:     "Complex double high water, fast spring tidal streams, and iconic regatta waters.",
		Geometry:          rectPolygon(-1.65, 50.60, -0.90, 50.90),
	},
	{
		Name:              "Andaman Sea (Phuket & Phang Nga Bay)",
		MacroZone:         "Asia",
		Type:              "Island Group",
		Tier:              "Standard",
		IsHiddenGem:       false,
		PrimeMonths:       []int{1, 2, 3, 4, 11, 12},
		DefaultScore:      93,
		AvgWindSpeedKnots: 14,
		AvgTempC:          29,
		BaselineNotes:     "Dry NE monsoon brings flat water and consistent breezes among limestone karsts.",
		Geometry:          rectPolygon(98.20, 7.60, 99.10, 8.40),
	},
	{
		Name:              "Seto Inland Sea (Japan)",
		MacroZone:         "Asia",
		Type:              "Island Group",
		Tier:              "Hidden Gem",
		IsHiddenGem:       true,
		PrimeMonths:       []int{4, 5, 10, 11},
		DefaultScore:      88,
		AvgWindSpeedKnots: 13,
		AvgTempC:          21,
		BaselineNotes:     "Sheltered historic island cruising with dramatic tidal gates like Naruto and Kurushima Straits.",
		Geometry:          rectPolygon(132.40, 33.90, 134.50, 34.60),
	},
	{
		Name:              "Whitsunday Islands (Australia)",
		MacroZone:         "Oceania",
		Type:              "Island Group",
		Tier:              "Standard",
		IsHiddenGem:       false,
		PrimeMonths:       []int{4, 5, 6, 7, 8, 9, 10, 11},
		DefaultScore:      94,
		AvgWindSpeedKnots: 18,
		AvgTempC:          25,
		BaselineNotes:     "Protected inside the Great Barrier Reef with steady SE trade winds during the austral winter/spring.",
		Geometry:          rectPolygon(148.60, -20.55, 149.25, -19.95),
	},
	{
		Name:              "Bay of Islands & Hauraki Gulf (New Zealand)",
		MacroZone:         "Oceania",
		Type:              "Coastal",
		Tier:              "Regional Favorite",
		IsHiddenGem:       false,
		PrimeMonths:       []int{1, 2, 3, 11, 12},
		DefaultScore:      92,
		AvgWindSpeedKnots: 16,
		AvgTempC:          22,
		BaselineNotes:     "Prime austral summer cruising ground with hundreds of deep-water coves and volcanic islands.",
		Geometry:          rectPolygon(174.00, -35.40, 174.60, -35.00),
	},
	{
		Name:              "Angra dos Reis & Ilha Grande (Brazil)",
		MacroZone:         "South America",
		Type:              "Island Group",
		Tier:              "Regional Favorite",
		IsHiddenGem:       false,
		PrimeMonths:       []int{1, 2, 3, 4, 5, 9, 10, 11, 12},
		DefaultScore:      89,
		AvgWindSpeedKnots: 13,
		AvgTempC:          26,
		BaselineNotes:     "365 rainforest-covered islands inside sheltered bays south of Rio de Janeiro.",
		Geometry:          rectPolygon(-44.65, -23.30, -44.00, -22.90),
	},
}
