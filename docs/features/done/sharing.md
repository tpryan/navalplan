Here is the Product Requirement Document (PRD) for the **Public Destination Reports & Map Snapshot** feature.

---

# PRD: Public Destination Reports & Map Snapshot

| Metadata | Details |
| --- | --- |
| **Project** | NavalPlan |
| **Feature** | Public Sharing & Map Capture |
| **Status** | Draft |
| **Target Release** | v1.1 |

## 1. Problem Statement

Captains using NavalPlan generate valuable "Voyage Guides" (agent-researched summaries, hazards, and itineraries) that they need to share with crew members or friends. Currently, viewing this data requires a NavalPlan account and login. Additionally, the map view is dynamic, meaning the "perfect view" of the route or destination context isn't preserved when shared or exported; it relies on the viewer resetting the map viewport.

## 2. Goals

1. **Frictionless Sharing:** Allow users to share a read-only version of their Voyage Guide via a unique, obfuscated URL (no login required for viewers).
2. **Context Preservation:** Implement a "Map Snapshot" feature where the Captain can capture the exact map zoom/center to serve as the static visual header for the public report.
3. **Security:** Ensure strictly read-only access for public viewers with all "Write", "Research", and "Edit" capabilities visually and functionally disabled.

## 3. User Stories

### 3.1 The Captain (Owner)

* **Enable Sharing:** As a Captain, I want to click a "Share" button on my voyage to generate a public link.
* **Capture Context:** As a Captain, before sharing, I want to pan and zoom the map to the perfect frame and click "Set Report Screenshot" so my crew sees exactly what I see.
* **Revoke Access:** As a Captain, I want to disable the link if I no longer want the information public.

### 3.2 The Guest (Public Viewer)

* **View Report:** As a Guest, I want to click the link and immediately see the Voyage Guide (Summary, Hazards, Itinerary) without registering.
* **Visual Context:** As a Guest, I want to see the static map image defined by the Captain at the top of the report.
* **No Accidental Edits:** As a Guest, I should not see any buttons to "Delete Stop", "Trigger Agent", or "Save Notes."

## 4. Technical Requirements

### 4.1 Backend (Go)

The existing `Voyage` model already supports `ShareToken` and `IsPublic`. We need to extend the public API surface to include the Voyage Guide.

* **New Endpoint:** `GET /api/v1/public/voyages/{token}/guide`
* **AuthLevel:** 0 (Public).
* **Logic:** Look up Voyage by token. If found, retrieve the associated `VoyageGuide`.
* **Response:** JSON containing the `VoyageGuide` data and the URL of the `VoyageMap` image.


* **Update Endpoint:** `POST /api/v1/voyages/{id}/guide/snapshot`
* **AuthLevel:** 1 (Owner Only).
* **Logic:** Accepts a raw image file (blob) captured from the frontend canvas. Saves it to `code/app/content/maps/voyage_{id}.png`. This replaces the existing `UploadVoyageMap` handler logic to specifically align with the "Screenshot" workflow.



### 4.2 Frontend (Vanilla JS + Vite)

* **Router Update:** Handle a new client-side route: `/shared/{token}`.
* **Read-Only Mode:**
* Create a global `state.isReadOnly` flag.
* **UI Logic:**
* If `isReadOnly` is true, hide: `Add Stop`, `Delete`, `Edit Notes`, `Research Area` buttons.
* Disable drag-and-drop on the map markers.
* Hide the "Settings/Profile" corner menu.




* **Map Capture Component (Owner Only):**
* Add a "Camera" icon to the Voyage Guide toolbar.
* **Action:** When clicked, use `map.getCanvas().toBlob()` (Mapbox API) to generate an image of the current viewport.
* **Upload:** POST this blob to the new snapshot endpoint.
* **Feedback:** Show a flash message "Report Cover Image Set."



## 5. User Interface (UI) Design

### 5.1 Owner View (Authenticated)

* **Share Modal:**
* Toggle Switch: "Public Link Enabled".
* Input Field: Read-only text box with the URL (e.g., `navalplan.com/shared/abc-123...`) + "Copy" button.
* **New Section:** "Report Preview Image".
* Button: "Update Snapshot from Current Map View".
* Thumbnail: Shows the current saved image (if any).





### 5.2 Public View (Unauthenticated)

* **Layout:** Single column "Article" layout.
* **Header:**
* **Hero Image:** The specific "Map Snapshot" captured by the Captain (not an interactive map).
* **Title:** Voyage Title + "Captain's Report".


* **Content:**
* **Summary Card:** Agent-generated summary.
* **Hazards:** Red warning block (if hazards exist).
* **Itinerary:** Simple timeline list (Date - Location).
* *Note: The interactive map is replaced by the static snapshot to ensure performance and specific framing.*



## 6. Implementation Plan

### Step 1: Backend API Extension

1. Modify `code/app/backend/server/routes.go` to add the public guide route.
2. Update `code/app/backend/server/handlers/guide.go` to implement `GetPublicVoyageGuide` (reusing logic from `GetVoyageGuide` but resolving via Token instead of ID).

### Step 2: Frontend "Read-Only" State

1. Update `code/app/frontend/js/state.js` to include `viewMode: 'owner' | 'public'`.
2. Refactor `ui.js` to conditionally render action buttons based on `viewMode`.

### Step 3: Map Capture Feature

1. Implement the `captureMapState()` function in `map.js` using `mapboxgl` canvas export.
2. Wire up the upload to `POST /api/v1/voyages/{id}/guide/map_image`.

### Step 4: Public Page Entry Point

1. Create `code/app/frontend/shared.html` (or handle via routing in `index.html`).
2. On load, fetch data from `/api/v1/public/voyages/{token}/guide`.
3. Render the static report view.

## 7. Security Considerations

* **Token Entropy:** Ensure `generateToken()` uses sufficiently random bytes (crypto/rand) to prevent enumeration.
* **Data Leakage:** The `GetPublicVoyageGuide` endpoint must **only** return the Guide fields and basic Stop info. It must **not** return the Captain's email, Google ID, or internal User ID.
* **CORS:** Ensure public endpoints are accessible if the domain differs (though likely same-origin for this release).