# System Design: NavalPlan

**Status:** Draft
**Goal:** A standalone sailing voyage planner that uses AI to research destinations day-by-day and generates professional "Captain's Logbooks" (via Google Docs).

---

### 1. High-Level Architecture

NavalPlan is a peer application to Navallog. It shares the same deployment infrastructure (Google Cloud Run, Cloud SQL) but operates as a distinct service to separate "Planning" (future) from "Logging" (past).

* **Frontend (SPA):** A map-centric interface (Mapbox GL JS + Vite) for visual route planning.
* **Backend (Go):** A RESTful API handling session management, CRUD operations for voyages, and orchestration of the agent.
* **Researcher Service (Cloud Run):** A specialized microservice running the Google ADK (Gemini) to perform "on-demand" research for specific coordinates and dates.
* **Database (PostgreSQL):** Stores users, voyage plans, stops, and cached agent reports.

---

### 2. Core Concepts & Data Model

The system models a trip not just as a geometry (line on a map), but as a chronological sequence of **Stops** (where you spend the night).

#### 2.1 Database Schema

```sql
-- Users (Potential to link with Navallog users via Google ID)
CREATE TABLE users (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    google_id VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Voyages: The container for a trip
CREATE TABLE voyages (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,     -- e.g., "Gulf Islands Summer 2025"
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    google_doc_id VARCHAR(255),      -- Link to the exported logbook
    last_exported_at TIMESTAMP
);

-- Stops: A specific location for a specific date
CREATE TABLE stops (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyages(id) ON DELETE CASCADE,
    target_date DATE NOT NULL,
    location_name VARCHAR(255),      -- User defined or Agent resolved
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    search_radius_nm INT DEFAULT 5,  -- Area to research (default 5nm)
    notes TEXT,                      -- Skipper's manual notes
    UNIQUE(voyage_id, target_date)   -- One stop per day (simplification for v1)
);

-- Briefings: The raw data returned by the Agent
CREATE TABLE briefings (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    stop_id INTEGER REFERENCES stops(id) ON DELETE CASCADE UNIQUE,
    weather_summary JSONB,           -- Wind, Gusts, Wave Height (Forecast or Hist. Avg)
    tides JSONB,                     -- High/Low water times and heights
    facilities JSONB,                -- List of Anchorages, Marinas, Moorings found
    created_at TIMESTAMP DEFAULT NOW()
);

```

---

### 3. Service: The Researcher (AI Agent)

The **Researcher** is a stateless service built with `google.golang.org/adk`. It acts as a "Virtual Harbourmaster."

* **Input:** `{ latitude, longitude, date, radius_nm }`
* **Tools:** `Google Search` (Search), `weather_lookup` (Optional/External API).
* **System Instruction:**
> "You are an expert sailing navigator. Given a coordinate and a date, research the immediate area (5nm radius).
> 1. **Facilities:** Identify anchorages (note protection/holding), marinas (VHF/Phone), and mooring fields.
> 2. **Environment:** Retrieve the specific weather outlook for {Date}. If >10 days out, provide historical averages. Find tidal predictions for the nearest station.
> Return a strictly structured JSON object."
> 
> 



---

### 4. Feature: Voyage Sharing
* **Goal:** Allow users to share a read-only view of their voyage with friends/family via a secret link.
* **Technology:** Unique Token generation, Public API endpoints.
* **Flow:**
    1. User clicks "Share".
    2. Server generates a random URL-safe token (saved to `voyage` table).
    3. User gets a link: `https://navalplan.app/shared/{token}`.
    4. Anyone with the link can view the Itinerary and Guide (Read-Only).


---

### 5. Frontend UX (Mapbox GL JS)

The UI is designed for "Day-by-Day" planning.

* **Layout:**
* **Left Sidebar (Itinerary):** A vertical timeline of dates based on the Voyage start/end.
* **Main View (Map):** Interactive globe.


* **Workflow:**
1. User clicks "July 14" in the sidebar.
2. Map highlights any existing stop or prompts "Click map to set destination."
3. User clicks a bay.
4. System draws a **5nm radius circle** around the click.
5. User clicks "Research Area."
6. **Agent Async State:** The circle pulses or changes color while the agent runs.
7. **Result:** Markers appear inside the circle (Anchors, Marinas). Sidebar fills with Weather/Tide data.



---

### 6. Integration API

We will expose a standard REST API.

* `POST /api/v1/voyages`: Create a new trip.
* `POST /api/v1/voyages/{id}/stops`: Add a stop (User clicks map).
* `POST /api/v1/stops/{id}/research`: Trigger the Agent (Async).
* `GET /api/v1/stops/{id}/briefing`: Poll for agent results.
