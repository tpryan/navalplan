package handlers

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"app/internal/model"
)

func TestHaversine(t *testing.T) {
	tests := []struct {
		name     string
		lat1     float64
		lon1     float64
		lat2     float64
		lon2     float64
		unit     string
		expected float64
		tol      float64
	}{
		{
			name:     "Same point",
			lat1:     18.45,
			lon1:     -64.62,
			lat2:     18.45,
			lon2:     -64.62,
			unit:     "nm",
			expected: 0.0,
			tol:      0.001,
		},
		{
			name:     "1 degree Latitude (approx 60nm)",
			lat1:     18.0,
			lon1:     -64.0,
			lat2:     19.0,
			lon2:     -64.0,
			unit:     "nm",
			expected: 60.0,
			tol:      0.5,
		},
		{
			name:     "Tortola to Virgin Gorda (approx 10nm)",
			lat1:     18.42,
			lon1:     -64.61,
			lat2:     18.45,
			lon2:     -64.44,
			unit:     "nm",
			expected: 9.8,
			tol:      0.5,
		},
		{
			name:     "KM check - London to Paris",
			lat1:     51.5074,
			lon1:     -0.1278,
			lat2:     48.8566,
			lon2:     2.3522,
			unit:     "km",
			expected: 344.0,
			tol:      1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversine(tt.lat1, tt.lon1, tt.lat2, tt.lon2, tt.unit)
			if math.Abs(got-tt.expected) > tt.tol {
				t.Errorf("haversine() = %v, want %v (tol %v)", got, tt.expected, tt.tol)
			}
		})
	}
}

func TestSearchBoundaryHint(t *testing.T) {
	tests := []struct {
		name      string
		lat       float64
		lng       float64
		radiusNM  float64
		expectedN string
		expectedS string
		expectedE string
		expectedW string
	}{
		{
			name:      "Dover / English Channel (Voyage 62)",
			lat:       50.9702,
			lng:       1.3578,
			radiusNM:  46,
			expectedN: "North edge (51.74°N, 1.36°E)",
			expectedS: "South edge (50.20°N, 1.36°E)",
			expectedE: "East edge (50.97°N, 2.58°E)",
			expectedW: "West edge (50.97°N, 0.14°E)",
		},
		{
			name:      "BVI / Caribbean (West Longitude)",
			lat:       18.45,
			lng:       -64.62,
			radiusNM:  30,
			expectedN: "North edge (18.95°N, 64.62°W)",
			expectedS: "South edge (17.95°N, 64.62°W)",
			expectedE: "East edge (18.45°N, 64.09°W)",
			expectedW: "West edge (18.45°N, 65.15°W)",
		},
		{
			name:      "Sydney / Southern Hemisphere",
			lat:       -33.86,
			lng:       151.20,
			radiusNM:  30,
			expectedN: "North edge (33.36°S, 151.20°E)",
			expectedS: "South edge (34.36°S, 151.20°E)",
			expectedE: "East edge (33.86°S, 151.80°E)",
			expectedW: "West edge (33.86°S, 150.60°E)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchBoundaryHint(tt.lat, tt.lng, tt.radiusNM)
			if !strings.Contains(got, tt.expectedN) {
				t.Errorf("searchBoundaryHint() missing north boundary %s, got: %s", tt.expectedN, got)
			}
			if !strings.Contains(got, tt.expectedS) {
				t.Errorf("searchBoundaryHint() missing south boundary %s, got: %s", tt.expectedS, got)
			}
			if !strings.Contains(got, tt.expectedE) {
				t.Errorf("searchBoundaryHint() missing east boundary %s, got: %s", tt.expectedE, got)
			}
			if !strings.Contains(got, tt.expectedW) {
				t.Errorf("searchBoundaryHint() missing west boundary %s, got: %s", tt.expectedW, got)
			}
		})
	}
}

func TestNormalizeRecommendationRole_RegionAgnostic(t *testing.T) {
	tests := []struct {
		name            string
		recName         string
		recType         string
		rawRole         string
		desc            string
		reasoning       string
		stayDetailsJSON string
		wantRole        string
	}{
		{
			name:            "BVI Road Town Moorings base with charter companies",
			recName:         "The Moorings Marina (Wickhams Cay II, Road Town)",
			recType:         "Marina",
			rawRole:         "start_end_base",
			desc:            "Primary bareboat charter fleet embarkation marina in Road Town, Tortola.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["The Moorings","Sunsail"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "BVI Parham Town / Hodges Creek Marina with Dream Yacht Charter",
			recName:         "Hodges Creek Marina (Parham Town, East End Tortola)",
			recType:         "Marina",
			rawRole:         "overnight_stop",
			desc:            "Major charter base for Dream Yacht Charter and MarineMax in East End Tortola.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["Dream Yacht Charter","MarineMax"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "BVI Scrub Island Resort Marina without charter fleet demoted to overnight_stop",
			recName:         "Scrub Island Resort, Spa & Marina",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Luxury resort marina with 55 deep-water transient slips, pool, and restaurants.",
			reasoning:       "Great overnight stopover with resort amenities.",
			stayDetailsJSON: `{"is_charter_base":true,"has_transient_slips":true}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "BVI Soper's Hole / West End stopover without charter fleet stays overnight_stop",
			recName:         "Soper's Hole Marina (Freshwater Pond / West End)",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Protected harbor with mooring balls, transient dockage, and provisioning.",
			reasoning:       "Popular first or last night stopover with moorings.",
			stayDetailsJSON: `{"has_moorings":true,"has_transient_slips":true}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "Chesapeake Annapolis charter base promoted to start_end_base",
			recName:         "Annapolis City Marina / Eastport",
			recType:         "Marina",
			rawRole:         "start_end_base",
			desc:            "Homeport embarkation base for Cruise Annapolis and Waypoints Yacht Charters.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["Cruise Annapolis","Waypoints"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "Chesapeake St. Michaels resort marina stays overnight_stop",
			recName:         "St. Michaels Harbour Inn & Marina",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Historic Eastern Shore harbor with transient slips, fuel, and dining.",
			reasoning:       "Top overnight destination on the Miles River.",
			stayDetailsJSON: `{"has_transient_slips":true}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "Fuel station always classified as service_only",
			recName:         "Road Reef Fuel Dock",
			recType:         "Fuel Station",
			rawRole:         "overnight_stop",
			desc:            "High-speed diesel and water dock.",
			wantRole:        "service_only",
		},
		{
			name:            "South Florida Bahia Mar / Fort Lauderdale charter hub promoted to start_end_base",
			recName:         "Bahia Mar Yachting Center",
			recType:         "Marina",
			rawRole:         "overnight_stop",
			desc:            "Major Fort Lauderdale embarkation marina and charter base.",
			wantRole:        "start_end_base",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := model.VoyageRecommendation{
				Name:        tc.recName,
				Type:        tc.recType,
				VoyageRole:  tc.rawRole,
				Description: tc.desc,
				Reasoning:   tc.reasoning,
				StayDetails: model.RawJSON(tc.stayDetailsJSON),
			}
			normalizeRecommendationRole(&rec)
			if rec.VoyageRole != tc.wantRole {
				t.Errorf("normalizeRecommendationRole(%q) = %q, want %q", tc.recName, rec.VoyageRole, tc.wantRole)
			}
		})
	}
}

func TestMergeVerifiedPlacesIntoRecommendations(t *testing.T) {
	initial := []model.VoyageRecommendation{
		{
			Name:       "Bimini Blue Water Marina",
			Type:       "Marina",
			VoyageRole: "overnight_stop",
			Latitude:   25.7265,
			Longitude:  -79.2974,
		},
	}
	rawPlaces := json.RawMessage(`[
		{"name":"Bahia Mar Yachting Center","type":"Marina","address":"801 Seabreeze Blvd, Fort Lauderdale, FL","latitude":26.1132,"longitude":-80.1062,"website_uri":"https://bahiamar.com"},
		{"name":"Dinner Key Marina","type":"Marina","address":"3400 Pan American Dr, Miami, FL","latitude":25.7275,"longitude":-80.2341,"website_uri":"https://miami.gov"},
		{"name":"Bimini Blue Water Marina","type":"Marina","address":"Alice Town, Bimini","latitude":25.7265,"longitude":-79.2974},
		{"name":"Random Restaurant","type":"Restaurant","address":"Miami, FL","latitude":25.7617,"longitude":-80.1918}
	]`)

	merged := mergeVerifiedPlacesIntoRecommendations(initial, rawPlaces)
	if len(merged) != 3 {
		t.Fatalf("expected 3 recommendations after merging verified places, got %d", len(merged))
	}
	if merged[1].Name != "Bahia Mar Yachting Center" || merged[1].VoyageRole != "start_end_base" {
		t.Errorf("expected Bahia Mar Yachting Center as start_end_base, got name=%q role=%q", merged[1].Name, merged[1].VoyageRole)
	}
	if merged[2].Name != "Dinner Key Marina" || merged[2].VoyageRole != "start_end_base" {
		t.Errorf("expected Dinner Key Marina as start_end_base, got name=%q role=%q", merged[2].Name, merged[2].VoyageRole)
	}
}
