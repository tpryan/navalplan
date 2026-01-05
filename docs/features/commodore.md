# Product Requirement Document: Seasonal Sailing Discovery ("The Commodore")

**Status:** Draft
**Feature:** Global Seasonal Discovery & "Hidden Gem" Recommendations

### 1. Overview

Current sailing planners require a user to *already know* where they want to go. This feature flips the model: it helps users answer **"Where should I sail in [Month]?"**

It presents an interactive world map highlighting regions that are in prime sailing season for a selected month. Crucially, it differentiates between **"Standard"** destinations (e.g., BVIs in January) and **"Deep Cuts"** (e.g., Chesapeake Bay in May, Maine in September)—non-obvious choices that offer excellent conditions without the crowds.

### 2. Goals

1. **Discovery:** Allow users to explore destinations based on *time of year*.
2. **Curated Intelligence:** Surface "Deep Cuts"—locations that are statistically good (weather/wind) but often overlooked.
3. **Contextual Grading:** Explain *why* a location is good right now (e.g., "Post-hurricane season," "Trade winds established," "Shoulder season pricing").

### 3. User Experience (UX)

#### 3.1 The "Commodore" View

* **Interface:** A full-screen Mapbox GL JS view (distinct from the specific Voyage planner).
* **Controls:** A dominant **"Month Slider"** (Jan - Dec) at the bottom.
* **Visuals:**
* As the slider moves, polygons appear/disappear on the map.
* **Color Coding:**
* 🔵 **Blue:** Established/Popular Hubs (The "Milk Run").
* 🟣 **Purple:** "Deep Cuts" / Hidden Gems (High reward, lower traffic).




* **Interaction:**
* Hovering a region shows a tooltip: *"Chesapeake Bay: 78°F, SW 10-15kt"*.
* Clicking a region opens a **Region Briefing** side panel.



#### 3.2 Region Briefing (Side Panel)

A pre-generated report containing:

* **The "Pitch":** Why go here in this specific month? (e.g., *"In May, the Chesapeake enjoys reliable southerlies before the humid summer heat sets in."*)
* **Conditions:** Average Wind, Temp, Precip for that month.
* **Difficulty:** "Novice" (BVI) vs. "Expert" (Cape Horn).
* **"The Deep Cut Factor":** Why is this special? (e.g., *"Migration of stripers," "Annapolis Boat Show," "Empty anchorages"*).

---

### 4. Technical Architecture

Unlike the "Researcher" (which runs on-demand for a specific user trip), this system requires **Global Reference Data**. We cannot have an agent research the entire planet every time a user logs in.

**Strategy:** The "Commodore" Agent runs as a background batch process to populate a `sailing_regions` database.

#### 4.1 Database Schema Changes

We need a table to store defined regions and their monthly suitability data.

```sql
-- Known sailing regions (Global)
CREATE TABLE sailing_regions (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL, -- e.g. "Sea of Cortez", "Chesapeake Bay"
    geometry JSONB,                    -- GeoJSON Polygon of the area
    type VARCHAR(50)                   -- "Coastal", "Island Group", "Ocean Crossing"
);

-- Monthly suitability scores
CREATE TABLE region_seasonality (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region_id INTEGER REFERENCES sailing_regions(id),
    month INTEGER NOT NULL,            -- 1-12
    suitability_score INTEGER,         -- 0-100 (0 = Hurricane Season, 100 = Prime)
    is_hidden_gem BOOLEAN DEFAULT FALSE,
    summary TEXT,                      -- "Perfect trade winds, dry season."
    deep_cut_reasoning TEXT,           -- "While Caribbean is hot, this area is..."
    avg_wind_speed INTEGER,
    avg_temp_c INTEGER,
    UNIQUE(region_id, month)
);

```

#### 4.2 Agent Strategy: "The Miner"

We will create a new Agent profile (`discovery_agent`) responsible for mining this intelligence.

**Prompt Strategy for "Deep Cuts":**
The prompt must explicitly separate "Popular" from "Niche".

> **System Instruction:**
> "You are a World Cruising Commodore with decades of experience. You know the Pilot Charts by heart.
> **Task 1 (The Standards):** For the month of **{{Month}}**, identify the top 5 world-famous sailing destinations.
> **Task 2 (The Deep Cuts):** Identify 5 destinations that are **excellent** for sailing in {{Month}} but are **non-obvious** or **underrated**. Look for:
> * Shoulder seasons just before/after peak crowds (e.g., Med in late Sept/Oct).
> * High-latitude summers (e.g., Maine, Scotland in July/Aug).
> * Safe pockets during off-seasons (e.g., Grenada during hurricane season).
> 
> 
> **Output:** For each found region, provide a JSON object with:
> * `name`: Region Name
> * `coordinates`: Approximate bounding box
> * `why_now`: The technical reason (weather patterns, currents)
> * `vibe`: "Remote", "Party", "Technical", "Family Friendly"
> * `deep_cut_factor`: Why is this a hidden gem?"
> 
> 

#### 4.3 API Endpoints

**`GET /api/v1/discovery/regions`**

* **Query Params:** `?month=5`
* **Returns:** GeoJSON collection of regions valid for that month, including metadata (`is_hidden_gem`, `summary`) for the frontend map popups.

**`GET /api/v1/discovery/regions/{id}/details`**

* **Returns:** Full details for the side panel.

---

### 5. Implementation Plan

1. **Backend (Go):**
* Define `SailingRegion` and `RegionSeasonality` models.
* Create a "Seeder" CLI tool that uses the `discovery_agent` to populate the DB for all 12 months (run once/monthly).


2. **Frontend (Mapbox):**
* Add a "Discovery Layer" to the map.
* Implement the Month Slider component.
* Style polygons:
* `fill-opacity`: 0.3
* `line-color`: Based on `is_hidden_gem`.




3. **Data Mining (The First Run):**
* Run the agent for May to generate the initial "Chesapeake" data point to validate the "Deep Cut" logic.