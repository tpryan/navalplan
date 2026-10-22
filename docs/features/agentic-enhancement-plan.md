# Engineering Plan: Agentic Enhancement & State-Backed Tool Architecture

**Date:** 2026-10-22  
**Status:** Proposed  
**Scope:** `code/services/researcher/`, `code/app/backend/internal/`, `code/app/frontend/`

---

## 1. Objective & Core Philosophy

NavalPlan is both a production voyage-planning platform and a showcase for multi-agent AI architecture. Currently, several agents feel "non-agentic" on close inspection—not because there are too many agents, but because they are given **fine-grained, chatty tools** and then **handcuffed to a 2-turn parallel checklist** so they don't time out or truncate 65K-token JSON payloads.

### The Core Shift: From "Checklist Pipe" to "Observe → Decide → Adapt"
1. **Agents Call Coarse-Grained Deterministic Tools (State-Backed):**
   * Every agent remains a first-class ADK agent that visibly invokes tools (preserving `/telemetry` SSE events, progress updates, and Vertex AI traces).
   * However, **the LLM is never used as a lossy copy-paste pipe for structured arrays**. When an agent calls `GetWeather`, `GetTides`, `GetSunriseSunset`, or `facility_discovery`, the Go tool writes the heavy raw payload (24h weather arrays, 48h tide tables, 100+ verified Google Places) directly into **ADK `session.State`** and returns a **compact analytical digest** to the model.
   * When the agent completes its reasoning, the backend merges the deterministic state arrays with the agent's synthesized intelligence.
2. **Free the Agent to Reason Across Signals & Iterate:**
   * Because the agent is no longer burning time and output tokens transcribing JSON arrays, it can use Turn 2 (and Turn 3 when needed) for genuine **agentic decision-making**: investigating causal chains across time/tide/daylight/pilotage, classifying start/end charter bases vs. overnight stops, finding bailout anchorages, and running forensic queries on GPX track anomalies.

---

## 2. Agent-by-Agent Enhancement Matrix

| Agent | Current Limitation | New Deterministic Tooling (Called by Agent) | New Agentic Reasoning Loop (Why It Needs an Agent) |
| :--- | :--- | :--- | :--- |
| **`specialist`** *(Voyage Recommender)* | Dumps 35–60 undifferentiated pins in 1 turn; no concept of how spots fit into a trip; drops marinas or hallucinates math in coords. | `SweepAreaInfrastructure` (sweeps Google Places across center + N/S/E/W sectors, saves verified marinas/fuel/ramps to `session.State`, returns sector coverage summary) + `FindCharterBasesAndAirports`. | **Voyage Role & Itinerary Architecture:**<br>1. Identifies **Start / End Bases** (where charter fleets, airport access, fuel, and provisioning converge).<br>2. Evaluates **Overnight Stops** at 1-day sailing rings (8–25 NM) from those bases—distinguishing **Transient Marinas (rentable overnight slips)** from private/day docks, and hunting Coast Pilot/cruising guides for **Protected Overnight Anchorages/Moorings**.<br>3. Tags every spot with `voyage_role` and `stay_details` for the Pilot Report and UI. |
| **`harbourmaster`** *(Stop Briefing)* | Blindly fetches stop data in 1 turn and re-emits 50KB of weather/tide/Places JSON. | State-backed `GetWeather`, `GetTides`, `GetSunriseSunset`, `SweepStopFacilities` (auto-expands radius rings `1 → 2.5 → 5 → 10 → 20 NM` in Go), and `ComputePassageTimeline` (aligns Stop $N$ departure + Stop $N+1$ arrival). | **Cross-Signal Interaction & Condition-Aware Selection:**<br>1. Analyzes how **Departure Tide + Transit Speed + Daylight + Weather + Coast Pilot** interact (e.g., *"Low tide is at 08:00 over a 5.5 ft bar; waiting until 10:30 pushes arrival at Stop $N+1$ past sunset, and Coast Pilot + NGA warnings say the entrance range lights are out"*).<br>2. Filters anchorage recommendations against the specific forecast wind direction and overnight wind shifts. |
| **`lookout`** *(Safety Auditor)* | Evaluates only Stop $N$'s static data; misses destination arrival state; still does some time-series math in-prompt. | `EvaluateSafetyThresholds` (expands `safety/validator.go` to cover all numeric, precip, 3h deterioration, wind-shift, wind-vs-wave, and per-segment `CalculateHeading` checks) + `ComputePassageTimeline` + `GetSafetyAlerts`. | **Cascade Hazard Investigation & Bailout Planning:**<br>1. Receives both Stop $N$ (departure) and Stop $N+1$ (arrival) environmental + hydrographic context.<br>2. When a hazard or timing cascade is triggered (e.g., tide-delayed departure → night arrival with unlit buoys, or 25 kt nose-wind on Segment 2), uses Turn 2 to query `hydrographic_pilot` / `SweepStopFacilities` for a **mid-leg Safe Haven / Bailout Anchorage** or calculates an **optimal departure window** (e.g., slack water). |
| **`debrief`** *(Post-Voyage)* | Zero tools; guesses tacking efficiency from 100KB of raw GPX XML. | `AnalyzeGPXTelemetry` (computes XTE, tack/gybe count, tack angles, VMG, and flags timestamped speed-drop/course-deviation anomalies), `GetHistoricalMarineWeather(lat, lng, time)`, `GetHistoricalTides(lat, lng, time)`. | **Forensic Track Investigator:**<br>Inspects the anomaly windows returned by `AnalyzeGPXTelemetry` (e.g., *"VMG dropped 45% across 6 tacks between 14:10–15:30"*), then calls weather/tide tools for that exact coordinate and timestamp to prove the tactical cause (e.g., *"Caught peak 2.8 kt foul ebb tide against 18 kt SW wind"*). |
| **`pilot`** *(Regional Guide)* | Runs 16 hardcoded searches regardless of voyage dates or itinerary. | `FetchRegionalBaseline` (concurrently fetches airports with exact km distances, marinas, and country/currency metadata into `session.State`). | **Adaptive Voyage-Scoped Briefer:**<br>Inspects the voyage dates and itinerary first: if the route crosses international waters, researches **Customs & Port-of-Entry procedures**; if during shoulder/hurricane season, drills into storm holes; feeds discovered charter fleets into `specialist`. |
| **`commodore`** *(Global Discovery)* | Invents noisy, overlapping GeoJSON polygons from scratch via web search. | `GetCandidateRegionsForMonth` (loads verified, non-overlapping canonical GeoJSON polygons from seed catalog) + `CheckSeasonalAnomalies` (search/climate check for ENSO, trade winds, major regattas). | **Live Seasonal & Crowd Arbitrage Scout:**<br>Selects from clean canonical geometries, then evaluates *current year* weather patterns, sargassum/cyclone anomalies, and regatta schedules to score and pitch the best Standard, Hidden Gem, and Challenging regions. |

---

## 3. Deep Dive: Priority Feature Workflows

### 3.1 `specialist`: Start/End Charter Bases vs. Overnight Stops (Slips & Anchorages)

Skippers planning a voyage need to answer two distinct questions that raw Google Places pins cannot answer on their own:
1. **"Where can I start and end this trip?" (`start_end_base`)**
   * Requires identifying hubs where **yacht charter companies** (bareboat/crewed fleets) operate, or major homeport marinas with airport proximity, fuel docks, customs (if international), and full provisioning.
2. **"Where can I stay each night along the route?" (`overnight_stop`)**
   * Requires distinguishing **Transient Marinas** (marinas that actually rent overnight visitor slips, have shore power/water, and monitor VHF/Dockwa—*not* private yacht clubs, boatyards, or dinghy docks) and **Overnight Anchorages / Mooring Fields** (verified holding ground, depth, and wind protection).

#### Agentic Execution Flow for `specialist`
* **Turn 1 (Infrastructure & Charter Sweep):**
  * Agent calls `SweepAreaInfrastructure(lat, lng, radius_meters, trip_type)` and `FindCharterBases(lat, lng, radius_meters)`.
  * Go executes the multi-sector Google Places sweep (center + N/S/E/W offsets), stores all raw verified facilities in `session.State["area_facilities"]`, and returns a structured **Sector & Harbor Summary** to the agent (listing candidate harbor towns, marina counts, fuel docks, and charter operators found).
* **Turn 2 (Role Investigation & Gap Filling):**
  * The agent identifies the 2–4 primary **Start/End Bases**.
  * It then checks the 8 compass sectors and 1-day sailing rings (10–25 NM from the bases) for **Overnight Stop** coverage.
  * It calls `hydrographic_pilot` and `batch_google_search` targeted specifically at candidate overnight harbors to verify:
    1. Which marinas offer **transient overnight slips** (and their VHF/reservation details).
    2. Which coves/bays offer **overnight anchorage or guest moorings** (and their holding, depth, and shelter).
* **Turn 3 (Structured Output):**
  * Agent outputs its curated recommendations with role classifications, which the backend merges with the verified coordinates/URLs in `session.State["area_facilities"]`.

#### Data Model & UI Surfacing for Recommendations
Add two columns to `voyage_recommendation` (via a new migration):
* `voyage_role TEXT DEFAULT 'overnight_stop'` — enum:
  * `"start_end_base"` — Charter fleet base or primary embarkation/turnaround harbor.
  * `"overnight_stop"` — Suitable for spending the night (either rentable transient slip, guest mooring, or sheltered anchorage).
  * `"day_stop"` — Lunch hook, day mooring, or scenic stop unsuitable for overnighting.
  * `"service_only"` — Fuel dock, pump-out, or boat launch ramp.
* `stay_details JSONB DEFAULT '{}'::jsonb` — structured capabilities:
  ```json
  {
    "is_charter_base": true,
    "charter_companies": ["The Moorings", "Sunsail"],
    "has_transient_slips": true,
    "slip_notes": "40 transient slips up to 60 ft; reserve via VHF 16 / Dockwa",
    "has_moorings": true,
    "has_anchorage": true,
    "holding_and_depth": "10-15 ft mud, good holding",
    "protection": "Sheltered from N, NE, E; exposed to S swell",
    "has_fuel": true,
    "provisioning": "Supermarket & chandlery within 0.3 NM"
  }
  ```

#### How This Surfaces in the Report & UI
1. **Pilot Report (`GET /api/v1/voyages/{id}/pilot_report` & Report View):**
   * Recommendations are grouped into clear planning tiers instead of a flat list:
     * **Recommended Start & End Bases (Charter & Turnaround Hubs)** — with links to the charter companies operating there and airport distance.
     * **Overnight Stops — Transient Marinas (Rent a Slip)** — showing slip notes, VHF, and fuel availability.
     * **Overnight Stops — Anchorages & Mooring Fields (On the Hook / Ball)** — showing depth, holding, and wind protection.
2. **Interactive Map & Sidebar UI (`code/app/frontend/js/main.js`):**
   * **Filter Bar:** Add role toggles (`Start/End Bases`, `Overnight (Slips)`, `Overnight (Anchor/Mooring)`, `Fuel & Day Stops`).
   * **InfoWindow & Sidebar Cards:** Display crisp capability badges (`Charter Base`, `Transient Slips`, `Overnight Anchorage`, `Fuel`) and shelter/holding pills.
   * **Itinerary Builder Assist:** When a voyage has no stops yet, highlight `Start/End Bases` first (with a *"Use as Start & Return Base"* option for round-trip charters). Once Stop 1 is placed, highlight `Overnight Stops` within 10–25 NM.

---

### 3.2 `harbourmaster` & `lookout`: Cross-Signal Interaction Reasoning (Tide × Departure Time × Daylight × Coast Pilot)

For a specific stop on a specific date, the highest-value intelligence is **how the environmental and hydrographic constraints interact across the clock**—both leaving Stop $N$ and arriving at Stop $N+1$.

#### Concrete Interaction Chains the Agents Must Catch
1. **Tide-Gated Departure → Late Arrival → Night Approach Hazards:**
   * *Scenario:* You plan to depart Stop $N$ in the morning, but Low Tide is at 08:30 and Coast Pilot notes a 5.5 ft controlling depth over the entrance bar or inner channel.
   * *Chain:* Waiting for mid-flood (10:30) delays departure by 2.5 hours → On a 26 NM leg against a 15 kt headwind, arrival at Stop $N+1$ shifts to 17:45 (after 17:10 sunset) → Coast Pilot / NGA Broadcast Warnings for Stop $N+1$ state that the entrance channel is unlit or a range light is extinguished.
2. **Tidal Current Gate vs. Wind Direction (Wind-Against-Tide Rips):**
   * *Scenario:* The planned route passes through a narrow strait or inlet (e.g., Woods Hole, Plum Gut, Cuttyhunk entrance).
   * *Chain:* Departing at 08:00 puts the boat at the chokepoint at 11:00 → 11:00 is peak 3.2 kt ebb current flowing West → Forecast wind at 11:00 is 20 kt SW (directly opposing the ebb) → Dangerous steep breaking seas in the channel.
3. **Arrival Tide State at Destination Anchorage / Marina:**
   * *Scenario:* Departing Stop $N$ at high water is easy, but arriving at Stop $N+1$ 5 hours later coincides with dead Low Water at a shallow destination anchorage or fuel dock.
4. **Overnight Wind Shift vs. Anchorage Protection:**
   * *Scenario:* Arrival wind at 16:00 is 10 kt SE, making an east-sheltered cove look calm, but the hourly forecast shows a frontal passage at 01:00 shifting wind to 24 kt NW (putting the boat on a lee shore in the dark).

#### New Tool: `ComputePassageTimeline` (`code/services/researcher/internal/tool/timeline.go`)
Give `harbourmaster` and `lookout` a deterministic Go tool that takes `(stopN, stopNPlus1, date, plannedTrack)` and computes an aligned hour-by-hour passage matrix:
* Fetches/reads Stop $N$ **and** Stop $N+1$ Tides, Weather, and Sunrise/Sunset.
* Computes candidate departure times (`06:00`, `07:00`, `08:00`, `09:00`, `10:00`, `11:00`, `12:00`) and, for realistic boat speeds (4, 5, 6, 7 kts, penalized when `CalculateHeading` shows beating into headwinds), outputs:
  * **Departure State at Stop $N$:** Tide height & stage (e.g., `0.3 ft, dead low — rising`), wind/wave, daylight margin after sunrise.
  * **En-Route Segment Timing:** ETA at key planned route waypoints/chokepoints + wind/wave at that hour.
  * **Arrival State at Stop $N+1$:** ETA vs. Sunset (e.g., `Arrives 42 min AFTER sunset`), tide height & stage at Stop $N+1$ upon arrival, and wind/wave at arrival.

#### Multi-Turn Agentic Loop in `harbourmaster` & `lookout`
* **Turn 1 (Gather & Align):**
  * Agent calls `ComputePassageTimeline`, `SweepStopFacilities` (state-backed), `GetSafetyAlerts`, and `hydrographic_pilot` (passing **both** Stop $N$ and Stop $N+1$ so RAG retrieves entrance depths, bridge clearances, currents, and lighted aids for both ends of the leg).
* **Turn 2 (Investigate Conflicts & Bailouts):**
  * If `ComputePassageTimeline` + `hydrographic_pilot` reveal a conflict (e.g., morning low-tide bar constraint, or arrival within 1 hour of sunset / after dark, or an opposing wind-against-tide gate):
    * The agent executes a targeted follow-up `hydrographic_pilot` / `GetSafetyAlerts` query for **night-entry hazards / unlit buoys / light outages at Stop $N+1$** or **bailout anchorages along the route**.
    * It identifies the **Optimal Departure Window** (e.g., *"Depart either before 06:30 prior to low water, or wait until 10:15 mid-flood and motor-sail at 6+ kts to beat the 17:10 sunset because Cuttyhunk inner buoys are unlit"*).

#### Surfacing in the Stop Briefing & UI
Add a structured `passage_plan` section inside `pilot_notes` (and surface interaction alerts at the top of `lookout`):
```json
"pilot_notes": {
  "overview": "...",
  "departure_and_arrival_timing": {
    "recommended_departure_window": "09:45 AM – 10:30 AM",
    "departure_tide_constraint": "Low tide (0.4 ft) at 08:15 AM restricts the 6 ft entrance bar until ~09:45 AM.",
    "arrival_outlook": "Departing at 10:00 AM (24 NM @ 5 kts) yields a 02:48 PM arrival at [Next Stop] on a rising tide (+3.8 ft), 2h 20m before sunset.",
    "night_entry_and_aids_warning": "Avoid departing after 12:00 PM: arrival after 05:10 PM sunset is hazardous as Coast Pilot notes unlit inner channel buoys at [Next Stop]."
  },
  "approach_and_channels": "...",
  "anchorages_and_moorings": "...",
  "regulations_and_hazards": "...",
  "sources": ["NOAA Coast Pilot 2, Ch. 5", "..."]
}
```

---

## 4. State-Backed Tool Architecture (How We Eliminate JSON Bloat Without Removing Agents)

To keep every agent fast and prevent 65K-token JSON truncation while keeping all tool calls inside the ADK agent:

1. **Tool Execution Writes to ADK `session.State`:**
   * `GetWeather` saves the full `WeatherResult` (with all 24-element hourly slices) to `ctx.Session().State().Set("briefing_weather", result)` and returns a compact 300-token digest to the LLM (peak wind, prevailing direction, min/max temp, wind shift hours, precip hours, wave peaks, and arrival/departure window conditions).
   * `GetTides` saves the full 48h `TideResult` to `ctx.Session().State().Set("briefing_tides", result)` and returns only the target date's High/Low extremes, tidal range, and station note to the LLM.
   * `GetSunriseSunset` saves `SunriseResult` to `ctx.Session().State().Set("briefing_sun", result)` and returns sunrise/sunset times.
   * `SweepStopFacilities` / `SweepAreaInfrastructure` runs the concurrent Google Places queries and radius expansion in Go, saves the full verified `[]Facility` slice to `ctx.Session().State().Set("verified_places", places)`, and returns a categorized summary to the LLM.
2. **Agent Emits Only High-Value Synthesis:**
   * The LLM outputs `pilot_notes` (including `departure_and_arrival_timing`), `weather_headline`, and any `wild_anchorages` / `voyage_role` classifications discovered from Coast Pilot and web search.
3. **Backend Assembler Combines State + Synthesis:**
   * `research.go` and `recommendation.go` read the deterministic structs from session state, merge the LLM's synthesized fields and geocoded wild anchorages, and persist the final `model.Briefing` or `[]model.VoyageRecommendation`.

---

## 5. Progressive "Build-As-You-Go" UI & Streaming Architecture

Multi-turn agentic reasoning takes longer end-to-end (~12–20s), so the UI should never block on a final monolithic response or cycle through fake timer-driven status strings (`time.NewTicker(8 * time.Second)` in `research.go:402` and `recommendation.go:327`).

Because ADK `/api/run` already streams every `function_call`, `function_response`, and `state_delta` event over SSE as tools execute, we can **hydrate the UI wave-by-wave in real time**.

### 5.1 Wave-by-Wave Hydration per View

#### A. Stop Briefing (`harbourmaster` + `lookout`) — 4-Wave Progressive Assembly
| Wave | Elapsed Time | Trigger Event | Immediate UI Update |
| :--- | :--- | :--- | :--- |
| **Wave 1: Environment & Tides** | **~1.5s** | `GetWeather`, `GetTides`, `GetSunriseSunset` complete in Turn 1 | **Hourly Weather strip, Tide Chart, and Sunrise/Sunset cards render immediately.** Numeric threshold alerts (`safety/validator.go`) appear in the Lookout panel. |
| **Wave 2: Map Pins & Local Facilities** | **~3s** | `SweepStopFacilities` completes in Turn 1 | **Marinas, moorings, fuel docks, and shore amenities pop onto the map and facility list.** |
| **Wave 3: Passage Timeline & Pilotage** | **~8–12s** | `ComputePassageTimeline` + `hydrographic_pilot` + `harbourmaster` synthesis complete | **Pilot Notes, Departure/Arrival Tide Window card, and Wild Anchorages** fill in. |
| **Wave 4: Lookout Cascade & Bailouts** | **~15–20s** | `lookout` completes Turn 2 cross-signal & night-approach audit | **Qualitative Lookout Alerts** (e.g., *"Low tide delays departure → arrives after dark with Entrance Light #4 out"* + bailout cove) slide into the Safety panel. |

#### B. Voyage Recommendations (`specialist`) — Live Pin Upgrading
* **Wave 1 (~2–3s — Instant Map Population):** As soon as `SweepAreaInfrastructure` and `FindCharterBases` finish in Turn 1, verified marinas, fuel docks, and mooring fields stream onto the map immediately via `recHub`.
* **Wave 2 (~8–12s — Role Badging & Wild Anchorages):** As `specialist` finishes evaluating charter fleets and transient slip policies in Turn 2/3, an SSE update upgrades those pins in place with **`Start / End Base`** and **`Overnight Slip`** badges + reasoning, and drops in the newly discovered **Wild Anchorages** from Coast Pilot and cruising guides.

#### C. Regional Guide (`pilot`) — Card-by-Card Reveal
* **Wave 1 (~2s):** **Nearest Airports** (with exact km distances) and **Country / Currency / Emergency Numbers** cards render immediately when `FetchRegionalBaseline` returns.
* **Wave 2 (~8–12s):** **Sailing Season, Charter Fleets, Security/Customs, and Hazards** cards fill in when the LLM completes its synthesis.

### 5.2 Plumbing Changes Required

1. **Stream Intermediate ADK Events in `AgentRunner` (`code/app/backend/internal/agent/agent.go`):**
   * Currently `RunStreaming` (line 250) discards every ADK SSE event except `role == "model"` text parts.
   * Extend `AgentEvent` to capture `FunctionCall`, `FunctionResponse`, and `Actions.StateDelta`, and add `RunStreamingWithEvents(ctx, appName, userID, sessionID, prompt, onEvent func(AgentEvent))` so handlers receive live tool-start and tool-completion callbacks as the agent works.
2. **Extend `ProgressEvent` with Partial Section Payloads (`code/app/backend/internal/model/models.go`):**
   ```go
   type ProgressEvent struct {
       Stage   string          `json:"stage"`             // "start", "tool", "partial", "done", "error"
       Section string          `json:"section,omitempty"` // "weather", "tides", "sun", "facilities", "pilot_notes", "alerts", "airports"
       Message string          `json:"message"`           // Live tool-driven status (e.g. "Checking Coast Pilot for night entry lights at Cuttyhunk...")
       Data    json.RawMessage `json:"data,omitempty"`    // Partial JSON payload for immediate UI section hydration
   }
   ```
   * When each state-backed tool returns, the backend immediately persists that column to the DB **and** broadcasts a `Stage: "partial"` event with the section's JSON on `progressHub`.
   * Replace the hardcoded 8–10s ticker goroutines in `research.go` and `recommendation.go` with real `Stage: "tool"` events derived from the agent's live `FunctionCall` name and arguments.
3. **Frontend Progressive Section Hydrator (`code/app/frontend/js/main.js`):**
   * Update the `API.streamProgress(sessionID, evt => ...)` handler to inspect `evt.stage`:
     * On `evt.stage === "partial"`, immediately invoke the matching section renderer (`renderWeatherSection(evt.data)`, `renderTidesSection(evt.data)`, `renderFacilitiesSection(evt.data)`, `renderPilotNotesSection(evt.data)`, `renderLookoutAlerts(evt.data)`) and remove that section's skeleton loader.
     * On `evt.stage === "tool"`, update the inline activity banner with the real tool action (`evt.message`).

---

## 6. Phased Implementation Roadmap

### Phase 1: State-Backed Tools, Progressive SSE Plumbing & Deterministic Safety Expansion
* Update `GetWeather`, `GetTides`, `GetSunriseSunset`, and `FindPlacesNearby` (`SweepStopFacilities` / `SweepAreaInfrastructure`) to write full payloads to ADK `session.State` and return compact analytical digests to the agent.
* Add `RunStreamingWithEvents` in `code/app/backend/internal/agent/agent.go`, extend `model.ProgressEvent` with `Section` and `Data`, and wire progressive section hydration in `code/app/frontend/js/main.js` (removing the fake ticker loops).
* Expand `code/app/backend/internal/safety/validator.go` to cover all numeric and time-series thresholds (precip, 3h rapid deterioration, wind/temp shifts, wind-vs-wave opposition, and per-segment `CalculateHeading`) and emit Wave 1 threshold alerts immediately.

### Phase 2: `ComputePassageTimeline` & Cross-Signal Stop/Lookout Reasoning
* Build `ComputePassageTimeline` in `code/services/researcher/internal/tool/timeline.go` combining Stop $N$ (departure) and Stop $N+1$ (arrival) tides, sun phase, weather, and planned route segments.
* Update `harbourmaster` and `lookout` prompts + `hydrographic_pilot` to pull Coast Pilot / NGA Sailing Directions and `GetSafetyAlerts` for **both** Stop $N$ and Stop $N+1$.
* Enable a 2–3 turn investigation loop so `harbourmaster` and `lookout` explicitly synthesize **Tide × Departure Time × Daylight × Light/Buoy Hazards** into `pilot_notes.departure_and_arrival_timing` and Lookout alerts (plus mid-leg bailout anchorages when a leg is hazardous).
* Surface the new **Departure & Arrival Timing** card in the Stop Briefing UI and Day Report (Wave 3 & Wave 4).

### Phase 3: `specialist` Role Classification (Start/End Charter Bases vs. Overnight Stops)
* Add migration for `voyage_role` and `stay_details` on `voyage_recommendation`.
* Add `FindCharterBases` and `SweepAreaInfrastructure` tools; pass regional charter fleet context from `pilot` (Voyage Guide) when available.
* Update `specialist` to run a multi-turn loop: (1) stream verified infrastructure pins in Wave 1 and identify `start_end_base` hubs with charter fleets/airports/fuel, (2) evaluate 1-day sailing rings for `overnight_stop` transient marinas (rentable slips) and protected anchorages/moorings, and (3) stream Wave 2 role badges (`voyage_role` and `stay_details`) + wild anchorages.
* Update the Frontend Map Filter Bar, InfoWindow badges, Itinerary Builder, and Pilot Report (`GET /api/v1/voyages/{id}/pilot_report`) to group and filter by **Start/End Bases**, **Overnight Slips**, and **Overnight Anchorages**.

### Phase 4: Forensic `debrief`, Adaptive `pilot`, and Grounded `commodore`
* **`debrief`:** Build `AnalyzeGPXTelemetry` in `internal/gpx` (computing XTE, tack count, VMG, and timestamped anomaly windows) and give `debrief` historical weather/tide lookup tools so it conducts a true multi-step forensic investigation of where and why the boat slowed down or deviated.
* **`pilot`:** Make the Regional Guide agent adaptive to the voyage's actual stops and dates (e.g., autonomous Customs/Port-of-Entry research for international crossings, storm-hole analysis for hurricane-season dates) while streaming `airports` and baseline country cards in Wave 1.
* **`commodore`:** Back `commodore` with a canonical `discovery_regions.json` geometry tool (`GetCandidateRegionsForMonth`) paired with live seasonal anomaly/event search tools.
