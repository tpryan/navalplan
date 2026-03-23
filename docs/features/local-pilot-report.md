This implementation plan outlines the steps to create a new **Local Pilot Report** feature. This report aggregates voyage details, the high-level voyage guide (summary, hazards, etc.), and all potential recommendations (marinas, anchorages, moorings) into a single view, specifically excluding stop-specific briefings (weather, tides).

### Implementation Plan: Local Pilot Report

#### 1. Model Updates
**File:** `app/backend/models/models.go`
* Define a new `PilotReport` struct to aggregate the data for the new endpoint.

```go
// PilotReport aggregates voyage info, the voyage guide, and all area recommendations.
type PilotReport struct {
	Voyage          *Voyage                `json:"voyage"`
	Guide           *VoyageGuide           `json:"guide,omitempty"`
	Recommendations []VoyageRecommendation `json:"recommendations"`
}
```

#### 2. Handler Implementation
**File:** `app/backend/server/handlers/voyages.go` (or a new file)
* Create a `GetPilotReport` handler.
* The handler must:
    1.  Validate the user session.
    2.  Retrieve the `Voyage` by ID and verify ownership.
    3.  Fetch the `VoyageGuide` (if it exists) to include the area summary and hazards.
    4.  Fetch all `VoyageRecommendation` entries associated with the voyage ID.
    5.  Return the aggregated `PilotReport` as JSON.

#### 3. Route Registration
**File:** `app/backend/server/routes.go`
* Register the new endpoint as a protected (Level 1) route.

```go
{http.MethodGet, "/api/v1/voyages/{id}/pilot_report", http.HandlerFunc(s.Handler.GetPilotReport), 1},
```

#### 4. (Optional) Pilot Summary Prompt
**File:** `services/researcher/prompts/pilot_summary_agent.md` (New)
* If a specific synthesized summary of the *recommendations* is desired (beyond the general `VoyageGuide` summary), create a new prompt for the Researcher service.
* This prompt would take the list of recommendations and generate a "Skipper's Overview" of the best options in the area.

#### 5. Logic for the "Local Pilot Stage"
* **Excluded Data:** Ensure the handler does not call `h.DB.ListVoyageBriefings` or `h.DB.GetBriefing`, as these are tied to specific `Stops` which have not yet been finalized.
* **Data Flow:**
    * **Voyage:** Provides dates and the center point of the search.
    * **VoyageGuide:** Provides the `Summary` and `Hazards` for the general region.
    * **Recommendations:** Provides the exhaustive list of "Hubs," "Anchorages," and "Moorings" found by the `navigator_agent`.

#### Summary of Objectives
* **Aggregation:** Combine `Voyage`, `VoyageGuide`, and `VoyageRecommendation` data.
* **Purpose:** Provide a research-focused view for the "Local Pilot Stage" before specific itinerary stops are set.
* **Simplicity:** Bypass the `Briefing` logic to keep the report focused on location discovery rather than day-to-day weather/tide planning.