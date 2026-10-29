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
			name:            "Marina with verified charter companies is start_end_base",
			recName:         "Wickhams Cay II Marina",
			recType:         "Marina",
			rawRole:         "start_end_base",
			desc:            "Primary bareboat charter fleet embarkation marina.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["The Moorings","Sunsail"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "Overnight marina with verified charter companies promoted to start_end_base",
			recName:         "Hodges Creek Marina",
			recType:         "Marina",
			rawRole:         "overnight_stop",
			desc:            "Major charter base for bareboat fleets.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["Dream Yacht Charter","MarineMax"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "Resort marina without charter fleet demoted to overnight_stop",
			recName:         "Scrub Island Resort, Spa & Marina",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Luxury resort marina with 55 deep-water transient slips, pool, and restaurants.",
			reasoning:       "Great overnight stopover with resort amenities.",
			stayDetailsJSON: `{"is_charter_base":true,"has_transient_slips":true}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "Stopover harbor without charter fleet or charter base evidence demoted to overnight_stop",
			recName:         "Soper's Hole Marina (Freshwater Pond / West End)",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Protected harbor with mooring balls, transient dockage, and provisioning.",
			reasoning:       "Popular first or last night stopover with moorings.",
			stayDetailsJSON: `{"has_moorings":true,"has_transient_slips":true}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "Mainland charter base with charter companies promoted to start_end_base",
			recName:         "Annapolis City Marina / Eastport",
			recType:         "Marina",
			rawRole:         "start_end_base",
			desc:            "Homeport embarkation base for bareboat yacht charters.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["Cruise Annapolis","Waypoints"]}`,
			wantRole:        "start_end_base",
		},
		{
			name:            "Harbour inn marina stays overnight_stop",
			recName:         "St. Michaels Harbour Inn & Marina",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "Historic harbor with transient slips, fuel, and dining.",
			reasoning:       "Top overnight destination on the river.",
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
			name:            "Marina with explicit embarkation base description promoted to start_end_base",
			recName:         "Bahia Mar Yachting Center",
			recType:         "Marina",
			rawRole:         "overnight_stop",
			desc:            "Major embarkation marina and charter base.",
			wantRole:        "start_end_base",
		},
		{
			name:            "Offshore salt pond stopover mentioning nearby charter hub in reasoning stays overnight_stop and clears charter fields",
			recName:         "Block Island Great Salt Pond (New Harbor)",
			recType:         "Hub",
			rawRole:         "start_end_base",
			desc:            "World-famous island cruising destination featuring massive mooring fields and vibrant onshore life.",
			reasoning:       "An absolute must-visit destination within comfortable cruising range of Newport and Buzzards Bay.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":["Bareboat Sailing Charters"]}`,
			wantRole:        "overnight_stop",
		},
		{
			name:            "Private Yacht Club stays overnight_stop",
			recName:         "New York Yacht Club Harbour Court",
			recType:         "Yacht Club",
			rawRole:         "start_end_base",
			desc:            "Renowned yacht club station overlooking the harbor.",
			stayDetailsJSON: `{"is_charter_base":true,"charter_companies":[]}`,
			wantRole:        "overnight_stop",
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
			var sd map[string]any
			_ = json.Unmarshal(rec.StayDetails, &sd)
			if tc.wantRole == "start_end_base" {
				if isBase, _ := sd["is_charter_base"].(bool); !isBase {
					t.Errorf("normalizeRecommendationRole(%q) expected stay_details.is_charter_base=true, got %v", tc.recName, sd)
				}
			} else if sd != nil {
				if isBase, _ := sd["is_charter_base"].(bool); isBase {
					t.Errorf("normalizeRecommendationRole(%q) expected stay_details.is_charter_base=false on %s, got %v", tc.recName, tc.wantRole, sd)
				}
			}
		})
	}
}

func TestEnrichRecommendationsFromGuide_AttachesCharterCompanies(t *testing.T) {
	recs := []model.VoyageRecommendation{
		{
			Name:        "South Wharf Yacht Yard & Marina (Padanaram)",
			Type:        "Marina",
			VoyageRole:  "overnight_stop",
			Latitude:    41.5845,
			Longitude:   -70.9462,
			Description: "Full-service marina on the western shore of the bay.",
			StayDetails: model.RawJSON(`{"is_charter_base":false,"charter_companies":[]}`),
		},
		{
			Name:        "Newport Harbor & Downtown Waterfront Hub",
			Type:        "Hub",
			VoyageRole:  "overnight_stop",
			Latitude:    41.4900,
			Longitude:   -71.3128,
			Description: "Historic harbor hub.",
			StayDetails: model.RawJSON(`{"is_charter_base":false,"charter_companies":[]}`),
		},
		{
			Name:        "Great Salt Pond (New Harbor, Block Island)",
			Type:        "Hub",
			VoyageRole:  "overnight_stop",
			Latitude:    41.1850,
			Longitude:   -71.5775,
			Description: "Landlocked island cruising stopover.",
			Reasoning:   "Within easy cruising range of Newport and Padanaram.",
			StayDetails: model.RawJSON(`{"is_charter_base":false,"charter_companies":[]}`),
		},
	}
	guide := &model.VoyageGuide{
		CharterInfo: model.RawJSON(`{
			"is_charter_destination": true,
			"companies": [
				{"name": "Nauti Jane's Yacht Charters", "base_marina": "South Wharf Yacht Yard", "location": "Padanaram, MA", "url": "https://www.nautijanesboatrentals.com/"},
				{"name": "Bareboat Sailing Charters", "base_marina": "Goat Island / Newport Harbor", "location": "Newport, RI", "url": "https://www.bareboatsailing.com/"}
			]
		}`),
	}
	enrichRecommendationsFromGuide(recs, guide)
	for i := 0; i < 2; i++ {
		r := recs[i]
		if r.VoyageRole != "start_end_base" {
			t.Errorf("recs[%d] (%s) VoyageRole = %q, want start_end_base", i, r.Name, r.VoyageRole)
		}
		var sd struct {
			IsCharterBase    bool     `json:"is_charter_base"`
			CharterCompanies []string `json:"charter_companies"`
		}
		_ = json.Unmarshal(r.StayDetails, &sd)
		if !sd.IsCharterBase || len(sd.CharterCompanies) == 0 {
			t.Errorf("recs[%d] (%s) expected is_charter_base=true and non-empty charter_companies, got %+v", i, r.Name, sd)
		}
	}
	if recs[2].VoyageRole != "overnight_stop" {
		t.Errorf("expected island stopover recs[2] (%s) to remain overnight_stop, got %q", recs[2].Name, recs[2].VoyageRole)
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
		{"name":"Bahia Mar Yacht Charters","type":"Marina","address":"801 Seabreeze Blvd, Fort Lauderdale, FL","latitude":26.1132,"longitude":-80.1062,"website_uri":"https://bahiamar.com","types":["marina","yacht_charter"]},
		{"name":"Dinner Key Marina","type":"Marina","address":"3400 Pan American Dr, Miami, FL","latitude":25.7275,"longitude":-80.2341,"website_uri":"https://miami.gov"},
		{"name":"Newport Yacht Charters","type":"Marina","address":"Bowen's Wharf, Newport, RI","latitude":41.4885,"longitude":-71.3150,"website_uri":"https://newportyachtcharters.com","types":["point_of_interest","yacht_charter"]},
		{"name":"Bimini Blue Water Marina","type":"Marina","address":"Alice Town, Bimini","latitude":25.7265,"longitude":-79.2974},
		{"name":"Random Restaurant","type":"Restaurant","address":"Miami, FL","latitude":25.7617,"longitude":-80.1918}
	]`)

	merged := mergeVerifiedPlacesIntoRecommendations(initial, rawPlaces)
	if len(merged) != 4 {
		t.Fatalf("expected 4 recommendations after merging verified places, got %d", len(merged))
	}
	if merged[1].Name != "Bahia Mar Yacht Charters" || merged[1].VoyageRole != "start_end_base" {
		t.Errorf("expected Bahia Mar Yacht Charters as start_end_base, got name=%q role=%q", merged[1].Name, merged[1].VoyageRole)
	}
	if merged[2].Name != "Dinner Key Marina" || merged[2].VoyageRole != "overnight_stop" {
		t.Errorf("expected plain marina Dinner Key Marina as overnight_stop, got name=%q role=%q", merged[2].Name, merged[2].VoyageRole)
	}
	if merged[3].Name != "Newport Yacht Charters" || merged[3].VoyageRole != "start_end_base" {
		t.Errorf("expected Newport Yacht Charters as start_end_base, got name=%q role=%q", merged[3].Name, merged[3].VoyageRole)
	}
}
