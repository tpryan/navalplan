Here is a comprehensive technical plan to migrate the NavalPlan application from Mapbox to Google Maps.

### **Migration Strategy: Mapbox GL JS to Google Maps JavaScript API**

**Objective:** Replace the Mapbox dependency with Google Maps while preserving voyage planning, route visualization, facility markers, and the "Discovery" mode polygon layers.

**Prerequisites:**

1. **Google Cloud Project:** Enable **Maps JavaScript API** and **Geocoding API**.
2. **API Key:** Generate an API Key restricted to the application's domain.

### **Troubleshooting API Errors**

**Issue:** `REQUEST_DENIED`
**Cause:** This error typically indicates that the API key is restricted or the specific API service is not enabled.
**Resolution:**
1.  **Enable Geocoding API:** Go to the Google Cloud Console > APIs & Services > Library and ensure the "Geocoding API" is enabled for your project.
2.  **Check API Key Restrictions:**
    *   If using an API key for the **Backend** (server-side), ensure it has **NO** "HTTP referrers" restrictions. Server-side requests do not send a referrer. You can restrict it by IP address if needed.
    *   If using an API key for the **Frontend** (client-side), "HTTP referrers" restrictions are appropriate (e.g., `localhost:8080`, `yourdomain.com`).
    *   **Recommendation:** Create two separate API keys: one for the Frontend (restricted by Referrer) and one for the Backend (restricted by IP or unrestricted for dev).

---

### **Phase 1: Backend & Configuration Updates**

**Target File:** `app/backend/config/config.go`
**Action:** Replace Mapbox configuration with Google Maps configuration.

1. **Update Struct:**
* Remove `MapboxToken`.
* Add `GoogleMapsAPIKey`.


2. **Environment Variables:**
* Ensure the backend injects the `Maps_API_KEY` into the frontend build or runtime configuration (replacing `__MAPBOX_TOKEN__` injection in `main.js`).



---

### **Phase 2: Frontend Dependencies & Loading**

**Target File:** `app/frontend/index.html`
**Action:** Remove Mapbox resources.

1. **Remove:** Links to `mapbox-gl.css` and the preconnects to `api.mapbox.com`.
2. **Add:** No strict need to add a script tag here if using dynamic loading in JS, but ensure `Material Symbols` (already present) remains, as it will be used for markers.

**Target File:** `app/frontend/js/main.js`
**Action:** Switch library loaders.

1. **Remove:** `import('mapbox-gl')`.
2. **Add:** Use the `@googlemaps/js-api-loader` (recommended) or a direct dynamic script injection function to load the Google Maps API.
* *Libraries to load:* `['places', 'geometry', 'marker']`. (Marker library is required for `AdvancedMarkerElement`).


3. **Update Configuration:** Replace `MAPBOX_TOKEN` constant with `Maps_API_KEY`.

---

### **Phase 3: Core Map Implementation**

**Target File:** `app/frontend/js/main.js`

#### 1. Map Initialization (`initMap`)

* **Mapbox:** `new mapboxgl.Map({ style: ..., center: ... })`
* **Google Maps:**
```javascript
const { Map } = await google.maps.importLibrary("maps");
map = new Map(document.getElementById("map-container"), {
  center: { lat: 39.8283, lng: -98.5795 },
  zoom: 3,
  mapId: "DEMO_MAP_ID", // Required for AdvancedMarkerElement, even if just "DEMO_MAP_ID"
  disableDefaultUI: false, // Keep navigation controls
  clickableIcons: false // Prevent clicking generic POIs interfering with route planning
});

```



#### 2. Cleaning the Map (`clearMap`)

* **Logic:** Iterate through the `markers` array and call `.map = null` (or `.setMap(null)`) on them.
* **Polylines:** Store the route Polyline in a variable and call `.setMap(null)`.
* **Data Layer:** Use `map.data.forEach` to remove features for the discovery/facilities layers.

---

### **Phase 4: Markers & Routing (Itinerary)**

**Target File:** `app/frontend/js/main.js` -> `renderMapStops`

#### 1. Numbered Stop Markers

* **Current:** Uses `mapboxgl.Marker(el)` with a custom DIV.
* **Migration:** Use `google.maps.marker.AdvancedMarkerElement`.
* Create the DOM element (the existing code creating `<div class="marker">...</div>` is reusable).
* Pass this element to the `content` property of `AdvancedMarkerElement`.
* **Click Handler:** Attach the click listener to the `AdvancedMarkerElement`.



#### 2. Route Lines (Polylines)

* **Current:** GeoJSON Source -> Line Layer.
* **Migration:** Use `google.maps.Polyline`.
* **Coordinates:** Map `currentStops` to `[{lat: x, lng: y}, ...]`.
* **Style:**
```javascript
new google.maps.Polyline({
  path: coordinates,
  geodesic: true,
  strokeColor: "#314c3b", // Brand Green
  strokeOpacity: 0, // Hide solid line
  icons: [{ // Create the dashed effect
    icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
    offset: '0',
    repeat: '20px'
  }],
  map: map
});

```





#### 3. Facilities (Icons)

* **Current:** GeoJSON Source -> Symbol Layer (using Mapbox sprite sheet).
* **Migration:** Use `AdvancedMarkerElement` with Material Symbols.
* Iterate through facilities.
* Create a `div` containing the `<span class="material-symbols-outlined">...</span>` corresponding to the facility type (anchor, storefront, etc.).
* Use this div as the `content` for the marker.
* **Popups:** Use `google.maps.InfoWindow` attached to the marker click event.



---

### **Phase 5: Interactions (Click-to-Plan)**

**Target File:** `app/frontend/js/main.js` -> `initMap` (click listener)

1. **Event Listener:** Change `map.on('click', ...)` to `map.addListener('click', (e) => { ... })`.
2. **Coordinates:** Access `e.latLng.lat()` and `e.latLng.lng()`.
3. **Reverse Geocoding (Crucial Change):**
* **Mapbox:** The code currently queries rendered features to find a label name (`queryRenderedFeatures`). Google Maps does not allow querying text labels from the base map tiles.
* **Solution:** You **must** call the **Google Geocoding API** inside the click handler to get the location name.


```javascript
const geocoder = new google.maps.Geocoder();
const response = await geocoder.geocode({ location: e.latLng });
const locationName = response.results[0]?.formatted_address || "Unknown Location";
// Proceed to create/update stop...

```



---

### **Phase 6: Discovery Mode (Polygons)**

**Target File:** `app/frontend/js/main.js` -> `renderDiscoveryLayer`

1. **Logic:** Replace Mapbox Sources/Layers with `map.data`.
2. **Implementation:**
* `map.data.addGeoJson(geojsonObject)`.
* **Styling:** Use `map.data.setStyle((feature) => { ... })`. Map the `tier` property (Hidden Gem, Regional, etc.) to `fillColor` and `strokeColor` options within the style function.


3. **Events:**
* `map.data.addListener('click', (event) => { ... })`.
* Access properties via `event.feature.getProperty('id')`.



---

### **Phase 7: Map Snapshots (High Risk)**

**Target File:** `app/frontend/js/main.js` -> `captureAndUploadMap`

* **Current:** `map.getCanvas().toBlob(...)` (Works natively in Mapbox/WebGL).
* **Problem:** Google Maps is DOM-based (mostly) and CORS restrictions often block `html2canvas` or `toDataURL` on the map container.
* **Solution (Google Static Maps API):**
* Instead of screenshotting the live map, construct a **Google Static Maps URL**.
* Include path parameters (encodable polylines) and marker parameters to match the voyage.
* Fetch this blob in JS (`fetch(staticMapUrl).then(r => r.blob())`).
* Pass this blob to the existing `API.uploadVoyageMap` function.
* *Note:* This ensures high-quality images for the reports without browser rendering quirks.



---

### **Summary of Library Changes**

| Feature | Mapbox GL JS Implementation | Google Maps JS API Implementation |
| --- | --- | --- |
| **Loader** | `import('mapbox-gl')` | `@googlemaps/js-api-loader` |
| **Styles** | Custom URI (Mapbox Studio) | Standard Google Map (or JSON styles) |
| **Stops** | `mapboxgl.Marker(el)` | `google.maps.marker.AdvancedMarkerElement({content: el})` |
| **Route** | GeoJSON Source + Line Layer | `google.maps.Polyline` |
| **Facilities** | GeoJSON + Symbol Layer | Loop -> `AdvancedMarkerElement` (w/ Material Icons) |
| **Regions** | GeoJSON + Fill Layer | `map.data.addGeoJson` + `map.data.setStyle` |
| **Click Info** | `queryRenderedFeatures` | `google.maps.Geocoder` (API Call) |
| **Snapshot** | `canvas.toBlob` | Fetch **Google Static Maps API** URL as Blob |