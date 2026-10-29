package main

import (
	"strings"
	"testing"
	"time"
)

func TestExtractJSONPayload(t *testing.T) {
	fenced := "Here is the output:\n```json\n{\"location_name\": \"Newport\"}\n```\nDone."
	if got := extractJSONPayload(fenced); got != `{"location_name": "Newport"}` {
		t.Errorf("extractJSONPayload(fenced) = %q", got)
	}

	arr := "Result:\n[{\"severity\": \"danger\"}]\n"
	if got := extractJSONPayload(arr); got != `[{"severity": "danger"}]` {
		t.Errorf("extractJSONPayload(arr) = %q", got)
	}
}

func TestScoreHarbourmasterOutput(t *testing.T) {
	validJSON := `{
		"location_name": "Cuttyhunk, MA",
		"weather_summary": {"summary": "SW 15 kt", "wind_speed_kt": 15},
		"sun_phase": {"sunrise": "06:58 AM", "sunset": "06:02 PM"},
		"tides": {"station_name": "Cuttyhunk", "events": [{"time": "2026-10-15 08:30", "type": "Low", "height_ft": 0.4}]},
		"pilot_notes": {
			"overview": "Protected pond entered via dredged channel.",
			"approach_and_channels": "Controlling depth 6 ft in entrance cut.",
			"departure_and_arrival_timing": {
				"recommended_departure_window": "09:30 AM - 10:30 AM",
				"departure_tide_constraint": "Rising tide needed over 6 ft entrance sill.",
				"arrival_outlook": "Arrive Block Island Great Salt Pond by 04:30 PM, 90 min before sunset.",
				"night_entry_and_aids_warning": "Avoid night entry due to unlit inner channel buoys."
			}
		},
		"facilities": [
			{"name": "Cuttyhunk Marina", "type": "Marina", "latitude": 41.424, "longitude": -70.931},
			{"name": "Cuttyhunk Moorings", "type": "Mooring", "latitude": 41.425, "longitude": -70.932},
			{"name": "Outer Harbor Anchorage", "type": "Anchorage", "latitude": 41.428, "longitude": -70.928}
		]
	}`
	schema, domain, failures := scoreHarbourmasterOutput(validJSON, true)
	if schema < 100 || domain < 100 || len(failures) > 0 {
		t.Errorf("scoreHarbourmasterOutput(valid) = (%.1f, %.1f, %v), want (100, 100, nil)", schema, domain, failures)
	}

	_, domainNoTiming, failuresNoTiming := scoreHarbourmasterOutput(`{
		"location_name": "Cuttyhunk",
		"weather_summary": {"summary": "OK"},
		"sun_phase": {"sunrise": "06:58 AM", "sunset": "06:02 PM"},
		"tides": {"events": []},
		"pilot_notes": {"overview": "Overview", "approach_and_channels": "Channel"},
		"facilities": [
			{"name": "A", "type": "Marina", "latitude": 41.4, "longitude": -70.9},
			{"name": "B", "type": "Mooring", "latitude": 41.4, "longitude": -70.9},
			{"name": "C", "type": "Anchorage", "latitude": 41.4, "longitude": -70.9}
		]
	}`, true)
	if domainNoTiming >= 100 || len(failuresNoTiming) == 0 {
		t.Errorf("expected failure when departure_and_arrival_timing is missing, got domain=%.1f failures=%v", domainNoTiming, failuresNoTiming)
	}
}

func TestScoreSpecialistOutput(t *testing.T) {
	validBVI := `{
		"recommendations": [
			{
				"name": "Wickhams Cay II / The Moorings Base (Road Town)",
				"type": "Marina",
				"voyage_role": "start_end_base",
				"stay_details": {"is_charter_base": true, "charter_companies": ["The Moorings", "Sunsail"]},
				"description": "Primary Tortola charter embarkation base."
			},
			{
				"name": "Norman Island - The Bight",
				"type": "Mooring",
				"voyage_role": "overnight_stop",
				"stay_details": {"is_charter_base": false},
				"description": "Protected overnight mooring harbor."
			},
			{
				"name": "Cooper Island Beach Club Moorings",
				"type": "Mooring",
				"voyage_role": "overnight_stop",
				"stay_details": {"is_charter_base": false},
				"description": "Overnight mooring field."
			}
		]
	}`
	schema, domain, failures := scoreSpecialistOutput(
		validBVI,
		[]string{"Road Town", "Wickham", "Nanny Cay"},
		[]string{"Norman", "Cooper", "Virgin Gorda"},
	)
	if schema < 100 || domain < 100 || len(failures) > 0 {
		t.Errorf("scoreSpecialistOutput(validBVI) = (%.1f, %.1f, %v), want (100, 100, nil)", schema, domain, failures)
	}

	falsePositive := `{
		"recommendations": [
			{
				"name": "Block Island Great Salt Pond (New Harbor)",
				"type": "Hub",
				"voyage_role": "start_end_base",
				"stay_details": {"is_charter_base": true, "charter_companies": ["Bareboat Sailing Charters"]},
				"description": "An absolute must-visit destination within comfortable cruising range of Newport and Buzzards Bay."
			}
		]
	}`
	_, domainFP, failuresFP := scoreSpecialistOutput(
		falsePositive,
		[]string{"Newport", "Padanaram"},
		[]string{"Block Island", "Great Salt Pond", "Cuttyhunk"},
	)
	if domainFP >= 100 || len(failuresFP) == 0 {
		t.Errorf("expected false-positive start_end_base failure on Block Island (even when description mentions Newport), got domain=%.1f failures=%v", domainFP, failuresFP)
	}
}

func TestScoreLookoutOutput(t *testing.T) {
	dangerJSON := `[{"severity":"danger","category":"weather","message":"Gale force 36-48 kt winds and 14 ft seas.","action":"Remain in port.","icon":"storm"}]`
	if s, d, f := scoreLookoutOutput(dangerJSON, "heavy_weather"); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreLookoutOutput(heavy_weather) = (%.1f, %.1f, %v)", s, d, f)
	}
	if _, d, f := scoreLookoutOutput(`[]`, "calm"); d < 100 || len(f) > 0 {
		t.Errorf("scoreLookoutOutput(calm empty) = (%.1f, %v)", d, f)
	}
	if _, d, f := scoreLookoutOutput(dangerJSON, "calm"); d != 0 || len(f) == 0 {
		t.Errorf("expected calm mode to fail when danger weather alert is emitted")
	}
	cascadeJSON := `[{"severity":"warning","category":"navigation","message":"Low tide at 08:30 AM over the 6 ft sill delays departure to 10:30 AM, and 17 kt headwinds on 115 deg push arrival past sunset at Menemsha with unlit inner buoys.","action":"Depart before low water or divert before dark.","icon":"wb_twilight"}]`
	if s, d, f := scoreLookoutOutput(cascadeJSON, "cascade"); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreLookoutOutput(cascade) = (%.1f, %.1f, %v)", s, d, f)
	}
}

func TestScorePilotAndCommodoreAndDebriefAndSubAgents(t *testing.T) {
	pilotJSON := `{
		"summary": "San Juan Islands to Sidney BC cross-border cruising ground.",
		"sailing_season": {"primary_season_months": ["June", "July"]},
		"hazards": [{"title": "Haro Strait Tidal Rips"}],
		"security_safety": {"summary": "Safe coastal waters; CBSA and US CBP customs clearance required when crossing the US/Canada border to Sidney."},
		"hubs": [{"name": "Friday Harbor"}],
		"charter_info": {"is_charter_destination": true, "companies": [{"name": "San Juan Sailing", "base_marina": "Squalicum Harbor", "location": "Bellingham, WA", "url": "https://www.sanjuansailing.com"}]},
		"airports": [{"name": "Friday Harbor Airport"}],
		"country_info": {"name": "United States / Canada"},
		"currencies": [{"code": "USD"}],
		"points_of_interest": [{"name": "Roche Harbor"}]
	}`
	if s, d, f := scorePilotOutput(pilotJSON, true); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scorePilotOutput = (%.1f, %.1f, %v)", s, d, f)
	}
	emptyCharterPilotJSON := strings.Replace(pilotJSON, `[{"name": "San Juan Sailing", "base_marina": "Squalicum Harbor", "location": "Bellingham, WA", "url": "https://www.sanjuansailing.com"}]`, `[]`, 1)
	if _, dEmpty, fEmpty := scorePilotOutput(emptyCharterPilotJSON, true); dEmpty >= 100 || len(fEmpty) == 0 {
		t.Errorf("expected scorePilotOutput to fail when is_charter_destination=true has empty companies, got domain=%.1f failures=%v", dEmpty, fEmpty)
	}

	commodoreJSON := `[
		{"name":"British Virgin Islands","type":"Island Group","tier":"Standard","suitability_score":95,"summary":"Peak trade winds.","deep_cut_reasoning":"Azores High extension drives steady 15-20 kt easterly trade winds across the Sir Francis Drake Channel with sheltered lee fetch and weak tidal currents during the winter peak window.","geometry":{"type":"Polygon","coordinates":[[[-65.0,18.0],[-64.4,18.1],[-64.0,18.4],[-64.2,18.9],[-64.8,18.8],[-65.0,18.0]]]}},
		{"name":"Southern New England (Newport, Buzzards Bay & Martha's Vineyard)","type":"Island Group","tier":"Classic Locale","suitability_score":95,"summary":"America's historic yachting capital.","deep_cut_reasoning":"Bermuda High summer extension generates reliable 15-20 kt afternoon SW thermal sea breezes across Narragansett Bay and Vineyard Sound, with strong tidal gates at Woods Hole and The Race.","geometry":{"type":"Polygon","coordinates":[[[-71.65,41.10],[-70.85,41.05],[-69.90,41.20],[-69.95,41.75],[-70.75,41.75],[-71.50,41.65],[-71.65,41.10]]]}},
		{"name":"Raja Ampat & Dampier Strait","type":"Island Group","tier":"Hidden Gem","is_hidden_gem":true,"suitability_score":93,"summary":"Equatorial monsoon transition window.","deep_cut_reasoning":"Shoulder-season ITCZ migration brings 10-16 kt breezes outside the cyclone belt, while strong 3-5 kt tidal currents through Dampier Strait demand careful slack-water pass timing.","geometry":{"type":"Polygon","coordinates":[[[129.8,0.5],[130.8,0.6],[131.4,1.2],[130.9,1.8],[129.9,1.6],[129.8,0.5]]]}},
		{"name":"Lofoten & Helgeland Archipelago","type":"Coastal","tier":"Hidden Gem","is_hidden_gem":true,"suitability_score":91,"summary":"Arctic thermal window.","deep_cut_reasoning":"High-latitude summer solstice ridge produces 12-18 kt coastal sea breezes inside the Vestfjorden island lee, though Moskstraumen tidal races require strict tide-gate navigation.","geometry":{"type":"Polygon","coordinates":[[[12.5,67.2],[14.8,67.4],[16.0,68.5],[14.5,69.0],[12.8,68.2],[12.5,67.2]]]}},
		{"name":"Abaco Sea & Exuma Sound","type":"Island Group","tier":"Regional Favorite","suitability_score":90,"summary":"Winter cold-front and trade-wind arbitrage.","deep_cut_reasoning":"Subtropical ridge and winter window between frontal passages deliver 14-20 kt easterly trade winds across the sheltered Abaco Sound lee with strong tidal cuts at Whale Cay.","geometry":{"type":"Polygon","coordinates":[[[-77.5,25.8],[-76.8,25.9],[-76.4,26.5],[-76.9,27.1],[-77.6,26.9],[-77.5,25.8]]]}},
		{"name":"Cape Verde Archipelago","type":"Ocean Crossing","tier":"Challenging","suitability_score":88,"summary":"Strong NE trades and winter island venturi.","deep_cut_reasoning":"Subtropical high pressure during the winter peak drives 20-28 kt NE trade winds and 2-3m Atlantic swell, with severe venturi acceleration and tidal rips in the channels between Sao Vicente and Santo Antao.","geometry":{"type":"Polygon","coordinates":[[[-25.5,14.6],[-23.2,14.7],[-22.6,16.2],[-24.0,17.3],[-25.4,17.0],[-25.5,14.6]]]}},
		{"name":"Hauraki Gulf & Colville Channel","type":"Coastal","tier":"Regional Favorite","suitability_score":89,"summary":"Austral summer sea breeze.","deep_cut_reasoning":"South Pacific high-pressure ridge establishes reliable 14-20 kt afternoon SW thermal breezes and sheltered deep-water fjord anchorages with strong tidal currents through Colville Channel.","geometry":{"type":"Polygon","coordinates":[[[174.5,-37.0],[175.6,-36.9],[175.9,-36.0],[175.1,-35.5],[174.4,-35.9],[174.5,-37.0]]]}}
	]`
	if s, d, f := scoreCommodoreOutput(commodoreJSON); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreCommodoreOutput(deep) = (%.1f, %.1f, %v), want (100, 100, nil)", s, d, f)
	}
	if sample := buildOutputSample("commodore", commodoreJSON); !strings.Contains(sample, "Novel") || !strings.Contains(sample, "Custom Polygons") {
		t.Errorf("buildOutputSample(commodore) missing Novel/Custom Polygons summary: %s", sample)
	}

	debriefClean := `{
		"summary": "Upwind beat against a foul flood tide forced wide tacks south of the rhumb line.",
		"conclusions": "Course deviation and extra distance stemmed from pinching into a chop near Middle Ground.",
		"tacking_efficiency": "Wide tacking angles in adverse current degraded windward VMG.",
		"weather_impact": "Southwest chop combined with opposing east-running flood slowed boat speed.",
		"observations": ["Time departure for slack or fair west-running ebb in Vineyard Sound."]
	}`
	if s, d, f := scoreDebriefOutput(debriefClean, []string{"28.40", "24.10", "17.8%"}); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreDebriefOutput(clean) = (%.1f, %.1f, %v)", s, d, f)
	}
	debriefRepeated := `{
		"summary": "Recorded 28.40 NM vs 24.10 NM (+17.8% variance).",
		"conclusions": "Longer track.",
		"tacking_efficiency": "Multiple tacks.",
		"weather_impact": "Headwind.",
		"observations": ["Wait for fair tide."]
	}`
	if _, d, f := scoreDebriefOutput(debriefRepeated, []string{"28.40", "24.10", "17.8%"}); d >= 100 || len(f) == 0 {
		t.Errorf("expected penalty for repeating forbidden literals, got domain=%.1f failures=%v", d, f)
	}

	facJSON := `{
		"facilities": [
			{"name": "Annapolis Yacht Basin", "type": "Marina"},
			{"name": "Port Annapolis Marina", "type": "Marina"},
			{"name": "Back Creek Moorings", "type": "Mooring"},
			{"name": "Severn River Anchorage", "type": "Anchorage"},
			{"name": "Eastport Yacht Club", "type": "Yacht Club"}
		]
	}`
	calls := []ToolInvocation{{Name: "FindPlacesNearby", Args: map[string]any{"radius": 37040.0}}}
	if s, d, f := scoreFacilityDiscoveryOutput(facJSON, calls, true); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreFacilityDiscoveryOutput(area) = (%.1f, %.1f, %v)", s, d, f)
	}

	hydroJSON := `{
		"data_available": true,
		"overview": "Cape Cod Canal is a sea-level waterway with strong reversing tidal currents.",
		"approach_and_channels": "Controlling depth 32 ft; Bourne and Sagamore bridges have 135 ft vertical clearance.",
		"hazards": ["Strong tidal currents up to 4.5 knots"],
		"citations": ["NOAA Coast Pilot 2, Chapter 6"]
	}`
	if s, d, f := scoreHydrographicPilotOutput(hydroJSON); s < 100 || d < 100 || len(f) > 0 {
		t.Errorf("scoreHydrographicPilotOutput = (%.1f, %.1f, %v)", s, d, f)
	}
}

func TestFilterEvalCasesAndFormatScorecard(t *testing.T) {
	all := allEvalCases()
	if len(all) != 23 {
		t.Fatalf("len(allEvalCases()) = %d, want 23", len(all))
	}

	hmOnly := filterEvalCases(all, "harbourmaster", "")
	if len(hmOnly) != 3 {
		t.Errorf("filterEvalCases(harbourmaster) = %d, want 3", len(hmOnly))
	}

	singleCase := filterEvalCases(all, "", "calm_conditions_no_alerts")
	if len(singleCase) != 1 || singleCase[0].AgentName != "lookout" {
		t.Errorf("filterEvalCases(calm_conditions_no_alerts) = %v", singleCase)
	}

	summaries := []ModelEvalSummary{
		summarizeModelResults("gemini-3.5-flash-lite", []CaseEvalResult{
			{
				AgentName:    "lookout",
				CaseID:       "calm_conditions_no_alerts",
				Passed:       true,
				SchemaScore:  100,
				ToolScore:    100,
				DomainScore:  100,
				OverallScore: 100,
				Duration:     2 * time.Second,
			},
		}, 2*time.Second),
		summarizeModelResults("gemini-2.5-flash", []CaseEvalResult{
			{
				AgentName:    "lookout",
				CaseID:       "calm_conditions_no_alerts",
				Passed:       true,
				SchemaScore:  100,
				ToolScore:    100,
				DomainScore:  100,
				OverallScore: 100,
				Duration:     4 * time.Second,
			},
		}, 4*time.Second),
		summarizeModelResults("broken-404-model", []CaseEvalResult{
			{
				AgentName:      "lookout",
				CaseID:         "calm_conditions_no_alerts",
				Passed:         false,
				SchemaScore:    0,
				ToolScore:      0,
				DomainScore:    0,
				OverallScore:   0,
				Duration:       500 * time.Millisecond,
				FailureReasons: []string{"unexpected warning alert"},
			},
		}, 500*time.Millisecond),
	}

	report := formatComparisonScorecard(summaries)
	for _, section := range []string{
		"NAVALPLAN MULTI-MODEL EVALUATION SCORECARD",
		"## Model Evaluation Comparison Matrix",
		"## Per-Agent Breakdown by Model",
		"## Per-Case Side-by-Side Breakdown",
		"## Per-Case Failure & Diagnostic Diffs",
		"Fastest (x)",
		"`gemini-3.5-flash-lite` (2.00x)",
		"#### broken-404-model (0/1 passed)",
	} {
		if !strings.Contains(report, section) {
			t.Errorf("formatComparisonScorecard missing section %q in:\n%s", section, report)
		}
	}
}
