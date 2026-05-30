This updated implementation plan incorporates the specific structure for the `weather_summary` field into the backend models, database schema, and update service logic.

### 1. Database Schema Update
You will need a migration to add the tracking column for the last update time.

* **File:** `tpryan/navalplan/.../code/app/db/schema.sql`
* **Action:** Add the following column to the `briefing` table:
    ```sql
    ALTER TABLE briefing ADD COLUMN weather_last_updated TIMESTAMPTZ;
    ```

### 2. Model Modifications
To handle the new JSON structure safely, define a Go struct that matches your specified format and update the `Briefing` model.

* **File:** `tpryan/navalplan/.../code/app/backend/models/models.go`
* **New Struct:**
    ```go
    type WeatherSummary struct {
        Summary         string  `json:"summary"`
        Condition       string  `json:"condition"`
        TempMinF        float64 `json:"temp_min_f"`
        TempMaxF        float64 `json:"temp_max_f"`
        WindSpeedKt     float64 `json:"wind_speed_kt"`
        WindDirection   string  `json:"wind_direction"`
        WaveHeightFt    float64 `json:"wave_height_ft"`
        DebugDurationMs int64   `json:"debug_duration_ms"`
    }
    ```
* **Updated Briefing Struct:** Add `WeatherLastUpdated` to the existing struct:
    ```go
    type Briefing struct {
        ID                 int64     `json:"id" db:"id"`
        StopID             int64     `json:"stop_id" db:"stop_id"`
        WeatherSummary     RawJSON   `json:"weather_summary" db:"weather_summary"`
        WeatherLastUpdated *time.Time `json:"weather_last_updated" db:"weather_last_updated"`
        // ... other fields (SunPhase, Tides, etc.)
    }
    ```

### 3. Datastore Layer
Implement methods to retrieve future stops across all voyages and update their briefings.

* **File:** `tpryan/navalplan/.../code/app/backend/datastore/` (e.g., `stops.go` or a new `briefings.go`)
* **Logic:**
    * **`ListAllFutureStops`**: Query the `stop` table for all records where `target_date >= CURRENT_DATE`. Include a join with `voyage` if necessary to ensure the voyage hasn't been deleted.
    * **`UpsertWeatherBriefing`**: A method to update the `weather_summary` and `weather_last_updated` columns for a given `stop_id`. If a briefing record doesn't exist for the stop yet, it should be created.

### 4. Backend Service Logic (The Updater)
This logic will orchestrate the data fetching and transformation.

* **Handler:** `UpdateAllFutureWeather` (likely in `handlers/admin.go`)
* **Workflow:**
    1.  **Fetch Stops:** Get all future stops from the database.
    2.  **Fetch Weather:** For each stop, use its `Latitude` and `Longitude` to call the Open-Meteo API.
    3.  **Transform Data:** Map the Open-Meteo response into the `WeatherSummary` Go struct defined in Step 2.
    4.  **Save:** Serialize the `WeatherSummary` struct to JSON and call `UpsertWeatherBriefing` with the current timestamp for `weather_last_updated`.

### 5. Routing and Security
Register the administrative endpoint to trigger the update.

* **File:** `tpryan/navalplan/.../code/app/backend/server/routes.go`
* **Action:** Add the following route with `AuthLevel 2` (Admin):
    ```go
    {http.MethodPost, "/api/admin/weather/update-future", http.HandlerFunc(s.Handler.UpdateAllFutureWeather), 2},
    ```

### 6. Frontend Integration
The frontend can now use the specific properties of the `weather_summary` object and the update timestamp.

* **UI Update:** In the stop briefing component, display the specific data points:
    * **Conditions:** Use the `condition` and `summary` strings.
    * **Details:** Show `temp_min_f`, `temp_max_f`, `wind_speed_kt`, and `wave_height_ft`.
    * **Recency:** Display the `weather_last_updated` timestamp to the user (e.g., "Forecast as of 10:30 AM").
* **API Usage:** Use the updated data from existing endpoints like `/api/v1/stops/{id}/briefing` or `/api/v1/voyages/{id}/pilot_report`.