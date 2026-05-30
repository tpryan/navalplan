Here is a comprehensive development plan designed for a coding agent like Claude to implement passage tracking, flexible date management, and spatial extrapolation in NavalPlan.

---

# Engineering Specification & Implementation Plan: Passage Tracking & Flexible Itineraries

## Objective

Refactor NavalPlan from its current assumption that a vessel must make landfall (anchorage, mooring, or dock) every night. The updated architecture must support **continuous offshore passages** spanning multiple days without stationary stops, dynamically compute estimated positions for passage days to fetch weather forecasts, allow dynamic timeline expansion, and support large regional search radiuses.

---

## 1. Database Architecture Updates (`app/db/schema.sql`)

Currently, the `briefing` table relies strictly on a unique `stop_id`. To introduce days without a physical landfall stop while preserving our data relationships, we will introduce a `stop_type` to the `stop` table. This allows us to track "Passage Tracking Points" as actual datastore positions to fetch and attach weather briefings.

### Migration Task Checklist:

* Create a new migration file: `code/app/db/migrations/000021_add_passage_tracking.up.sql`.
* Add an explicit column for `stop_type` to differentiate stationary nights from open-water transits.
* Ensure constraints allow flexible handling of location names when tracking open sea positions.

```sql
-- code/app/db/migrations/000021_add_passage_tracking.up.sql

ALTER TABLE stop ADD COLUMN stop_type VARCHAR(30) DEFAULT 'landfall' NOT NULL;
-- Supported types: 'landfall' (anchorage/marina/dock) or 'passage_point' (at-sea tracking position)

-- Modify location name requirement for passage points
ALTER TABLE stop ALTER COLUMN location_name DROP NOT NULL;

```

---

## 2. Go Backend Model & Datastore Extensions (`app/backend/models/models.go`)

Update the Go structs to accommodate tracking points and support larger operational dimensions.

### Model Updates:

* Update `Stop` in `code/app/backend/models/models.go` to include the type field:
```go
type Stop struct {
    ID               int64     `json:"id" db:"id"`
    VoyageID         int64     `json:"voyage_id" db:"voyage_id"`
    TargetDate       time.Time `json:"target_date" db:"target_date"`
    StopType         string    `json:"stop_type" db:"stop_type"` // "landfall" or "passage_point"
    LocationName     string    `json:"location_name" db:"location_name"`
    PreciseLocation  string    `json:"precise_location" db:"precise_location"`
    Latitude         float64   `json:"latitude" db:"latitude"`
    Longitude        float64   `json:"longitude" db:"longitude"`
    SearchRadius     int       `json:"search_radius" db:"search_radius"`
    SearchRadiusUnit string    `json:"search_radius_unit" db:"search_radius_unit"`
    Notes            string    `json:"notes" db:"notes"`
    CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

```



---

## 3. Core Engine: Coordinate Extrapolation for Passage Days

When a voyage has gaps between landfall stops (e.g., Stop A on Day 1, Stop B on Day 4), the backend must automatically compute intermediary `passage_point` coordinates for Day 2 and Day 3 using linear interpolation. This provides the standard `(lat, lon)` footprint required by the **Researcher Agent** loop to fetch weather briefings.

### Implementation Blueprint for Claude:

Create a new utility or service method `service.InterpolatePassagePoints(ctx, voyageID)` that executes the following lifecycle whenever a voyage's timeline or landfall points change:

1. **Query Existing Landfalls**: Fetch all stops for the voyage ordered by `target_date` where `stop_type = 'landfall'`.
2. **Detect Chronological Gaps**: Iterate through the ordered landfalls. If $Date_{n+1} - Date_n > 1\text{ day}$, a passage gap is present.
3. **Calculate Intermediary Vector Steps**:
Let $D$ be the total days between landfalls. For any missing day index $k$ (where $0 < k < D$):

$$\text{Lat}_k = \text{Lat}_n + \left(\frac{k}{D}\right) \times (\text{Lat}_{n+1} - \text{Lat}_n)$$


$$\text{Lon}_k = \text{Lon}_n + \left(\frac{k}{D}\right) \times (\text{Lon}_{n+1} - \text{Lon}_n)$$


4. **Upsert Passage Records**: Create or update matching `passage_point` records in the `stop` table for those missing dates using the extrapolated coordinates. Mark `location_name` as `"Passage Leg (Extrapolated)"`.
5. **Chain Research Execution**: Automatically trigger the async research loop (`/api/v1/stops/{id}/research`) for these generated points so that offshore weather data is seamlessly pulled.

---

## 4. Researcher Agent Modification

The **Researcher Agent** utilizes Gemini to search for local facility definitions using Google Search tools. For an active passage at sea, facility lookups are unnecessary.

### Updates to Agent Invocation:

* Update the routing layer in the backend context when invoking the Researcher Agent. If `stop_type == "passage_point"`, explicitly configure the payload or prompt instructions passed to the LLM backend to bypass maritime facility/dockage lookup constraints, keeping the computational scope restricted strictly to wind, wave height, swell period, and safety alert metrics.

---

## 5. API Extensions

To accommodate changes to voyage timelines from within the workspace view, add the following handlers:

* **`POST /api/v1/voyages/{id}/extend`**: Accepts an updated `end_date` or an `additional_days` count parameter. Updates the `voyage` database row and triggers the coordinate extrapolation logic immediately.
* **`PATCH /api/v1/voyages/{id}/config`**: Allows increasing the `search_radius` up to `300` to reflect wide-region ocean transits.

---

## 6. Frontend Workspace Evolution (Vanilla JS & Semantic HTML)

Adhering strictly to Vanilla JS (ES Modules) and semantic layouts, the user interface must clear out rigid date controls.

### Implementation Tasks for UI Flow:

* **Interactive Timeline Actions**: In the stop planning layout, include an explicitly semantic action widget:
```html
<button id="btn-extend-timeline" class="btn btn-secondary" type="button">Add Day to Passage</button>

```


* **Timeline Presentation**: Update the itinerary renderer loop. If a row returns `stop_type === 'passage_point'`, render it as a continuous dotted navigation path rather than displaying anchor or harbor facility cards. Show a distinctive `Compass` or `Transit` indicator to visually signal an active night passage at sea.
* **Map Overlay**: Use the Google Maps API layer to render passage points along a unified polyline connecting major landfalls, indicating that weather details are derived from spatial extrapolation.

---

## Execution Workflow for Claude:

1. Run SQL migration script to update schema constraints.
2. Update the `models.Stop` struct and the corresponding SQL queries within `code/app/backend/datastore/stops.go`.
3. Build out the linear interpolation service engine for handling date windows between known points.
4. Integrate passage point support into the background job worker that triggers weather updates.
5. Enhance the frontend client components using vanilla JS module architecture to interact seamlessly with the dynamic extensions.