## System Architecture

```
                                  [ User Interface (Google Maps JS) ]
                                 /                                   \
                 Upload GPX (Planned/Recorded)             Map Layers & Post-Sail Debrief
                               /                                       \
                              v                                         v
+----------------------------------------------------+   +---------------------------------------+
|                 Go Backend Engine                  |   |        PostgreSQL DB (000024)         |
|  - XML/GPX Parsing (Routes, Tracks, Points)        |-->|  - voyage_track (GeoJSON, metadata)   |
|  - Leg Auto-Splitting & Multi-Day Matching         |   |  - Track associations (Voyage / Stop) |
|  - Ramer-Douglas-Peucker (RDP) Downsampling        |   +---------------------------------------+
+-------------------------+--------------------------+
                          |
             Waypoint Weather Sampling & Track Delta
                          |
                          v
+----------------------------------------------------+
|               Researcher / AI Agents               |
|  - Lookout: Waypoint-interpolated weather warnings |
|  - Pilot: Planned vs. Actual tactical debrief       |
|  - Re-forecasting: Mid-trip dynamic updates        |
+----------------------------------------------------+

```

---

## Database Schema Migration

Add a migration `code/app/db/migrations/000024_add_gpx_tracks.up.sql` to manage planned routes and recorded tracks linked to voyages and individual stops:

```sql
CREATE TYPE track_kind AS ENUM ('planned', 'recorded');

CREATE TABLE voyage_track (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voyage_id UUID NOT NULL REFERENCES voyage(id) ON DELETE CASCADE,
    voyage_stop_id UUID REFERENCES voyage_stop(id) ON DELETE SET NULL,
    kind track_kind NOT NULL DEFAULT 'planned',
    name VARCHAR(255) NOT NULL,
    file_name VARCHAR(255),
    start_time TIMESTAMPTZ,
    end_time TIMESTAMPTZ,
    distance_nm NUMERIC(8,2),
    duration_interval INTERVAL,
    max_speed_kts NUMERIC(5,2),
    avg_speed_kts NUMERIC(5,2),
    geojson JSONB NOT NULL,
    simplified_geojson JSONB NOT NULL,
    raw_gpx TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_voyage_track_voyage ON voyage_track(voyage_id, kind);
CREATE INDEX idx_voyage_track_stop ON voyage_track(voyage_stop_id);

```

The rollback file `000024_add_gpx_tracks.down.sql` drops the table and enum:

```sql
DROP TABLE IF EXISTS voyage_track;
DROP TYPE IF EXISTS track_kind;

```

---

## Backend Implementation (`code/app/backend`)

### 1. GPX Processing Engine (`internal/gpx/`)

* **Parsing (`parser.go`):** Parses GPX XML schemas.


* `<rte>` / `<rtept>`: Treated as `planned` tracks containing waypoint coordinates and navigation targets.
* `<trk>` / `<trkseg>` / `<trkpt>`: Treated as `recorded` tracks extracting ISO-8601 timestamps, elevation/depth, and GPS fixes to calculate point-to-point SOG (speed over ground) and COG (course over ground).


* **Polyline Downsampling (`simplify.go`):** Implements the Ramer-Douglas-Peucker (RDP) algorithm. Generates a compact `simplified_geojson` payload (reducing vertex density by 80–90%) for Google Maps rendering and LLM agent context, while storing the full-resolution geometry in `geojson`.


* **Leg & Stop Matching:**
* **Single Master GPX:** Inspects track breaks, extended stationary periods, or proximity to configured `voyage_stop` coordinates to automatically split multi-day files into discrete legs.


* **Individual Leg GPX:** Directly associates with the targeted `voyage_stop_id`.





### 2. REST Endpoints (`internal/server/handlers/tracks.go`)

Registers routes in `internal/server/routes.go`:

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/voyages/{id}/tracks` | Upload master GPX (planned route or full voyage track). |
| `POST` | `/api/voyages/{id}/stops/{stopId}/tracks` | Upload leg-specific GPX (planned leg or completed segment). |
| `GET` | `/api/voyages/{id}/tracks` | Retrieve all GeoJSON tracks, metrics, and stop associations. |
| `DELETE` | `/api/voyages/{id}/tracks/{trackId}` | Delete a specific track. |
| `POST` | `/api/voyages/{id}/tracks/{trackId}/debrief` | Trigger an AI after-action review comparing planned vs. recorded. |

---

## Agent Pipeline Integration (`code/services/researcher`)

### 1. Weather Along Planned Track (`Lookout` Agent)

* **Current Behavior:** Weather is queried at fixed stop locations.


* **Enhanced GPX Pipeline:**
* Extracts sample coordinates along the planned track polyline at regular intervals (e.g., every 10–15 nautical miles or 2-hour projected travel windows).


* Queries `services/researcher/internal/tool/weather.go` for every sampled point.


* Evaluates wind vectors relative to boat heading, identifying adverse sea states, lee shores, and wind-against-tide hazards directly along the intended path.





### 2. Mid-Trip Updates & Live Leg Progress

* Uploading an in-progress track marks previous legs as completed and anchors the vessel's current position to the final trackpoint.
* Re-evaluates tidal gates and future leg departure windows based on actual progress rather than static estimates.
* Triggers an incremental refresh for the remaining voyage legs via the `Lookout` and `Pilot` tools.



### 3. After-Action Learning Engine (`Pilot` Agent)

Add a comparative debrief prompt to `code/services/researcher/internal/prompt/pilot.md`:

* **Track Variance Analysis:** Compares planned rhumb lines against actual GPS tracks, analyzing tacking efficiency, leeway, and detours.
* **Forecast vs. Reality:** Compares historical marine forecasts at waypoint times against observed boat speed and headings.
* **Actionable Observations:** Generates structured debrief insights (e.g., *"Leg 2 took 1.5 hours longer than projected due to a 2-knot adverse current near the headland; departure occurred 40 minutes before optimal slack water."*).

---

## Frontend & Google Maps Integration (`code/app/frontend`)

### 1. Google Maps Vector Layering (`js/geometry.js` / Map Renderer)

Leverages the existing Google Maps JavaScript API implementation:

```javascript
// 1. Planned Route (Dashed Nautical Cyan Line)
const lineSymbol = {
  path: 'M 0,-1 0,1',
  strokeOpacity: 1,
  scale: 3
};

const plannedRoute = new google.maps.Polyline({
  path: plannedCoordinates,
  strokeOpacity: 0,
  icons: [{
    icon: lineSymbol,
    offset: '0',
    repeat: '15px'
  }],
  strokeColor: '#0284c7',
  strokeWeight: 3,
  map: googleMapInstance
});

// 2. Recorded Track (Solid Safety Orange Line)
const recordedTrack = new google.maps.Polyline({
  path: recordedCoordinates,
  strokeColor: '#ea580c',
  strokeOpacity: 0.9,
  strokeWeight: 4,
  map: googleMapInstance
});

// 3. Interactive Point Inspection on Hover
recordedTrack.addListener('mousemove', (event) => {
  const pointData = findNearestTrackPoint(event.latLng);
  trackTooltip.setContent(`
    <div class="track-hud">
      <strong>Time:</strong> ${pointData.time}<br/>
      <strong>SOG:</strong> ${pointData.speed} kts | <strong>COG:</strong> ${pointData.bearing}°
    </div>
  `);
  trackTooltip.setPosition(event.latLng);
  trackTooltip.open(googleMapInstance);
});

```

### 2. UI Components

* **Track Upload Dropzone (`js/ui/`):** File upload modal supporting `.gpx` files with switches for track type (`Planned Route` vs. `Recorded Track`) and scope (`Full Voyage` vs. `Specific Leg`).


* **Map Layer Controls:** Visibility toggles for `Planned Route`, `Actual Track`, and `Divergence Overlay`.


* **Post-Sail Debrief Card:** Displays summary metrics alongside AI-generated passage observations:


* Planned vs. Actual Distance (nm)
* Planned vs. Actual Passage Duration
* Speed Over Ground profile (Min / Avg / Max)
* Tactical observations and lessons learned



---

## Phased Implementation Roadmap

* **Phase 1: Storage & GPX Parser**
* Apply migration `000024_add_gpx_tracks.up.sql`.


* Implement GPX XML parser and RDP simplification algorithm with Go test coverage.




* **Phase 2: API Endpoints**
* Build upload, retrieval, and delete handlers in `handlers/tracks.go`.


* Implement automated splitting for multi-leg master tracks.




* **Phase 3: Google Maps Visualization**
* Render planned dashed routes and recorded solid tracks via the Google Maps JavaScript API.


* Add vertex inspection tooltips (SOG, COG, timestamp) and layer toggle controls.




* **Phase 4: Lookout Agent Integration**
* Update `Lookout` weather queries to sample along the planned polyline instead of straight-line endpoints.


* Enable mid-trip re-forecasting anchored to the latest recorded coordinate.




* **Phase 5: Post-Voyage Pilot Debrief**
* Create the AI comparison prompt in `pilot.md`.


* Surface tactical post-sail debriefs and performance metrics on the voyage dashboard.

* **Phase 6: GPS Speed Glitch Filtering & Agent Tooling (v1.1.0)**
* Implement `FilterGPXData` tool in researcher agent service to detect and eliminate sudden GPS speed spikes and acceleration glitches (speed change over short time window, or speed exceeding plausible cutoff).
* Instruct Pilot Agent in `pilot.md` to invoke `FilterGPXData` before evaluating track performance and top speed whenever GPX data is present.
* Integrate speed change glitch detection directly into the backend `buildParsedTrack` parser to ensure stored `max_speed_kts` metrics are clean.