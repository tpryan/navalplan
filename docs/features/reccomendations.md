This design plan outlines the addition of the **Voyage Navigator** feature, which utilizes a new AI agent to analyze a voyage's target area and recommend specific locations for anchoring, mooring, or docking before the skipper manually enters stops.

### 1. Backend Data Model Updates

To support these recommendations, a new model and database table are required to store the agent's findings independently of the actual voyage stops.

* **New Model (`app/backend/models/models.go`):**
    ```go
    type Recommendation struct {
        ID          int64   `json:"id" db:"id"`
        VoyageID    int64   `json:"voyage_id" db:"voyage_id"`
        Name        string  `json:"name" db:"name"`
        Type        string  `json:"type" db:"type"` // "Anchorage", "Mooring", "Marina"
        Latitude    float64 `json:"latitude" db:"latitude"`
        Longitude   float64 `json:"longitude" db:"longitude"`
        Description string  `json:"description" db:"description"`
        Reasoning   string  `json:"reasoning" db:"reasoning"` // Why the agent chose this
        CreatedAt   time.Time `json:"created_at" db:"created_at"`
    }
    ```
* **Database Schema:** A new `voyage_recommendations` table with a foreign key to `voyages(id)` and an index on `voyage_id` for fast retrieval.

### 2. The "Local Pilot" Agent (Researcher Service)

A new agent, the **Local Pilot**, will be added to the Researcher service using the Agent Developer Kit (ADK).

* **Agent Configuration (`services/researcher/main.go`):**
    * **Name:** `navigator_agent`.
    * **Tools:** `Google Search` (to find reviews and current harbor conditions) and the existing `Places API` tool (to gather coordinates and facility types).
    * **Instruction (`navigator_agent.md`):** Similar to the "Commodore" agent, this prompt will instruct the model to identify the top 3-5 locations in each category (Anchor, Moor, Dock) within the voyage's search radius. It must analyze the density of facilities to ensure recommendations are diverse and high-quality.
* **Output Format:** The agent will return a structured JSON array of recommendation objects including coordinates and a "reasoning" string explaining the location's appeal.

### 3. API Routes and Handlers

New endpoints are needed to trigger the analysis and fetch the results.

* **Trigger Analysis:** `POST /api/v1/voyages/{id}/recommendations/generate`. This handler will fetch the voyage coordinates and radius, delegate to the `navigator_agent`, and save the results to the new recommendations table.
* **Fetch Recommendations:** `GET /api/v1/voyages/{id}/recommendations`. Returns the list of generated recommendations for the frontend to display.

### 4. Frontend Implementation and Map Visualization

The frontend will be updated to display these "potential" stops as a distinct layer on the map, similar to the discovery mode.

* **UI Trigger:** In the `itinerary-view`, an "Explore Area" or "AI Suggestions" button will appear when a voyage has no stops.
* **Map Layer (`app/frontend/js/main.js`):**
    * **Recommendation Layer:** A new `recommendationMarkers` array will manage the lifecycle of these suggested points.
    * **Visual Style:** Use distinct icons for each type:
        * **Anchor:** `anchor` icon.
        * **Moor:** `crisis_alert` icon (standardized mooring symbol).
        * **Dock:** `directions_boat` or `storefront` icon.
    * **Interaction:** Clicking a recommendation marker will open an InfoWindow showing the agent's "Reasoning" and a button to "Add to Itinerary." This action would then call `API.createStop()` using the recommendation's data.

### 5. Workflow Summary

1.  **Creation:** User creates a voyage (e.g., "British Virgin Islands") with a center point.
2.  **Trigger:** User clicks "Get Recommendations."
3.  **Inference:** The **Local Pilot** agent researches the BVI, identifies the best marinas in Tortola, moorings in The Bight, and anchorages in White Bay.
4.  **Display:** The map populates with these specialized markers, grouped by type.
5.  **Action:** The user reviews the suggestions and clicks "Add to Itinerary" on a specific marina to turn it into an official `Stop` for Day 1.