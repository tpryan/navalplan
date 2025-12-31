### **Product Requirement Document: Voyage Destination Intelligence**

#### **1. Overview**

Users need high-level "local knowledge" before planning specific daily stops. This feature introduces a "Destination Guide" that provides general nautical intelligence for the voyage's region.

#### **2. Goals**

* Provide seasonal context (Sailing season vs. Hurricane/Storm season).
* Identify regional anomalies (e.g., "Strong currents in the narrow channels," "katabatic winds").
* List major nautical hubs (primary ports of entry, marina clusters).
* Identify charter availability (useful for users planning fly-in voyages).

#### **3. Architecture Changes**

We will add a `VoyageGuide` entity linked to the `Voyage`. This is similar to the existing `Briefing` entity but attached to the parent trip rather than a child stop.

---

### **Technical Specification & Implementation**

#### **Step 1: Database Schema Changes**

We need a new table to store this high-level research. It will use `JSONB` columns to store structured data returned by the agent, similar to your existing `briefing` table.

**File:** `app/db/schema.sql` (Add this)

```sql
CREATE TABLE voyage_guide (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE UNIQUE,
    summary TEXT,
    sailing_season JSONB,   -- { "best_months": [], "storm_season": [], "notes": "" }
    hazards JSONB,          -- [ { "name": "Gulf Stream", "description": "..." } ]
    hubs JSONB,             -- [ { "name": "Nanny Cay", "type": "Marina Hub" } ]
    charter_info JSONB,     -- { "available": true, "major_companies": [] }
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_voyage_guide_voyage_id ON voyage_guide(voyage_id);

```

#### **Step 2: Backend Models**

Update your Go models to support the new table.

**File:** `app/backend/models/models.go`

```go
package models

import "time"

// ... existing structs ...

type VoyageGuide struct {
	ID            int64     `json:"id" db:"id"`
	VoyageID      int64     `json:"voyage_id" db:"voyage_id"`
	Summary       string    `json:"summary" db:"summary"`
	SailingSeason RawJSON   `json:"sailing_season" db:"sailing_season"`
	Hazards       RawJSON   `json:"hazards" db:"hazards"`
	Hubs          RawJSON   `json:"hubs" db:"hubs"`
	CharterInfo   RawJSON   `json:"charter_info" db:"charter_info"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

```

#### **Step 3: Agent Implementation**

You need a new agent configuration. Since you are using the `google.golang.org/adk` framework, you can add a new agent definition (e.g., `guide_agent`) alongside your existing `researcher_agent` inside `services/researcher/main.go`.

**New System Prompt for "Destination Guide Agent":**

> **Role:** You are a Local Knowledge Expert and Sailing Guide.
> **Task:** Research the general sailing region for the location: "{{LocationName}}".
> **Output:** Produce a JSON object strictly following this schema:
> ```json
> {
>   "summary": "A 2-3 sentence overview of sailing in this region.",
>   "sailing_season": {
>     "primary_season_months": ["November", "December", ...],
>     "storm_season_months": ["August", "September"],
>     "storm_risk_level": "High/Medium/Low",
>     "notes": "Hurricane season peaks in Sept."
>   },
>   "hazards": [
>     { "title": "Coral Heads", "description": "Numerous uncharted coral heads inside the reef." },
>     { "title": "Christmas Winds", "description": "Strong trade winds (25-30kt) common in Dec/Jan." }
>   ],
>   "hubs": [
>     { "name": "Road Town", "description": "Major provisioning and charter hub." }
>   ],
>   "charter_info": {
>      "is_charter_destination": true,
>      "companies": ["Moorings", "Dream Yacht"]
>   }
> }
> 
> ```
> 
> 
> **Tools:** Use Google Search to answer these specific questions:
> 1. "Sailing season months for [Location]"
> 2. "Hurricane season [Location]"
> 3. "Sailing hazards and anomalies [Location]"
> 4. "Major marinas and sailing hubs [Location]"
> 5. "Yacht charter companies [Location]"
> 
> 

#### **Step 4: API Expansion (OpenAPI)**

Add endpoints to trigger this research and retrieve the results. This follows the pattern of your existing `stops/{id}/research` endpoints.

**File:** `docs/specs/openapi.yaml`

```yaml
paths:
  # ... existing paths ...
  
  # New Endpoint to trigger the guide research
  /api/v1/voyages/{id}/research_guide:
    post:
      tags: [Research]
      summary: Generate the Destination Guide for the voyage
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        '202':
          description: Research Started

  # New Endpoint to retrieve the result
  /api/v1/voyages/{id}/guide:
    get:
      tags: [Voyage]
      summary: Get the Destination Guide
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        '200':
          description: Guide Data
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/VoyageGuide'
        '404':
          description: Guide not found (not researched yet)

components:
  schemas:
    # ... existing schemas ...
    VoyageGuide:
      type: object
      properties:
        id:
          type: integer
        summary:
          type: string
        sailing_season:
          type: object
          properties:
            primary_season_months:
              type: array
              items:
                type: string
            storm_season_months:
              type: array
              items:
                type: string
            notes:
              type: string
        hazards:
          type: array
          items:
            type: object
            properties:
              title: 
                type: string
              description: 
                type: string
        hubs:
          type: array
          items:
            type: object
            properties:
              name: 
                type: string
              description: 
                type: string
        charter_info:
          type: object
          properties:
            is_charter_destination:
              type: boolean
            companies:
              type: array
              items:
                type: string

```

#### **5. UI/UX Suggestions**

* **Location:** Place this on the **Voyage Dashboard** (the top-level view before clicking into specific stops).
* **Visuals:**
* Use a timeline bar to visualize "Best Season" vs. "Storm Season" relative to the current Voyage Dates. If the voyage dates overlap with "Storm Season," display a warning badge.
* List "Major Hubs" as clickable chips that could potentiality be added as a Stop.