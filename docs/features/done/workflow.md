This implementation plan outlines the steps to decouple date selection from the initial voyage creation, enabling a "Discovery First" workflow where you can perform local pilot research before committing to a specific timeframe.

### Implementation Plan: Discovery-First Workflow

#### 1. Database Schema Migration
**File:** `code/app/db/migrations/000011_optional_voyage_dates.up.sql` (New)
To support a voyage without fixed dates, the `start_date` and `end_date` columns in the `voyage` table must allow `NULL` values.

```sql
ALTER TABLE voyage ALTER COLUMN start_date DROP NOT NULL;
ALTER TABLE voyage ALTER COLUMN end_date DROP NOT NULL;
```

#### 2. Model Updates
**File:** `code/app/backend/models/models.go`
Update the `Voyage` struct to use pointers for dates, allowing them to be `nil` in Go and `NULL` in the database.

```go
type Voyage struct {
	ID               int64      `json:"id" db:"id"`
	// ... other fields
	StartDate        *time.Time `json:"start_date" db:"start_date"` // Changed to pointer
	EndDate          *time.Time `json:"end_date" db:"end_date"`     // Changed to pointer
	// ... other fields
}
```

#### 3. Backend Handler Adjustments
**File:** `code/app/backend/server/handlers/voyages.go`
* **CreateVoyage:** Update the validation logic to no longer require `start_date` and `end_date` during the initial POST request.
* **UpdateVoyage:** Ensure this handler can correctly process the transition from a "Discovery" state (no dates) to a "Planning" state (dates added).

#### 4. Frontend Workflow Redesign
**File:** `code/app/frontend/js/main.js` (and relevant UI components)
1.  **Creation Phase:** Modify the "New Voyage" modal to make the date inputs optional. The user only needs to provide a **Location Name** and **Search Radius** to begin.
2.  **Voyage View (Discovery Mode):** When a voyage has no dates, the UI should prioritize the "Local Pilot Research" action.
    * Display the **Navigator Agent** button prominently to find marinas and anchorages.
    * Hide or disable "Stop Research" (weather/tides) since those require specific dates.
3.  **Transition to Planning:** Add a "Set Voyage Dates" button in the voyage header. Clicking this opens a date picker that, when saved, triggers a `PUT` request to update the voyage, enabling the full itinerary planning features.

#### 5. Local Pilot Research Integration
**File:** `code/app/backend/server/handlers/recommendation.go`
Verify that `performRecommendationGeneration` correctly handles `Voyage` objects with `nil` dates. Since the `navigator_agent` primarily relies on `Latitude`, `Longitude`, and `SearchRadius`, it can function effectively in this "Discovery" state without temporal constraints.

### Summary of Workflow Change

* **Step 1:** Create Voyage (Location + Radius only).
* **Step 2 (Discovery):** Run "Local Pilot Research" to see all nearby facilities.
* **Step 3 (Commitment):** Add Start/End dates when the timeframe is decided.
* **Step 4 (Planning):** Add specific `Stops` and perform detailed briefing research (weather/tides).