# Engineering Plan: Full-Screen Tabbed & Day-Filtered Voyage Report

**Date:** 2026-10-22  
**Status:** Proposed  
**Scope:** `code/app/frontend/index.html`, `code/app/frontend/js/main.js`, `code/app/frontend/css/components.css`, `code/app/frontend/css/layout.css`

---

## 1. Objective

Replace the cramped `#modal-report` overlay and binary **Sailing Mode vs. All Data** toggle (`.np-report--sailing-only`) with a **dedicated Full-Screen Report View** featuring two orthogonal filtering axes:

1. **Category Tabs:** `All` · `Navigation` · `Weather` · `Amenities`
2. **Day Filter Bar:** `All Days` · `Day 1 (Stop Name)` · `Day 2 (Stop Name)` · … (plus `Overview / Regional Guide` when on `All Days`)

This gives the skipper an at-a-glance cockpit reference where they can lock onto **Day 2 → Navigation** (or **Day 2 → All**) on a single screen without scrolling through a 15-page document.

---

## 2. Information Architecture & Category Mapping

Every section in `generateReportHTML()` (`code/app/frontend/js/main.js:7284–7729`) maps cleanly to one of the three domain categories (and all appear under **`All`**):

| Report Section | `All` | `Navigation` | `Weather` | `Amenities` | Scope (`All Days` vs. `Day N`) |
| :--- | :---: | :---: | :---: | :---: | :--- |
| **Voyage Hero & Summary Stats** | ✓ | ✓ *(NM, Stops, Moorings, Anchorages)* | ✓ *(Season / Wind summary)* | ✓ *(POIs, Marinas)* | `All Days` only (compact header on `Day N`) |
| **Voyage Map Snapshot & Captain Check-in** | ✓ | ✓ | — | — | `All Days` only |
| **Day-by-Day Summary Grid (`.np-day-grid`)** | ✓ | ✓ *(Distance NM & Mini Tide)* | ✓ *(Condition, Temp, Wind, Sun, Mini Tide)* | — | `All Days` only (clicking a tile jumps to `Day N`) |
| **Voyage-Wide Safety Overview (`#np-safety-overview`)** | ✓ | ✓ *(Navigation/Tide alerts)* | ✓ *(Weather/Sun alerts)* | — | `All Days` only |
| **Destination Guide (`.np-guide-section`)** | ✓ | Hazards, Security & Safety | Sailing Season & Storm Risk | Hubs, Charter Info, Airports, Country/Currency, POIs | `All Days` only |
| **Area Recommendations (`.np-rec-section`)** | ✓ | Anchorages, Moorings, Start/End Bases | — | Marinas, Hubs, Fuel, Other | `All Days` only |
| **Stop Header (Number, Location, Date, Coords)** | ✓ | ✓ | ✓ | ✓ | Every Stop (`All Days` & `Day N`) |
| **Stop Weather Grid & 24h Hourly Timeline** | ✓ | — | ✓ | — | Per Stop (`All Days` & `Day N`) |
| **Stop Tide Chart (`#reportTideChart_N`)** | ✓ | ✓ *(Tidal heights/gates affect both nav & weather)* | ✓ | — | Per Stop (`All Days` & `Day N`) |
| **Stop Safety Lookout (`.lookout-box`)** | ✓ | ✓ *(All alerts + Travel Plan table)* | ✓ *(Weather/Sun/Tide alerts)* | — | Per Stop (`All Days` & `Day N`) |
| **Stop Pilot Notes (`renderPilotNotesHTML`)** | ✓ | ✓ *(Overview, Approach/Channels, Anchorages, Regulations, Departure Timing)* | — | — | Per Stop (`All Days` & `Day N`) |
| **Stop Facilities (`.np-facility-report-card`)** | ✓ | Anchorages, Moorings, Fuel Stations, Boat Launches | — | Marinas, Yacht Clubs, Restaurants, Bars, Cafes, Stores, Pharmacies, Services | Per Stop (`All Days` & `Day N`) |
| **Passage Tactical Debrief (`.np-report-stop-debrief`)** | ✓ | ✓ *(Track comparison, tacking efficiency, conclusions)* | ✓ *(Weather impact)* | — | Per Stop (`All Days` & `Day N`) |

---

## 3. UI & Layout Specification

### 3.1 Full-Screen Viewport Container
Convert `#modal-report` from a centered modal box with a backdrop overlay into a full-viewport workspace view (`.np-report-fullscreen`):
* **Dimensions:** `position: fixed; inset: 0; width: 100vw; height: 100vh; z-index: 1000; background: var(--bg-canvas);`
* **No Backdrop Required:** Hide `#modal-overlay` when the report is open; provide a clear **`← Back to Voyage`** button in the top-left of the sticky header (plus `Escape` key support).
* **Shared & Print Parity:** Both `/voyages/{id}/report`, `/shared/{token}`, and `/voyages/{id}/print` share the exact same category tab bar and day selector (hidden on `@media print`, which prints the currently active tab + day selection).

### 3.2 Sticky Two-Tier Control Header
Replace `.modal-header-row` and the legacy `#btn-toggle-sailing-mode` button with a sticky header bar:

```text
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ [← Back to Voyage]   Voyage Report: BVI Spring Charter        [Audit] [Share] [Copy] [⎙] │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ TABS:  [ All ]   [ 🧭 Navigation ]   [ 🌬️ Weather & Tides ]   [ ⚓ Amenities & Shore ]   │
├──────────────────────────────────────────────────────────────────────────────────────────┤
│ DAYS:  ( All Days )  ( Day 1: Road Town )  ( Day 2: Norman Isl )  ( Day 3: North Sound ) │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

1. **Primary Category Tabs (`role="tablist"`):**
   * `all` — **All Data** (icon: `dashboard`)
   * `navigation` — **Navigation** (icon: `explore`) — Pilot notes, approach channels, anchorages/moorings, travel plan table, navigation safety alerts, tide curves, GPX debriefs.
   * `weather` — **Weather** (icon: `air`) — Daily & 24h hourly wind/wave/precip/temp timelines, sunrise/sunset, tide curves, weather safety alerts, seasonal outlook.
   * `amenities` — **Amenities** (icon: `storefront`) — Marinas, restaurants, bars, provisioning stores, pharmacies, chandleries, charter companies, airports, country/currency info.
2. **Secondary Day Filter Strip (Horizontal Scrollable Pill Bar):**
   * **`All Days`** (default) — Shows the voyage-level overview + all stops in sequence.
   * **`Day 1 · <Short Location>`**, **`Day 2 · <Short Location>`**, … — Clicking any day pill:
     * Hides the voyage-wide preamble (Hero, Map Snapshot, Regional Guide, Multi-day Grid) so the selected day sits **at the very top of the viewport**.
     * Displays a compact **Single-Day Banner** (`Day 2 of 5 · Tue, May 12 · Norman Island → Cooper Island (6.4 NM)`) with **`← Prev Day`** and **`Next Day →`** quick-step buttons.
   * Clicking any tile in the **Day-by-Day Grid** on `All Days` also selects that day pill automatically.

---

## 4. Technical Implementation Plan

### Phase 1: Full-Screen Shell & URL Routing
* **Files:** `code/app/frontend/index.html`, `code/app/frontend/css/components.css`, `code/app/frontend/js/main.js`
* **Changes:**
  1. Update `#modal-report` in `index.html` to `.np-report-fullscreen` with the two-tier header (`#report-category-tabs` and `#report-day-pills`).
  2. Remove `#btn-toggle-sailing-mode` and replace `.np-report--sailing-only` with data-attribute filtering:
     * Container attributes: `data-report-tab="all|navigation|weather|amenities"` and `data-report-day="all|1|2|..."`.
  3. **URL Deep-Linking:** Support clean URL state in `handleRouting()` and `window.history.replaceState`:
     * `/voyages/{id}/report` → `tab=all`, `day=all`
     * `/voyages/{id}/report?tab=navigation&day=2` (and preserve backward compatibility for `/voyages/{id}/report/sailing` by mapping it to `tab=navigation`).
     * Mirror the query params on `/shared/{token}?tab=...&day=...` and `/voyages/{id}/print?tab=...&day=...`.

### Phase 2: Granular Section Tagging in `generateReportHTML()`
* **Files:** `code/app/frontend/js/main.js`, `code/app/frontend/css/components.css`
* **Changes:**
  1. Tag each block emitted by `generateReportHTML()` and `generateGuideHTML()` with `data-report-cats="..."` (space-separated list of matching categories, e.g. `data-report-cats="all navigation"`, `data-report-cats="all weather"`, `data-report-cats="all navigation weather"` for Tide Charts and Safety Lookout boxes).
  2. Tag each stop container `.np-report-stop` with `data-stop-day="${idx + 1}"` and voyage-level overview sections with `data-report-scope="overview"`.
  3. **Split Facilities by Category:**
     * Inside each stop's facilities loop (`main.js:7684`), tag each `.np-facility-report-card` based on `f.type`:
       * `Anchorage`, `Mooring`, `Fuel Station`, `Boat Launch` → `data-report-cats="all navigation"`
       * `Marina`, `Yacht Club` → `data-report-cats="all navigation amenities"` (relevant to both docking navigation and shore amenities)
       * `Restaurant`, `Bar`, `Cafe`, `Store`, `Pharmacy`, `Services` → `data-report-cats="all amenities"`
     * Hide the `.np-facilities-wrapper` heading automatically if zero child cards match the active category tab.
  4. **Filter Lookout Alerts by Category:**
     * Tag each rendered alert item (`alertToHTML(a)`) with `data-alert-cat="${a.category}"`:
       * `navigation`, `tides` → shown under `Navigation` (and `All`)
       * `weather`, `sun`, `tides` → shown under `Weather` (and `All`)

### Phase 3: Single-Day Focused View & Chart Redraw Handling
* **Files:** `code/app/frontend/js/main.js`, `code/app/frontend/css/components.css`
* **Changes:**
  1. Add `setReportFilter({ tab, day })` helper in `main.js`:
     * Updates active states on `#report-category-tabs` and `#report-day-pills`.
     * Updates `data-report-tab` and `data-report-day` on the report root container.
     * Scrolls the report scroll container back to `top: 0`.
     * Re-triggers Chart.js resize/render (`renderTideChart`) if a previously hidden tide canvas becomes visible when switching to `Navigation`, `Weather`, or `All`.
  2. Add a **Single-Day Navigation Header** at the top of `.np-report-stop` (visible when `data-report-day != "all"`) with `← Day N-1` and `Day N+1 →` buttons and keyboard arrow support (`ArrowLeft` / `ArrowRight` when focused in the report).
  3. Make each `.np-day-tile` in the `All Days` summary grid clickable (`onclick` → `setReportFilter({ day: idx + 1 })`) so clicking Day 3's summary tile jumps straight into Day 3's single-day view.

### Phase 4: Print, Copy & Tests
* **Files:** `code/app/frontend/js/main.js`, `code/app/frontend/spec/track.spec.mjs`
* **Changes:**
  1. **Print (`window.print()`):** Respects the currently selected `tab` and `day` so a skipper can print just **Day 2** or just the **Navigation** packet for the whole trip.
  2. **Copy Report (`#btn-copy-report`):** Copies only the currently visible sections matching the active `tab` and `day` filter.
  3. **Unit Tests (`npm test`):** Add Jasmine specs verifying category tagging (`navigation`, `weather`, `amenities`, `all`), facility card classification, and single-day filtering.
