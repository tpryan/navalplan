import DOMPurify from 'dompurify';
import { API, API_BASE, set503Callback } from './api.js';
import { checkSession, currentUser } from './auth.js';
import { Ticker } from './ticker.js';
import { importLibrary, setOptions } from "@googlemaps/js-api-loader";

const GOOGLE_MAPS_API_KEY = __GOOGLE_MAPS_API_KEY__;
setOptions({
  key: GOOGLE_MAPS_API_KEY,
  version: "weekly",
});

// Dynamic library loading
let googleMapsLib = null;
async function loadGoogleMaps() {
    if (googleMapsLib) return googleMapsLib;
    googleMapsLib = await importLibrary("maps");
    return googleMapsLib;
}

let ChartLib = null;
async function loadChart() {
    if (ChartLib) return ChartLib;
    ChartLib = (await import('chart.js/auto')).default;
    return ChartLib;
}

// Start loading large libraries immediately
loadGoogleMaps();
loadChart();

// Configuration

// Consistent marker colors across all map views
const MARKER_COLORS = {
    anchorage:   '#388E3C', // green
    mooring:     '#7B1FA2', // purple
    marina:      '#E65100', // orange
    hub:         '#E65100', // orange (resource hubs treated same as marina)
    'yacht club':'#E65100', // orange
    bar:         '#F9A825', // yellow
    restaurant:  '#5D4037', // brown
    default:     '#455A64', // blue-gray
};

function markerColor(type) {
    if (!type) return MARKER_COLORS.default;
    const t = type.toLowerCase();
    if (t.includes('anchor'))    return MARKER_COLORS.anchorage;
    if (t.includes('moor'))      return MARKER_COLORS.mooring;
    if (t.includes('marina'))    return MARKER_COLORS.marina;
    if (t.includes('hub'))       return MARKER_COLORS.hub;
    if (t.includes('yacht'))     return MARKER_COLORS['yacht club'];
    if (t.includes('restaurant'))return MARKER_COLORS.restaurant;
    if (t.includes('bar'))       return MARKER_COLORS.bar;
    return MARKER_COLORS.default;
}

// State
let researchTicker = null;
let voyages = [];
let currentVoyage = null;
let currentStops = [];
let selectedDate = null;
let map = null;
let markers = [];
let routePolyline = null;
let facilityMarkers = []; // each entry: { marker: AdvancedMarkerElement, type: string }
const activeFacilityFilters = new Set(['anchorage', 'marina', 'mooring', 'bar', 'restaurant', 'other']);
let recommendationMarkers = []; // each entry: { marker: AdvancedMarkerElement, type: string }
const activeFilters = new Set(['anchorage', 'mooring', 'hub']);
let voyageRecommendations = [];
let pilotCircle = null;
let pilotCenterMarker = null;
let pilotRadiusMarker = null;
let isPilotResearching = false;
let radarSweep = null;
let searchRing = null;
// Module-level handles for pilot SSE streams and poll so clearRecommendations() can tear them down.
let _pilotEventSource = null;
let _pilotProgressES = null;
let _pilotPoll = null;
// Module-level handles for other long-running research polls cleared on voyage exit.
let _fullResPoll = null;
let _fullResProgressES = null;
let _guidePoll = null;
let _guideProgressES = null;
let _stopPoll = null;
let _stopProgressES = null;

// createRadarSweepOverlay — factory that returns a RadarSweep class once
// google.maps.OverlayView is available (cannot extend it at parse time).
function createRadarSweepOverlay(OverlayView) {
    return class RadarSweep extends OverlayView {
        constructor(map, center, radiusMeters) {
            super();
            this._map = map;
            this._center = center;
            this._radiusMeters = radiusMeters;
            this._element = null;
            this._active = false;
            this.setMap(map);
        }

        onAdd() {
            const div = document.createElement('div');
            div.className = 'radar-sweep-container';
            
            // Add internal structure
            div.innerHTML = `
                <div class="radar__circle radar__circle_outer"></div>
                <div class="radar__circle radar__circle_inner"></div>
                <div class="radar__beam"></div>
            `;
            
            this._element = div;
            const panes = this.getPanes();
            panes.overlayLayer.appendChild(div);
            this._active = true;
        }

        draw() {
            if (!this._element || !this._active) return;
            const proj = this.getProjection();
            if (!proj) return;

            const latVal = typeof this._center.lat === 'function' ? this._center.lat() : this._center.lat;
            const lngVal = typeof this._center.lng === 'function' ? this._center.lng() : this._center.lng;
            const centerPx = proj.fromLatLngToDivPixel({ lat: latVal, lng: lngVal });

            const metersPerPx = 156543.03392 * Math.cos(latVal * Math.PI / 180) / Math.pow(2, this._map.getZoom());
            const radiusPx = this._radiusMeters / metersPerPx;
            const size = Math.ceil(radiusPx * 2);

            this._element.style.width = `${size}px`;
            this._element.style.height = `${size}px`;
            this._element.style.left = `${Math.round(centerPx.x - size / 2)}px`;
            this._element.style.top = `${Math.round(centerPx.y - size / 2)}px`;
        }

        onRemove() {
            this._active = false;
            if (this._element && this._element.parentNode) {
                this._element.parentNode.removeChild(this._element);
            }
            this._element = null;
        }

        stop() {
            this.setMap(null);
        }
    };
}
// Cached RadarSweep class — resolved once after Maps library loads.
let _RadarSweepClass = null;
async function getRadarSweepClass() {
    if (_RadarSweepClass) return _RadarSweepClass;
    const { OverlayView } = await importLibrary("maps");
    _RadarSweepClass = createRadarSweepOverlay(OverlayView);
    return _RadarSweepClass;
}

// createSearchRingOverlay — animated ring that draws and erases itself at the voyage search radius.
function createSearchRingOverlay(OverlayView) {
    return class SearchRing extends OverlayView {
        constructor(map, center, radiusMeters) {
            super();
            this._map = map;
            this._center = center;
            this._radiusMeters = radiusMeters;
            this._element = null;
            this._active = false;
            this.setMap(map);
        }

        onAdd() {
            const div = document.createElement('div');
            div.className = 'search-ring-container';
            div.innerHTML = `
                <div class="search-ring__pulse"></div>
                <div class="search-ring__circle"></div>
                <div class="search-ring__circle" style="inset: 25%"></div>
            `;
            this._element = div;
            this.getPanes().overlayLayer.appendChild(div);
            this._active = true;
        }

        draw() {
            if (!this._element || !this._active) return;
            const proj = this.getProjection();
            if (!proj) return;

            const latVal = typeof this._center.lat === 'function' ? this._center.lat() : this._center.lat;
            const lngVal = typeof this._center.lng === 'function' ? this._center.lng() : this._center.lng;
            const centerPx = proj.fromLatLngToDivPixel({ lat: latVal, lng: lngVal });

            const metersPerPx = 156543.03392 * Math.cos(latVal * Math.PI / 180) / Math.pow(2, this._map.getZoom());
            const radiusPx = this._radiusMeters / metersPerPx;
            const size = Math.ceil(radiusPx * 2);

            this._element.style.width = `${size}px`;
            this._element.style.height = `${size}px`;
            this._element.style.left = `${Math.round(centerPx.x - size / 2)}px`;
            this._element.style.top  = `${Math.round(centerPx.y - size / 2)}px`;
        }

        onRemove() {
            this._active = false;
            if (this._element && this._element.parentNode) {
                this._element.parentNode.removeChild(this._element);
            }
            this._element = null;
        }

        stop() { this.setMap(null); }
    };
}

let _SearchRingClass = null;
async function getSearchRingClass() {
    if (_SearchRingClass) return _SearchRingClass;
    const { OverlayView } = await importLibrary("maps");
    _SearchRingClass = createSearchRingOverlay(OverlayView);
    return _SearchRingClass;
}

// Stop sweep sequencer state
const STOP_SWEEP_RADIUS_M = 1852; // 1 nautical mile
let stopSweepTimer = null;
let activeStopSweepInstance = null;
let activeStopSweepStop = null; // stop object currently being swept
let stopSweepQueue = []; // pending stop objects (not yet researched)
let stopSweepCursor = 0;
let _routeLineAnimInterval = null;


async function advanceStopSweep() {
    if (!map || stopSweepQueue.length === 0) return;
    if (activeStopSweepInstance) { activeStopSweepInstance.stop(); activeStopSweepInstance = null; }

    const stop = stopSweepQueue[stopSweepCursor % stopSweepQueue.length];
    stopSweepCursor++;
    activeStopSweepStop = stop;

    const Cls = await getRadarSweepClass();
    activeStopSweepInstance = new Cls(map, { lat: stop.latitude, lng: stop.longitude }, STOP_SWEEP_RADIUS_M);
}

async function startStopSweepSequence(stops) {
    clearStopSweeps();
    stopSweepQueue = [...stops];
    stopSweepCursor = 0;
    // Hide all stop markers for the duration of the sweep sequence
    markers.forEach(m => m.map = null);
    await advanceStopSweep();
    // Only cycle if there are multiple stops; a single stop runs continuously.
    if (stops.length > 1) {
        stopSweepTimer = setInterval(advanceStopSweep, 3000);
    }

    // Show the animated outer ring around the full voyage search area
    if (map && currentVoyage && currentVoyage.latitude != null && currentVoyage.longitude != null) {
        if (searchRing) { searchRing.stop(); searchRing = null; }
        const radiusMeters = (currentVoyage.search_radius || 60) * 1852;
        const SearchRingClass = await getSearchRingClass();
        searchRing = new SearchRingClass(
            map,
            { lat: currentVoyage.latitude, lng: currentVoyage.longitude },
            radiusMeters
        );
    }

    // Animate the route line (marching ants)
    if (routePolyline) {
        let iconOffset = 0;
        if (_routeLineAnimInterval) clearInterval(_routeLineAnimInterval);
        _routeLineAnimInterval = setInterval(() => {
            iconOffset = (iconOffset + 1) % 20;
            if (routePolyline) {
                routePolyline.set('icons', [{
                    icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
                    offset: iconOffset + 'px',
                    repeat: '20px'
                }]);
            }
        }, 50);
    }
}

function removeStopFromSweepQueue(stopId) {
    stopSweepQueue = stopSweepQueue.filter(s => s.id !== stopId);
    if (stopSweepQueue.length === 0) {
        clearStopSweeps();
    } else if (stopSweepQueue.length === 1 && stopSweepTimer) {
        // Stop cycling — one stop left, let it run continuously
        clearInterval(stopSweepTimer);
        stopSweepTimer = null;
    }
}

function clearStopSweeps() {
    if (stopSweepTimer) { clearInterval(stopSweepTimer); stopSweepTimer = null; }
    if (activeStopSweepInstance) { activeStopSweepInstance.stop(); activeStopSweepInstance = null; }
    if (searchRing) { searchRing.stop(); searchRing = null; }
    if (_routeLineAnimInterval) {
        clearInterval(_routeLineAnimInterval);
        _routeLineAnimInterval = null;
        // Reset line to static dashes
        if (routePolyline) {
            routePolyline.set('icons', [{
                icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
                offset: '0',
                repeat: '20px'
            }]);
        }
    }
    activeStopSweepStop = null;
    // Restore all stop markers
    markers.forEach(m => m.map = map);
    stopSweepQueue = [];
    stopSweepCursor = 0;
}

let isResearchAllRunning = false;
let activeInfoWindow = null;
let lastKnownItineraryFull = false;
let lastKnownResearchDone = false;
let healthCheckInterval = null;
// Chart.js instance registry — keyed by canvas ID so we can destroy before re-render.
const chartInstances = new Map();
let editingVoyageId = null;
let currentMode = 'planner'; // 'planner' or 'discovery'
let discoveryRegions = [];

// Research Area Sync State
let syncTimeout;
const syncToBackend = (force = false) => {
    return new Promise((resolve, reject) => {
        clearTimeout(syncTimeout);
        const runSync = async () => {
            if (!pilotCircle) return resolve();
            const c = pilotCircle.getCenter();
            const rMeters = pilotCircle.getRadius();
            const rNm = Math.round(rMeters / 1852);
            
            try {
                // Update local state first for immediate UI responsiveness
                currentVoyage.latitude = c.lat();
                currentVoyage.longitude = c.lng();
                currentVoyage.search_radius = rNm;
                currentVoyage.search_radius_unit = currentVoyage.search_radius_unit || 'nm';
                
                const updated = await API.updateVoyage(currentVoyage.id, {
                    ...currentVoyage,
                    latitude: c.lat(),
                    longitude: c.lng(),
                    search_radius: rNm,
                    search_radius_unit: currentVoyage.search_radius_unit
                });
                currentVoyage = updated;
                console.log("NavalPlan: Voyage research area synced to DB:", rNm, "nm");
                resolve(updated);
            } catch (err) {
                console.error("NavalPlan: Failed to sync voyage research area:", err);
                reject(err);
            }
        };

        if (force) {
            runSync();
        } else {
            syncTimeout = setTimeout(runSync, 1000);
        }
    });
};

// Pagination
let currentVoyagePage = 1;
const VOYAGE_PAGE_LIMIT = 20;
let currentStopPage = 1;
const STOP_PAGE_LIMIT = 50;

/**
 * Strips Plus Codes (e.g. "82GQ+6Q ") from location names for cleaner UI display
 */
function displayLocationName(name) {
    if (!name) return "";
    // Regular expression to match Plus Codes at the start of the string
    // Matches 4-8 alphanumeric chars + '+' + 2-3 alphanumeric chars followed by space
    return name.replace(/^[A-Z0-9]{4,8}\+[A-Z0-9]{2,3}\s*/i, "").trim();
}

/**
 * Ensures the input is an array, handling the wrapped {"recommendations": [...]} format
 */
function ensureRecommendationsArray(data) {
    if (!data) return [];
    if (Array.isArray(data)) return data;
    if (data.recommendations && Array.isArray(data.recommendations)) return data.recommendations;
    return [];
}

/**
 * Smoothing algorithm for polygons (Chaikin's)
 */
function smoothPolygon(coordinates, iterations = 2) {
    if (!coordinates || coordinates.length < 3) return coordinates;
    
    let result = coordinates;
    for (let i = 0; i < iterations; i++) {
        result = chaikin(result);
    }
    return result;
}

/**
 * Generate a circular polygon from a center point and radius in miles
 * with organic jitter to make it look like a 'blob'.
 */
function getCirclePolygon(center, radiusMiles, numPoints = 24, jitter = 0.3, seed = 0) {
    const coords = [];
    const R = 3958.8; // Earth's radius in miles
    const lat1 = (center.lat * Math.PI) / 180;
    const lon1 = (center.lng * Math.PI) / 180;

    // Deterministic pseudo-random based on seed + index
    const getJitter = (i) => {
        const val = Math.sin(seed + i) * 10000;
        return val - Math.floor(val);
    };

    for (let i = 0; i < numPoints; i++) {
        const brng = (2 * Math.PI * i) / numPoints;
        
        // Random factor between (1-jitter) and (1+jitter)
        const randomFactor = (1 - jitter) + (getJitter(i) * jitter * 2);
        const d = (radiusMiles * randomFactor) / R;

        const lat2 = Math.asin(
            Math.sin(lat1) * Math.cos(d) +
            Math.cos(lat1) * Math.sin(d) * Math.cos(brng)
        );
        const lon2 =
            lon1 +
            Math.atan2(
                Math.sin(brng) * Math.sin(d) * Math.cos(lat1),
                Math.cos(d) - Math.sin(lat1) * Math.sin(lat2)
            );
        coords.push([ (lon2 * 180) / Math.PI, (lat2 * 180) / Math.PI ]);
    }
    // Close the loop
    if (coords.length > 0) {
        coords.push([coords[0][0], coords[0][1]]);
    }
    return coords;
}

function hashString(str) {
    let hash = 0;
    if (!str) return hash;
    for (let i = 0; i < str.length; i++) {
        hash = ((hash << 5) - hash) + str.charCodeAt(i);
        hash |= 0;
    }
    return hash;
}

function chaikin(coords) {
    if (!coords || coords.length < 2) return coords;
    const newCoords = [];
    // Handle the closed loop: if last point == first point, we smooth across it
    const isClosed = coords[0][0] === coords[coords.length-1][0] && coords[0][1] === coords[coords.length-1][1];
    
    for (let i = 0; i < coords.length - 1; i++) {
        const p0 = coords[i];
        const p1 = coords[i + 1];
        
        const q = [
            0.75 * p0[0] + 0.25 * p1[0],
            0.75 * p0[1] + 0.25 * p1[1]
        ];
        const r = [
            0.25 * p0[0] + 0.75 * p1[0],
            0.25 * p0[1] + 0.75 * p1[1]
        ];
        
        newCoords.push(q);
        newCoords.push(r);
    }
    
    if (isClosed) {
        // Explicitly close the loop with the exact first point
        newCoords.push([newCoords[0][0], newCoords[0][1]]);
    } else {
        // If not closed, keep endpoints (less ideal for smoothing)
        newCoords.unshift(coords[0]);
        newCoords.push(coords[coords.length-1]);
    }
    
    return newCoords;
}

let last503Alert = 0;
document.addEventListener('DOMContentLoaded', () => {
  set503Callback((count) => {
    const now = Date.now();
    if (count >= 2 && now - last503Alert > 30000) {
      last503Alert = now;
      showNotification('High Demand', 'Our AI models are currently experiencing high demand. Some research tasks may take longer or require a retry. We recommend waiting a few minutes if errors persist.');
    }
  });
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  
  // Shared/Public View Handler
  if (window.location.pathname.startsWith('/shared/')) {
      const token = window.location.pathname.replace('/shared/', '');
      if (token) {
          initSharedMode(token);
          return;
      }
  }

  checkSession();
  initMap();
  initUI();
  initAdminUI();
  initOnboarding();
  loadVoyages();
  startHealthCheck();
}

function startHealthCheck() {
    const warning = document.getElementById('health-warning');
    if (!warning) return;

    const check = async () => {
        const result = await API.checkHealth();
        if (result.ok) {
            warning.classList.add('hidden');
        } else {
            // Differentiate text
            let text = 'Backend Connection Lost';
            if (result.status === 503 && result.message.includes('Agent')) {
                text = 'Agent Service Unavailable';
            }
            
            warning.innerHTML = `<span class="material-symbols-outlined">warning</span> ${text}`;
            warning.classList.remove('hidden');
        }
    };

    // Check every 10 seconds; clear any previous interval first
    if (healthCheckInterval) clearInterval(healthCheckInterval);
    healthCheckInterval = setInterval(check, 10000);
    // Initial check
    check();
}

/**
 * reverseGeocode — shared helper that converts a lat/lng to a human-readable
 * location name and plus-code precise location.
 * Returns { locationName, preciseLocation }.
 */
async function reverseGeocode(latLng) {
    const { lat, lng } = typeof latLng.lat === 'function'
        ? { lat: latLng.lat(), lng: latLng.lng() }
        : latLng;

    const { Geocoder } = await importLibrary("geocoding");
    const geocoder = new Geocoder();
    const response = await geocoder.geocode({ location: { lat, lng } });
    const r = response.results[0];
    if (!r) return { locationName: `${lat.toFixed(3)}, ${lng.toFixed(3)}`, preciseLocation: '' };

    const getComp = (type) => r.address_components.find(c => c.types.includes(type))?.long_name;
    const locality = getComp('locality') || getComp('sublocality');
    const region = getComp('administrative_area_level_1');
    const country = getComp('country');

    let locationName = r.formatted_address;
    if (locality && country) {
        locationName = region ? `${locality}, ${region}, ${country}` : `${locality}, ${country}`;
    } else if (region && country) {
        locationName = `${region}, ${country}`;
    } else if (country) {
        locationName = country;
    }

    let preciseLocation = '';
    if (r.plus_code) {
        preciseLocation = r.plus_code.compound_code || r.plus_code.global_code || '';
    }

    return { locationName, preciseLocation };
}

function initUI() {
    researchTicker = new Ticker('research-ticker');
    initPilotAndFilterListeners();
    initDiscoveryListeners();
    initMobileMenuListeners();
    initVoyageModalListeners();
    initNavigationListeners();
    initModalCloseListeners();
}

function initPilotAndFilterListeners() {
    const btnSetDates = document.getElementById('btn-set-dates');
    if (btnSetDates) {
        btnSetDates.addEventListener('click', () => { if (currentVoyage) openEditModal(currentVoyage); });
    }

    const btnPilotSuggestions = document.getElementById('btn-pilot-suggestions');
    if (btnPilotSuggestions) {
        btnPilotSuggestions.addEventListener('click', () => handlePilotSuggestionsClick());
    }

    // Pilot recommendation filter buttons
    document.querySelectorAll('.pilot-filter-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const type = btn.dataset.type;
            if (activeFilters.has(type)) {
                activeFilters.delete(type);
                btn.classList.remove('active');
            } else {
                activeFilters.add(type);
                btn.classList.add('active');
            }
            recommendationMarkers.forEach(({ marker, type: markerType }) => {
                marker.map = activeFilters.has(markerType) ? map : null;
            });
        });
    });

    // Facility filter buttons
    document.querySelectorAll('.facility-filter-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const type = btn.dataset.type;
            if (activeFacilityFilters.has(type)) {
                activeFacilityFilters.delete(type);
                btn.classList.remove('active');
            } else {
                activeFacilityFilters.add(type);
                btn.classList.add('active');
            }
            facilityMarkers.forEach(({ marker, type: markerType }) => {
                marker.map = activeFacilityFilters.has(markerType) ? map : null;
            });
        });
    });

    // Empty Voyage Prompt
    const modalOverlay = document.getElementById('modal-overlay');
    const btnEmptyPilot = document.getElementById('btn-empty-pilot');
    const btnEmptyManual = document.getElementById('btn-empty-manual');
    const modalEmptyVoyage = document.getElementById('modal-empty-voyage');

    if (btnEmptyPilot) {
        btnEmptyPilot.addEventListener('click', () => {
            modalEmptyVoyage.classList.add('hidden');
            modalOverlay.classList.add('hidden');
            handlePilotSuggestionsClick();
        });
    }

    if (btnEmptyManual) {
        btnEmptyManual.addEventListener('click', () => {
            modalEmptyVoyage.classList.add('hidden');
            modalOverlay.classList.add('hidden');
            if (currentVoyage) {
                const start = new Date(currentVoyage.start_date).toISOString().split('T')[0];
                selectDate(start);
            }
        });
    }
}

function initDiscoveryListeners() {
    const btnDiscover = document.getElementById('btn-discover');
    const btnCloseDiscovery = document.getElementById('btn-close-discovery');
    const monthSlider = document.getElementById('month-slider');

    if (btnDiscover) btnDiscover.addEventListener('click', () => toggleDiscoveryMode(true));
    if (btnCloseDiscovery) btnCloseDiscovery.addEventListener('click', () => toggleDiscoveryMode(false));

    const btnCloseDiscoveryIntro = document.getElementById('btn-close-discovery-intro');
    if (btnCloseDiscoveryIntro) {
        btnCloseDiscoveryIntro.addEventListener('click', () => {
            document.getElementById('modal-discovery-intro').classList.add('hidden');
            document.getElementById('modal-overlay').classList.add('hidden');
        });
    }

    if (monthSlider) {
        const months = [
            'January', 'February', 'March', 'April', 'May', 'June',
            'July', 'August', 'September', 'October', 'November', 'December'
        ];
        monthSlider.addEventListener('input', (e) => {
            const month = parseInt(e.target.value);
            document.getElementById('month-display').textContent = months[month - 1];
            loadDiscoveryRegions(month);
        });
    }
}

function initMobileMenuListeners() {
    const appContainer = document.getElementById('app');
    const btnMobileMenu = document.getElementById('btn-mobile-menu');
    const btnCloseSidebar = document.getElementById('btn-close-sidebar');

    if (btnMobileMenu) btnMobileMenu.addEventListener('click', () => appContainer.classList.add('menu-open'));
    if (btnCloseSidebar) btnCloseSidebar.addEventListener('click', () => appContainer.classList.remove('menu-open'));
}

function initVoyageModalListeners() {
    const btnNewVoyage = document.getElementById('btn-new-voyage');
    const modalOverlay = document.getElementById('modal-overlay');
    const modalNewVoyage = document.getElementById('modal-new-voyage');
    const btnCancelVoyage = document.getElementById('btn-cancel-voyage');
    const formNewVoyage = document.getElementById('form-new-voyage');
    const btnUseMapCenter = document.getElementById('btn-use-map-center');
    const displayCoords = document.getElementById('voyage-coords-display');
    const inputLat = document.getElementById('voyage-lat');
    const inputLng = document.getElementById('voyage-lng');
    const modalTitle = modalNewVoyage.querySelector('h2');
    const submitBtn = formNewVoyage.querySelector('button[type="submit"]');

    // Open Modal (Create Mode)
    btnNewVoyage.addEventListener('click', () => {
        editingVoyageId = null;
        modalTitle.textContent = 'Plan a New Voyage';
        submitBtn.textContent = 'Create Voyage';
        modalOverlay.classList.remove('hidden');
        modalNewVoyage.classList.remove('hidden');
        // Hide date fields for initial creation (Discovery First)
        document.getElementById('voyage-date-fields').classList.add('hidden');
        document.getElementById('voyage-start').value = '';
        document.getElementById('voyage-end').value = '';
        document.getElementById('voyage-title').value = '';
        document.getElementById('voyage-location-name').value = '';
        document.getElementById('voyage-radius').value = 60;
        displayCoords.textContent = '';
        inputLat.value = '';
        inputLng.value = '';
    });

    const closeModal = () => {
        modalOverlay.classList.add('hidden');
        modalNewVoyage.classList.add('hidden');
        formNewVoyage.reset();
        displayCoords.textContent = '';
        inputLat.value = '';
        inputLng.value = '';
        document.getElementById('voyage-precise-location').value = '';
        editingVoyageId = null;
    };

    btnCancelVoyage.addEventListener('click', closeModal);
    modalOverlay.addEventListener('click', closeModal);

    // Auto-set End Date
    const inputStart = document.getElementById('voyage-start');
    const inputEnd = document.getElementById('voyage-end');
    inputStart.addEventListener('change', () => {
        if (inputStart.value && !inputEnd.value) {
            const d = new Date(inputStart.value);
            d.setUTCDate(d.getUTCDate() + 1);
            inputEnd.value = d.toISOString().split('T')[0];
        }
    });

    // Use Map Center
    if (btnUseMapCenter) {
        btnUseMapCenter.addEventListener('click', async () => {
            if (!map) return;
            const center = map.getCenter();
            const lat = center.lat();
            const lng = center.lng();
            const zoom = map.getZoom();

            inputLat.value = lat;
            inputLng.value = lng;
            displayCoords.textContent = `Lat: ${lat.toFixed(4)}, Lng: ${lng.toFixed(4)}`;

            // At zoom 10, ~20nm is good coverage. Higher zoom = smaller radius.
            let radius = Math.round(20 * Math.pow(2, 10 - zoom));
            radius = Math.max(5, Math.min(200, radius));
            const inputRadius = document.getElementById('voyage-radius');
            if (inputRadius) inputRadius.value = radius;

            try {
                const { locationName, preciseLocation } = await reverseGeocode({ lat, lng });
                document.getElementById('voyage-location-name').value = locationName;
                document.getElementById('voyage-precise-location').value = preciseLocation;
            } catch (e) {
                console.warn("Failed to geocode map center", e);
            }
        });
    }

    // Form Submit
    formNewVoyage.addEventListener('submit', async (e) => {
        e.preventDefault();
        const formData = new FormData(formNewVoyage);
        const start = formData.get('start_date');
        const end = formData.get('end_date');
        const voyageData = {
            title: formData.get('title'),
            start_date: start ? start + 'T00:00:00Z' : null,
            end_date: end ? end + 'T00:00:00Z' : null,
            location_name: formData.get('location_name'),
            precise_location: formData.get('precise_location'),
            latitude: formData.get('latitude') ? parseFloat(formData.get('latitude')) : null,
            longitude: formData.get('longitude') ? parseFloat(formData.get('longitude')) : null,
            search_radius: parseInt(formData.get('search_radius')) || 60,
            search_radius_unit: 'nm'
        };
        try {
            let savedVoyage;
            if (editingVoyageId) {
                savedVoyage = await API.updateVoyage(editingVoyageId, voyageData);
            } else {
                savedVoyage = await API.createVoyage(voyageData);
            }
            closeModal();
            await loadVoyages();
            const freshVoyage = voyages.find(v => v.id === savedVoyage.id);
            await selectVoyage(freshVoyage || savedVoyage);
        } catch (err) {
            console.error(err);
            showNotification('Error', 'Failed to save voyage. Check console.');
        }
    });
}

function initNavigationListeners() {
    const btnBack = document.getElementById('btn-back-voyages');
    const btnExport = document.getElementById('btn-export-voyage');
    const btnEditVoyage = document.getElementById('btn-edit-voyage');

    if (btnBack) btnBack.addEventListener('click', showVoyageList);
    if (btnExport) btnExport.addEventListener('click', handleShowReport);

    if (btnEditVoyage) {
        btnEditVoyage.addEventListener('click', () => { if (currentVoyage) openEditModal(currentVoyage); });
    }

    const btnViewGuide = document.getElementById('btn-view-guide');
    if (btnViewGuide) {
        btnViewGuide.addEventListener('click', () => { if (currentVoyage) handleGuideClick(currentVoyage, btnViewGuide); });
    }

    const btnResearchAll = document.getElementById('btn-research-all');
    if (btnResearchAll) btnResearchAll.addEventListener('click', () => handleResearchAll(true));
}

function initModalCloseListeners() {
    const modalOverlay = document.getElementById('modal-overlay');

    // Report modal
    const modalReport = document.getElementById('modal-report');
    const btnCloseReport = document.getElementById('btn-close-report');
    const btnCopyReport = document.getElementById('btn-copy-report');
    const closeReport = () => {
        modalReport.classList.add('hidden');
        if (document.getElementById('modal-new-voyage').classList.contains('hidden')) {
            modalOverlay.classList.add('hidden');
        }
    };
    if (btnCloseReport) btnCloseReport.onclick = closeReport;
    if (btnCopyReport) btnCopyReport.onclick = handleCopyReport;

    // Notification modal
    const modalNotification = document.getElementById('modal-notification');
    const btnCloseNotification = document.getElementById('btn-close-notification');
    if (btnCloseNotification) {
        btnCloseNotification.onclick = () => {
            modalNotification.classList.add('hidden');
            if (document.getElementById('modal-new-voyage').classList.contains('hidden') &&
                document.getElementById('modal-briefing').classList.contains('hidden') &&
                document.getElementById('modal-report').classList.contains('hidden') &&
                document.getElementById('modal-guide').classList.contains('hidden')) {
                modalOverlay.classList.add('hidden');
            }
        };
    }

    // Guide modal
    const modalGuide = document.getElementById('modal-guide');
    const btnCloseGuide = document.getElementById('btn-close-guide');
    const closeGuide = () => {
        modalGuide.classList.add('hidden');
        if (document.getElementById('modal-new-voyage').classList.contains('hidden')) {
            modalOverlay.classList.add('hidden');
        }
    };
    if (btnCloseGuide) btnCloseGuide.onclick = closeGuide;
}

async function handleCopyReport() {
    const btnCopyReport = document.getElementById('btn-copy-report');
    const content = document.getElementById('report-content');
    const originalText = btnCopyReport.textContent;
    btnCopyReport.textContent = 'Processing...';
    btnCopyReport.disabled = true;

    // 0. Convert Remote Images (like the Map) to Data URIs
    const remoteImages = content.querySelectorAll('img');
    const processedImages = [];
    for (const img of remoteImages) {
        if (img.src.startsWith('data:')) continue;
        try {
            const resp = await fetch(img.src);
            const blob = await resp.blob();
            const dataUrl = await new Promise(resolve => {
                const reader = new FileReader();
                reader.onload = () => resolve(reader.result);
                reader.readAsDataURL(blob);
            });
            processedImages.push({ el: img, src: img.src });
            img.src = dataUrl;
        } catch (err) {
            console.warn('Failed to embed image:', img.src, err);
        }
    }

    // 1. Convert Canvases to Images
    const canvases = content.querySelectorAll('canvas');
    const tempImages = [];
    canvases.forEach(canvas => {
        const img = document.createElement('img');
        img.src = canvas.toDataURL();
        img.style.width = '100%';
        img.style.height = 'auto';
        canvas.parentNode.insertBefore(img, canvas);
        canvas.style.display = 'none';
        tempImages.push({ canvas, img });
    });

    // 2. Remove Icons (to avoid copying their text)
    const icons = content.querySelectorAll('.material-symbols-outlined');
    const removedIcons = [];
    icons.forEach(icon => {
        const placeholder = document.createComment('icon-placeholder');
        const parent = icon.parentNode;
        removedIcons.push({ icon, parent, next: icon.nextSibling, placeholder });
        parent.replaceChild(placeholder, icon);
    });

    // 2b. Convert Facility Lists to Divs with H4s
    const facilityLists = content.querySelectorAll('.facility-list');
    const modifiedLists = [];
    facilityLists.forEach(ul => {
        const container = document.createElement('div');
        const listItems = ul.querySelectorAll('li.facility-item');
        const originalItems = [];
        listItems.forEach(li => {
            const h4 = document.createElement('h4');
            while (li.firstChild) h4.appendChild(li.firstChild);
            container.appendChild(h4);
            originalItems.push({ li, h4 });
        });
        ul.parentNode.insertBefore(container, ul);
        ul.style.display = 'none';
        modifiedLists.push({ ul, container, originalItems });
    });

    // 2c. Convert Overview Grid to Table
    const overviewGrid = content.querySelector('.overview-grid');
    const overviewReplacements = [];
    if (overviewGrid) {
        const table = document.createElement('table');
        table.style.width = '100%';
        table.style.borderCollapse = 'separate';
        table.style.borderSpacing = '10px';
        const cards = Array.from(overviewGrid.querySelectorAll('.overview-card'));
        let currentRow = null;
        cards.forEach((card, index) => {
            if (index % 4 === 0) {
                currentRow = document.createElement('tr');
                table.appendChild(currentRow);
            }
            const td = document.createElement('td');
            td.style.border = '1px solid #ccc';
            td.style.borderRadius = '8px';
            td.style.padding = '10px';
            td.style.backgroundColor = '#fff';
            td.style.verticalAlign = 'top';
            td.style.width = '25%';
            while (card.firstChild) td.appendChild(card.firstChild);
            currentRow.appendChild(td);
        });
        overviewGrid.parentNode.insertBefore(table, overviewGrid);
        overviewGrid.style.display = 'none';
        overviewReplacements.push({ grid: overviewGrid, table, originalCards: cards });
    }

    // 3. Strip Styles and Classes
    const allElements = content.querySelectorAll('*');
    const originalAttributes = [];
    allElements.forEach(el => {
        if (tempImages.some(t => t.img === el)) return;
        if (modifiedLists.some(m => m.ul === el)) return;
        if (overviewReplacements.some(r => r.grid === el)) return;
        originalAttributes.push({ el, style: el.getAttribute('style'), class: el.getAttribute('class') });
        el.removeAttribute('style');
        el.removeAttribute('class');
    });

    // 3a. Apply clipboard-friendly styles
    content.querySelectorAll('th').forEach(th => {
        th.style.textAlign = 'left';
        th.style.backgroundColor = 'rgb(227, 220, 211)';
        th.style.padding = '4px 8px';
        th.style.border = '1px solid #cccccc';
        th.style.textTransform = 'capitalize';
        const attr = originalAttributes.find(a => a.el === th);
        if (attr && attr.class && attr.class.includes('briefing-table-label-width')) {
            th.style.width = '20ch';
            th.style.whiteSpace = 'nowrap';
        }
    });
    content.querySelectorAll('thead').forEach(thead => { thead.style.backgroundColor = 'rgba(0,0,0,0.05)'; });
    content.querySelectorAll('td').forEach(td => { td.style.padding = '4px 8px'; td.style.border = '1px solid #cccccc'; td.style.verticalAlign = 'top'; });
    content.querySelectorAll('table').forEach(table => { table.style.borderCollapse = 'collapse'; table.style.width = '100%'; table.style.marginTop = '1rem'; table.style.marginBottom = '1rem'; });
    content.querySelectorAll('h3').forEach(h3 => { h3.style.color = 'rgb(88, 61, 27)'; h3.style.marginTop = '1.5rem'; h3.style.marginBottom = '0.5rem'; h3.style.borderBottom = '1px solid #cccccc'; h3.style.paddingBottom = '4px'; });
    content.querySelectorAll('h4').forEach(h4 => { h4.style.margin = '0.5rem 0'; h4.style.fontSize = '1.1rem'; h4.style.color = 'rgb(88, 61, 27)'; });
    content.querySelectorAll('img').forEach(img => { img.style.width = '100%'; img.style.maxWidth = '600px'; img.style.height = 'auto'; img.style.display = 'block'; img.style.margin = '1rem 0'; });

    // 4. Copy to clipboard (Clipboard API with HTML, fallback to execCommand)
    try {
        const htmlBlob = new Blob([content.outerHTML], { type: 'text/html' });
        const textBlob = new Blob([content.innerText], { type: 'text/plain' });
        await navigator.clipboard.write([new ClipboardItem({ 'text/html': htmlBlob, 'text/plain': textBlob })]);
        btnCopyReport.textContent = 'Copied!';
        setTimeout(() => btnCopyReport.textContent = originalText, 2000);
    } catch (_clipboardErr) {
        const range = document.createRange();
        range.selectNode(content);
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
        try {
            document.execCommand('copy');
            btnCopyReport.textContent = 'Copied!';
            setTimeout(() => btnCopyReport.textContent = originalText, 2000);
        } catch (err) {
            console.error('Failed to copy', err);
            showNotification('Error', 'Failed to copy report to clipboard');
            btnCopyReport.textContent = originalText;
        } finally {
            window.getSelection().removeAllRanges();
        }
    } finally {
        // 5. Restore everything
        window.getSelection().removeAllRanges();
        originalAttributes.forEach(({ el, style, class: cls }) => {
            if (style !== null) el.setAttribute('style', style); else el.removeAttribute('style');
            if (cls !== null) el.setAttribute('class', cls); else el.removeAttribute('class');
        });
        modifiedLists.forEach(({ ul, container, originalItems }) => {
            originalItems.forEach(({ li, h4 }) => { while (h4.firstChild) li.appendChild(h4.firstChild); });
            container.remove();
            ul.style.display = '';
        });
        removedIcons.forEach(({ icon, parent, placeholder }) => { parent.replaceChild(icon, placeholder); });
        tempImages.forEach(({ canvas, img }) => { canvas.style.display = ''; img.remove(); });
        overviewReplacements.forEach(({ grid, table, originalCards }) => {
            table.querySelectorAll('td').forEach((td, i) => {
                const card = originalCards[i];
                if (card) { while (td.firstChild) card.appendChild(td.firstChild); }
            });
            table.remove();
            grid.style.display = '';
        });
        processedImages.forEach(({ el, src }) => { el.src = src; });
        btnCopyReport.disabled = false;
    }
}

async function loadVoyages() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '<p class="loading-text">Loading voyages...</p>';

  try {
    voyages = await API.getVoyages(currentVoyagePage, VOYAGE_PAGE_LIMIT);
    renderVoyageList();
  } catch (err) {
    console.error(err);
    if (err.message === 'Unauthorized') {
        listContainer.innerHTML = '<p class="loading-text">Login to view and plan your voyages.</p>';
    } else {
        listContainer.innerHTML = '<p class="loading-text error">Failed to load voyages.</p>';
    }
  }
}

function renderVoyageList() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '';

  if ((!voyages || voyages.length === 0) && currentVoyagePage === 1) {
    listContainer.innerHTML = '<p class="loading-text">No voyages yet. Plan your first trip!</p>';
    return;
  }

  const dated = voyages
    .filter(v => v.start_date)
    .sort((a, b) => {
      const dateDiff = new Date(b.start_date) - new Date(a.start_date);
      return dateDiff !== 0 ? dateDiff : a.title.localeCompare(b.title);
    });

  const undated = voyages
    .filter(v => !v.start_date)
    .sort((a, b) => a.title.localeCompare(b.title));

  const sorted = [...dated, ...undated];

  if (dated.length > 0 && undated.length > 0) {
    sorted.splice(dated.length, 0, 'SEPARATOR');
    sorted.splice(0, 0, 'DATED_HEADER');
  }

  sorted.forEach(voyage => {
    if (voyage === 'DATED_HEADER') {
      const hdr = document.createElement('p');
      hdr.className = 'font-xs text-gray uppercase tracking-wider p-xs mb-xs';
      hdr.textContent = 'Upcoming & Recent';
      listContainer.appendChild(hdr);
      return;
    }
    if (voyage === 'SEPARATOR') {
      const sep = document.createElement('p');
      sep.className = 'font-xs text-gray uppercase tracking-wider p-xs mt-sm mb-xs voyage-list-separator';
      sep.textContent = 'Undated';
      listContainer.appendChild(sep);
      return;
    }
    const el = document.createElement('div');
    el.className = 'voyage-item';
    
    const locationHtml = voyage.location_name ? `<p class="font-sm text-gray">📍 ${DOMPurify.sanitize(displayLocationName(voyage.location_name))}</p>` : '';
    
    const startDate = voyage.start_date ? new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : 'No dates set';
    const endDate = voyage.end_date ? new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : '';
    const dateRange = endDate ? `${startDate} - ${endDate}` : startDate;
    
    el.innerHTML = DOMPurify.sanitize(`
      <div class="voyage-info">
        <h2>${voyage.title}</h2>
        <p>${dateRange}</p>
        ${locationHtml}
      </div>
      <div class="voyage-actions">
        <button class="btn-icon edit" title="Edit">
          <span class="material-symbols-outlined">edit</span>
        </button>
        <button class="btn-icon delete" title="Delete">
          <span class="material-symbols-outlined">delete</span>
        </button>
      </div>
    `);
    
    // Select Voyage
    el.querySelector('.voyage-info').addEventListener('click', () => selectVoyage(voyage));

    // Edit Voyage
    const btnEdit = el.querySelector('.edit');
    btnEdit.addEventListener('click', (e) => {
      e.stopPropagation();
      openEditModal(voyage);
    });

    // Delete Voyage
    const btnDelete = el.querySelector('.delete');
    btnDelete.addEventListener('click', async (e) => {
      e.stopPropagation();
      showNotification('Delete Voyage', `Are you sure you want to delete "${voyage.title}"?`, [
          {
              label: 'Delete',
              type: 'danger',
              hideClose: true,
              callback: async () => {
                  try {
                      await API.deleteVoyage(voyage.id);
                      loadVoyages();
                      if (currentVoyage && currentVoyage.id === voyage.id) {
                          showVoyageList(); // Reset view if we deleted the current voyage
                      }
                  } catch (err) {
                      console.error(err);
                      showNotification('Error', 'Failed to delete voyage');
                  }
              }
          },
          {
              label: 'Cancel',
              type: 'secondary'
          }
      ]);
    });

    listContainer.appendChild(el);
  });

  // Pagination Controls
  const paginationControls = document.createElement('div');
  paginationControls.className = 'flex justify-center align-center gap-md mt-md p-sm';
  
  const prevBtn = document.createElement('button');
  prevBtn.className = 'btn secondary';
  prevBtn.disabled = currentVoyagePage === 1;
  prevBtn.innerHTML = '<span class="material-symbols-outlined">chevron_left</span>';
  prevBtn.onclick = () => {
      if (currentVoyagePage > 1) {
          currentVoyagePage--;
          loadVoyages();
      }
  };

  const pageLabel = document.createElement('span');
  pageLabel.className = 'text-gray font-sm';
  pageLabel.textContent = `Page ${currentVoyagePage}`;

  const nextBtn = document.createElement('button');
  nextBtn.className = 'btn secondary';
  // If we got fewer items than limit, we are likely on the last page
  nextBtn.disabled = voyages.length < VOYAGE_PAGE_LIMIT;
  nextBtn.innerHTML = '<span class="material-symbols-outlined">chevron_right</span>';
  nextBtn.onclick = () => {
      currentVoyagePage++;
      loadVoyages();
  };

  paginationControls.appendChild(prevBtn);
  paginationControls.appendChild(pageLabel);
  paginationControls.appendChild(nextBtn);

  if (voyages.length > 0 || currentVoyagePage > 1) {
      listContainer.appendChild(paginationControls);
  }
}

function openEditModal(voyage) {
    editingVoyageId = voyage.id;
    const modalOverlay = document.getElementById('modal-overlay');
    const modalNewVoyage = document.getElementById('modal-new-voyage');
    const modalTitle = modalNewVoyage.querySelector('h2');
    const submitBtn = document.querySelector('#form-new-voyage button[type="submit"]');

    modalTitle.textContent = 'Edit Voyage';
    submitBtn.textContent = 'Update Voyage';
    
    // Show date fields in edit mode
    document.getElementById('voyage-date-fields').classList.remove('hidden');

    document.getElementById('voyage-title').value = voyage.title;
    document.getElementById('voyage-start').value = voyage.start_date ? voyage.start_date.split('T')[0] : '';
    document.getElementById('voyage-end').value = voyage.end_date ? voyage.end_date.split('T')[0] : '';
    document.getElementById('voyage-location-name').value = voyage.location_name || '';
    document.getElementById('voyage-radius').value = voyage.search_radius || 60;
    document.getElementById('voyage-precise-location').value = voyage.precise_location || '';
    
    if (voyage.latitude != null && voyage.longitude != null) {
        document.getElementById('voyage-lat').value = voyage.latitude;
        document.getElementById('voyage-lng').value = voyage.longitude;
        document.getElementById('voyage-coords-display').textContent = `Lat: ${voyage.latitude.toFixed(4)}, Lng: ${voyage.longitude.toFixed(4)}`;
    } else {
        document.getElementById('voyage-lat').value = '';
        document.getElementById('voyage-lng').value = '';
        document.getElementById('voyage-coords-display').textContent = '';
    }

    modalOverlay.classList.remove('hidden');
    modalNewVoyage.classList.remove('hidden');
}

function showVoyageList() {
    document.getElementById('voyage-list').classList.remove('hidden');
    document.getElementById('itinerary-view').classList.add('hidden');
    document.querySelector('.sidebar-actions').classList.remove('hidden');

    currentVoyage = null;
    selectedDate = null;
    lastKnownItineraryFull = false;
    clearRecommendations();   // also clears pilot poll/SSE
    clearPilotCircle();
    clearMap();
    clearStopSweeps();
    // Cancel any other in-flight research polls
    if (_fullResPoll) { clearInterval(_fullResPoll); _fullResPoll = null; }
    if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
    if (_guidePoll) { clearInterval(_guidePoll); _guidePoll = null; }
    if (_guideProgressES) { _guideProgressES.close(); _guideProgressES = null; }
    if (_stopPoll) { clearInterval(_stopPoll); _stopPoll = null; }
    if (_stopProgressES) { _stopProgressES.close(); _stopProgressES = null; }
}

function updateItineraryHeader(voyage) {
    document.getElementById('itinerary-title').textContent = voyage.title;
    
    const startDate = voyage.start_date ? new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : null;
    const endDate = voyage.end_date ? new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : null;
    
    const datesEl = document.getElementById('itinerary-dates');
    const btnSetDates = document.getElementById('btn-set-dates');

    if (startDate && endDate) {
        datesEl.textContent = `${startDate} - ${endDate}`;
        if (btnSetDates) btnSetDates.classList.add('hidden');
    } else {
        datesEl.textContent = 'No dates set for this voyage';
        if (btnSetDates) btnSetDates.classList.remove('hidden');
    }
}

async function selectVoyage(voyage) {
    try {
        // Fetch full voyage details to ensure we have search_radius and other fields
        const fullVoyage = await API.getVoyage(voyage.id);
        currentVoyage = fullVoyage;
    } catch (err) {
        console.warn("Failed to fetch full voyage details, using list data", err);
        currentVoyage = voyage;
    }

    document.getElementById('voyage-list').classList.add('hidden');
    document.getElementById('itinerary-view').classList.remove('hidden');
    document.querySelector('.sidebar-actions').classList.add('hidden');
    
    // For mobile
    document.getElementById('app').classList.remove('menu-open');

    lastKnownItineraryFull = false;
    clearRecommendations();
    clearPilotCircle();
    updateItineraryHeader(currentVoyage);

    // Hide/Show Stop-based actions in Discovery Mode
    const hasDates = currentVoyage.start_date && currentVoyage.end_date;
    const btnResearchAll = document.getElementById('btn-research-all');
    if (btnResearchAll) {
        if (!hasDates) btnResearchAll.classList.add('hidden');
    }

    // Reset pagination
    currentStopPage = 1;
    loadStops();
}

async function loadStops() {
    const list = document.getElementById('itinerary-list');
    list.innerHTML = '<div class="loading-state"><p>Loading stops...</p></div>';

    try {
        if (currentVoyage.start_date && currentVoyage.end_date) {
            currentStops = await API.getStops(currentVoyage.id, currentStopPage, STOP_PAGE_LIMIT);
        } else {
            currentStops = [];
        }
        
        renderItinerary();
        renderMapStops();

        // Toggle visibility of Research All button
        const btnResearchAll = document.getElementById('btn-research-all');
        if (btnResearchAll) {
            const hasDates = currentVoyage.start_date && currentVoyage.end_date;
            if (currentStops.length > 0 && hasDates) {
                btnResearchAll.classList.remove('hidden');
            } else {
                btnResearchAll.classList.add('hidden');
            }
        }

        // Auto-load recommendations if they exist
        try {
            const res = await API.getRecommendations(currentVoyage.id);
            voyageRecommendations = ensureRecommendationsArray(res);
            renderRecommendations();
            
            // Re-render itinerary now that we have recommendations (to hide the prompt in Discovery Mode)
            renderItinerary();
        } catch (e) {
            voyageRecommendations = [];
            console.warn("No recommendations found or failed to load", e);
        }

        // Check if itinerary is empty AND research hasn't been done - Show Prompt
        const hasDates = currentVoyage.start_date && currentVoyage.end_date;
        if (hasDates && currentStops.length === 0 && currentStopPage === 1 && voyageRecommendations.length === 0) {
            document.getElementById('modal-empty-voyage').classList.remove('hidden');
            document.getElementById('modal-overlay').classList.remove('hidden');
        } else if (voyageRecommendations.length > 0) {
            // Ensure empty voyage modal is hidden if we have recommendations (Discovery Mode or Planner)
            document.getElementById('modal-empty-voyage').classList.add('hidden');
            const modalNewVoyage = document.getElementById('modal-new-voyage');
            if (modalNewVoyage.classList.contains('hidden')) {
                document.getElementById('modal-overlay').classList.add('hidden');
            }
        }

        await checkItineraryFullness();

        if (map) {
            const { LatLngBounds } = await importLibrary("core");
            const bounds = new LatLngBounds();

            if (currentStops.length > 0) {
                currentStops.forEach(stop => bounds.extend({ lat: stop.latitude, lng: stop.longitude }));
            }
            
            if (currentVoyage.latitude != null && currentVoyage.longitude != null) {
                bounds.extend({ lat: currentVoyage.latitude, lng: currentVoyage.longitude });
                
                // If we have no stops yet, zoom to the search radius
                if (currentStops.length === 0) {
                    const { Circle } = await importLibrary("maps");
                    const radiusMeters = (currentVoyage.search_radius || 60) * 1852;
                    const tempCircle = new Circle({
                        center: { lat: currentVoyage.latitude, lng: currentVoyage.longitude },
                        radius: radiusMeters
                    });
                    bounds.union(tempCircle.getBounds());
                }
            }

            // Include Pilot Circle in bounds if it exists
            if (pilotCircle) {
                bounds.union(pilotCircle.getBounds());
            }

            if (!bounds.isEmpty()) {
                map.fitBounds(bounds, 50);
            }
        }
    } catch (err) {
        console.error(err);
        showNotification('Error', 'Failed to load stops');
        list.innerHTML = '<div class="error-state"><p>Failed to load stops.</p></div>';
    }
}

async function executeResearchAll() {
    if (!currentVoyage) return;
    
    isResearchAllRunning = true;
    renderItinerary();
    
    const btnResearchAll = document.getElementById('btn-research-all');
    const originalContent = btnResearchAll.innerHTML;
    btnResearchAll.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
    btnResearchAll.disabled = true;
    
    // 1. Visual Indicators: Spin all icons
    const researchBtns = document.querySelectorAll('.day-actions .research');
    researchBtns.forEach(btn => {
        // Only spin if not already done/spinning
        if (!btn.querySelector('.spin') && !btn.querySelector('.material-symbols-outlined').textContent.includes('check_circle')) {
           btn.dataset.originalContent = btn.innerHTML;
           btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
           btn.disabled = true;
        }
    });

    try {
        // 1b. Snapshot timestamps to distinguish new data from old
        const initialTimestamps = {};
        try {
            const existingGuide = await API.getVoyageGuide(currentVoyage.id);
            if (existingGuide) initialTimestamps['guide'] = new Date(existingGuide.created_at).getTime();
            
            const existingBriefings = (await API.getVoyageBriefings(currentVoyage.id)) || [];
            existingBriefings.forEach(b => {
                if (b) initialTimestamps['stop_' + b.stop_id] = new Date(b.created_at).getTime();
            });
        } catch (e) { console.warn("Failed to snapshot timestamps", e); }

        // Helper to check freshness
        const isNewData = (item, type, id) => {
            const key = type + (id ? '_' + id : '');
            const prevTime = initialTimestamps[key];
            if (!prevTime) return true; // No previous data, so this must be new
            const newTime = new Date(item.created_at).getTime();
            return newTime > prevTime;
        };

        const fullResRes = await API.triggerFullResearch(currentVoyage.id);
        showNotification('Research Started', 'Full voyage research has started. Individual stops will update as they complete.');
        if (researchTicker) researchTicker.start();

        // Start radar sweep sequence cycling through all stops
        if (map && currentStops.length > 0) {
            startStopSweepSequence(currentStops);
        }

        // Stream progress updates into ticker
        if (fullResRes && fullResRes.session_id && researchTicker) {
            _fullResProgressES = API.streamProgress(fullResRes.session_id, (evt) => {
                researchTicker.push(evt.message);
            });
        }

        // 2. Poll for completion
        const startTime = Date.now();
        const TIMEOUT_MS = 240000; // 4 minutes timeout

        let pendingStops = [...currentStops];
        let guideComplete = false;
        let consecutiveErrors = 0;

        _fullResPoll = setInterval(async () => {
            // Check timeout
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(_fullResPoll); _fullResPoll = null;
                if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
                clearStopSweeps();
                if (researchTicker) researchTicker.error('Research Timeout');
                btnResearchAll.innerHTML = originalContent;
                btnResearchAll.disabled = false;

                // Revert stuck spinners
                const stuckBtns = document.querySelectorAll('.day-actions .research:disabled');
                stuckBtns.forEach(btn => {
                       btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
                       btn.disabled = false;
                });
                showNotification('Research Timeout', 'Research is taking longer than expected. Some stops may still be processing.');
                return;
            }

            try {
                // Check Guide
                if (!guideComplete) {
                    const g = await API.getVoyageGuide(currentVoyage.id);
                    if (g && isNewData(g, 'guide')) {
                        guideComplete = true;
                        if (researchTicker) researchTicker.push('Voyage guide complete');
                    }
                }

                // Check Stops
                try {
                    const briefings = (await API.getVoyageBriefings(currentVoyage.id)) || [];

                    for (let i = pendingStops.length - 1; i >= 0; i--) {
                        const stop = pendingStops[i];
                        const b = (briefings || []).find(br => br.stop_id === stop.id);

                        if (b && isNewData(b, 'stop', stop.id)) {
                            pendingStops.splice(i, 1);
                            removeStopFromSweepQueue(stop.id);

                            if (researchTicker) researchTicker.push(`Research complete for ${displayLocationName(stop.location_name)}`);

                            const btn = document.querySelector(`.research[data-stop-id="${stop.id}"]`);
                            if (btn) {
                                btn.innerHTML = '<span class="material-symbols-outlined" style="color: var(--brand-green);">check_circle</span>';
                                btn.disabled = false;
                                btn.title = "View Briefing";
                                btn.classList.remove('spin');
                            }
                        }
                    }
                } catch (e) {
                    console.warn("Poll: Briefings fetch failed", e);
                    consecutiveErrors++;
                }

                // If all done
                if (guideComplete && pendingStops.length === 0) {
                    clearInterval(_fullResPoll); _fullResPoll = null;
                    if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
                    clearStopSweeps();
                    isResearchAllRunning = false;
                    renderItinerary();
                    if (researchTicker) researchTicker.stop();
                    btnResearchAll.innerHTML = originalContent;
                    btnResearchAll.disabled = false;

                    renderMapStops();
                    showNotification('Research Complete', 'All research tasks have been completed successfully.');
                } else if (consecutiveErrors > 15) {
                    clearInterval(_fullResPoll); _fullResPoll = null;
                    if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
                    clearStopSweeps();
                    isResearchAllRunning = false;
                    renderItinerary();
                    if (researchTicker) researchTicker.error('Research Failed');
                    btnResearchAll.innerHTML = originalContent;
                    btnResearchAll.disabled = false;
                    showNotification('Research Failed', 'Connection to server lost. Please try again.');
                } else {
                    consecutiveErrors = 0;
                }

            } catch (err) {
                console.error("Polling cycle error", err);
                consecutiveErrors++;
            }
        }, 5000); // Poll every 5 seconds

    } catch (err) {
        console.error(err);
        clearInterval(_fullResPoll); _fullResPoll = null;
        if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
        clearStopSweeps();
        if (researchTicker) researchTicker.stop();
        btnResearchAll.innerHTML = originalContent;
        btnResearchAll.disabled = false;
        // Revert spinners
        const researchBtns = document.querySelectorAll('.day-actions .research');
        researchBtns.forEach(btn => {
           btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
           btn.disabled = false;
        });
        showNotification('Error', 'Failed to trigger research.');
    }
}

async function handleResearchAll(confirmFirst = true) {
    if (!currentVoyage) return;
    
    if (confirmFirst) {
        showNotification('Full Research', 'This will trigger research for the entire voyage and all stops. Continue?', [
            {
                label: 'Continue',
                callback: executeResearchAll
            },
            {
                label: 'Cancel',
                type: 'secondary'
            }
        ]);
    } else {
        executeResearchAll();
    }
}

async function checkItineraryFullness(isManualAction = false) {
    if (!currentVoyage) return;
    
    // If no dates, it can't be full in the itinerary sense
    if (!currentVoyage.start_date || !currentVoyage.end_date) {
        lastKnownItineraryFull = false;
        
        // Ensure recommendations are visible if research hasn't been done
        const btnPilot = document.getElementById('btn-pilot-suggestions');
        if (btnPilot) btnPilot.classList.remove('hidden');
        
        const btnResearchAll = document.getElementById('btn-research-all');
        if (btnResearchAll) btnResearchAll.classList.add('hidden');
        
        return;
    }

    // Calculate expected days
    const start = new Date(currentVoyage.start_date);
    const end = new Date(currentVoyage.end_date);
    const diffTime = Math.abs(end - start);
    const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24)) + 1;

    let isFull = false;
    let allResearchDone = false;

    try {
        // Fetch ALL stops for this voyage to be sure (bypass pagination)
        const allStops = (await API.getStops(currentVoyage.id, 1, 1000)) || [];
        isFull = allStops.length >= diffDays;

        if (isFull) {
            // Check if briefings exist for all stops AND guide exists
            const briefings = (await API.getVoyageBriefings(currentVoyage.id)) || [];
            const guide = await API.getVoyageGuide(currentVoyage.id);
            
            // Criteria: Briefings for every stop + Global Voyage Guide
            allResearchDone = briefings.length >= allStops.length && guide !== null;
        }
    } catch (e) {
        console.error("Failed to check itinerary/research status", e);
    }

    const oldFull = lastKnownItineraryFull;
    const oldResearch = lastKnownResearchDone;

    lastKnownItineraryFull = isFull;
    lastKnownResearchDone = allResearchDone;

    if (oldFull !== lastKnownItineraryFull || oldResearch !== lastKnownResearchDone) {
        renderItinerary();
    }

    // Toggle visibility of Research All button
    const btnResearchAll = document.getElementById('btn-research-all');
    if (btnResearchAll) {
        if (currentStops.length > 0) {
            btnResearchAll.classList.remove('hidden');
        } else {
            btnResearchAll.classList.add('hidden');
        }
    }

    // Toggle visibility of Pilot Suggestions button
    const btnPilot = document.getElementById('btn-pilot-suggestions');
    if (btnPilot) {
        if (isFull) {
            btnPilot.classList.add('hidden');
        } else {
            btnPilot.classList.remove('hidden');
        }
    }

    // If it just BECAME full OR if this was a manual action filling the last slot, notify user
    // (ONLY if research isn't already done)
    if (isFull && !allResearchDone && (isManualAction || !lastKnownItineraryFull)) {
        showNotification(
            "Itinerary Ready", 
            "Every day of your voyage now has a destination! All research hubs and spots have been cleared. \n\nNext Step: Click the 'Research All' (Globe) icon to gather weather, tides, and local charts for your trip.",
            {
                label: "Start Full Research Now",
                callback: () => handleResearchAll(false)
            }
        );
        clearRecommendations();
        clearPilotCircle();
    }

    lastKnownItineraryFull = isFull;

    // Ensure recommendations are hidden if full
    if (isFull) {
        clearRecommendations();
        clearPilotCircle();
    }
}

async function handlePilotSuggestionsClick() {
    if (!currentVoyage) return;
    
    isPilotResearching = true;
    renderItinerary();
    
    const btn = document.getElementById('btn-pilot-suggestions');
    const icon = btn.querySelector('.material-symbols-outlined');
    
    try {
        icon.classList.add('spin');
        btn.disabled = true;
        
        // 1. Ensure latest drag state is synced to DB before triggering research
        if (pilotCircle) {
            await syncToBackend(true);
        } else {
            await renderPilotCircle();
        }
        
        // 2. Clear existing recommendations from UI before starting fresh
        voyageRecommendations = [];
        renderRecommendations();

        // Start radar sweep animation — hide drag handles during research
        if (map && pilotCircle) {
            if (radarSweep) radarSweep.stop();
            if (pilotCenterMarker) pilotCenterMarker.map = null;
            if (pilotRadiusMarker) pilotRadiusMarker.map = null;
            const RadarSweepClass = await getRadarSweepClass();
            radarSweep = new RadarSweepClass(map, pilotCircle.getCenter(), pilotCircle.getRadius());
        }


        // 3. Trigger both Local Pilot (Recommendations) and Voyage Guide research
        const [recRes] = await Promise.all([
            API.generateRecommendations(currentVoyage.id),
            API.triggerVoyageGuideResearch(currentVoyage.id)
        ]);

        // Start a ticker to show progress
        if (researchTicker) {
            researchTicker.start("The AI is researching resource hubs, anchorages, and moorings...");
        }

        // Shared completion handler — called from either the done progress event or the poll.
        let pilotDone = false;
        const finishPilotResearch = async () => {
            if (pilotDone) return;
            pilotDone = true;
            clearInterval(_pilotPoll); _pilotPoll = null;
            if (_pilotEventSource) { _pilotEventSource.close(); _pilotEventSource = null; }
            if (_pilotProgressES) { _pilotProgressES.close(); _pilotProgressES = null; }

            // Do a final fetch to ensure we have the full list
            try {
                const res = await API.getRecommendations(currentVoyage.id);
                const recs = ensureRecommendationsArray(res);
                if (recs && recs.length > 0) {
                    voyageRecommendations = recs;
                    renderRecommendations();
                    renderItinerary();
                }
            } catch (e) { /* best effort */ }

            isPilotResearching = false;
            if (radarSweep) { radarSweep.stop(); radarSweep = null; }
            if (pilotCenterMarker) pilotCenterMarker.map = map;
            if (pilotRadiusMarker) pilotRadiusMarker.map = map;
            if (researchTicker) researchTicker.stop();
            icon.classList.remove('spin');
            btn.disabled = false;

            if (voyageRecommendations.length > 0) {
                showNotification("Research Complete", `We've identified ${voyageRecommendations.length} resource hubs, anchorages, and moorings in your voyage area.`);
                if (map && voyageRecommendations.length > 0) {
                    const { LatLngBounds } = await importLibrary("core");
                    const bounds = new LatLngBounds();
                    voyageRecommendations.forEach(r => bounds.extend({ lat: r.latitude, lng: r.longitude }));
                    if (pilotCircle) bounds.union(pilotCircle.getBounds());
                    map.fitBounds(bounds, 100);
                }
            } else {
                showNotification('Incomplete', "The AI research is taking longer than expected. Please try again or check back in a few minutes.");
            }
        };

        // Stream progress events into the ticker; trigger completion on 'done'.
        if (recRes.progress_session_id && researchTicker) {
            _pilotProgressES = API.streamProgress(recRes.progress_session_id, (evt) => {
                researchTicker.push(evt.message);
                if (evt.stage === 'done') {
                    console.log('[progress] done received — finishing pilot research');
                    finishPilotResearch();
                }
            });
        }

        const sessionID = recRes.session_id;
        if (sessionID) {
            console.log('[rec-stream] connecting', sessionID);
            _pilotEventSource = new EventSource(`${API_BASE}/voyages/${currentVoyage.id}/recommendations/stream?session_id=${sessionID}`);

            _pilotEventSource.addEventListener('recommendation', (e) => {
                try {
                    const rec = JSON.parse(e.data);
                    console.log('[rec-stream] recommendation', rec.name, rec.type);
                    if (!voyageRecommendations.some(r => r.id === rec.id)) {
                        voyageRecommendations.push(rec);
                        renderRecommendations();
                        renderItinerary();
                    }
                } catch (err) {
                    console.error('[rec-stream] parse error', err);
                }
            });

            _pilotEventSource.onerror = (e) => {
                console.warn('[rec-stream] stream closed or error', e);
                if (_pilotEventSource) { _pilotEventSource.close(); _pilotEventSource = null; }
            };
        }

        // Poll as a fallback in case the progress stream is unavailable.
        // Completes when the agent is clearly done (10+ results and count stable).
        let attempts = 0;
        let lastCount = 0;
        let stableRounds = 0;
        const maxAttempts = 30; // 5 minutes (10s interval)

        _pilotPoll = setInterval(async () => {
            attempts++;
            try {
                const res = await API.getRecommendations(currentVoyage.id);
                const recs = ensureRecommendationsArray(res);

                if (recs && recs.length > 0) {
                    voyageRecommendations = recs;
                    renderRecommendations();
                    renderItinerary();
                }

                // Stable count for 2 consecutive polls with at least 10 results means agent is done
                if (recs && recs.length >= 10) {
                    if (recs.length === lastCount) {
                        stableRounds++;
                        if (stableRounds >= 2) finishPilotResearch();
                    } else {
                        stableRounds = 0;
                    }
                    lastCount = recs.length;
                }
            } catch (e) {
                console.error("Polling error:", e);
            }

            if (attempts >= maxAttempts) finishPilotResearch();
        }, 10000);

    } catch (err) {
        console.error(err);
        isPilotResearching = false;
        if (radarSweep) { radarSweep.stop(); radarSweep = null; }
        if (pilotCenterMarker) pilotCenterMarker.map = map;
        if (pilotRadiusMarker) pilotRadiusMarker.map = map;
        renderItinerary();
        showNotification('Error', 'Failed to start pilot suggestions');
        icon.classList.remove('spin');
        btn.disabled = false;
    }
}

async function renderRecommendations() {
    try {
        clearRecommendationMarkers();
        if (!map || !currentVoyage) return;

        if (!Array.isArray(voyageRecommendations)) {
            console.error("voyageRecommendations is not an array!", voyageRecommendations);
            return;
        }

        // Only render pilot circle if itinerary is NOT full OR if we have recommendations
        if (!lastKnownItineraryFull || (voyageRecommendations && voyageRecommendations.length > 0)) {
            await renderPilotCircle();
        }

        if (!voyageRecommendations || voyageRecommendations.length === 0) return;

        const { AdvancedMarkerElement } = await importLibrary("marker");

        const styles = {
            hub:       { color: MARKER_COLORS.hub,       icon: 'hub',          label: 'Resource Hub' },
            anchorage: { color: MARKER_COLORS.anchorage, icon: 'anchor',       label: 'Anchorage' },
            mooring:   { color: MARKER_COLORS.mooring,   icon: 'crisis_alert', label: 'Mooring' },
        };

        voyageRecommendations.forEach((rec) => {
            const type = (rec.type || '').toLowerCase();
            let style = styles.hub;
            if (type.includes('anchor')) style = styles.anchorage;
            else if (type.includes('moor')) style = styles.mooring;

            const iconDiv = document.createElement('div');
            iconDiv.className = 'map-marker-icon';
            iconDiv.style.backgroundColor = style.color;
            iconDiv.innerHTML = `<span class="material-symbols-outlined map-icon-glyph">${style.icon}</span>`;

            const marker = new AdvancedMarkerElement({
                map,
                position: { lat: rec.latitude, lng: rec.longitude },
                content: iconDiv,
                title: rec.name,
                zIndex: 20,
            });

            marker.addListener('gmp-click', () => {
                showRecommendationInfoWindow(rec, marker, style);
            });

            const filterKey = type.includes('anchor') ? 'anchorage' : type.includes('moor') ? 'mooring' : 'hub';
            if (!activeFilters.has(filterKey)) marker.map = null;

            recommendationMarkers.push({ marker, type: filterKey });
        });

        if (recommendationMarkers.length > 0) {
            document.getElementById('pilot-controls').classList.remove('hidden');
        }

    } catch (err) {
        console.error("Error in renderRecommendations:", err);
    }
}

function showRecommendationInfoWindow(rec, anchor, style) {
    if (activeInfoWindow) activeInfoWindow.close();

    const { InfoWindow } = googleMapsLib;
    const references = rec.reference_links ? (typeof rec.reference_links === 'string' ? JSON.parse(rec.reference_links) : rec.reference_links) : [];
    
    let resourcesHtml = '';
    if (rec.url || references.length > 0) {
        resourcesHtml = `
            <div style="margin-bottom: 15px;">
                <p style="margin: 0 0 5px 0; font-size: 0.8rem; font-weight: bold; color: #666; text-transform: uppercase;">Resources:</p>
                <div style="display: flex; flex-wrap: wrap; gap: 8px;">
                    ${rec.url ? `
                        <a href="${rec.url}" target="_blank" style="font-size: 0.85rem; color: #1a73e8; font-weight: bold; text-decoration: none; display: flex; align-items: center; gap: 4px;">
                            <span class="material-symbols-outlined" style="font-size: 14px;">language</span>
                            Website
                        </a>
                    ` : ''}
                    ${references.map((url, i) => `
                        <a href="${url}" target="_blank" style="font-size: 0.85rem; color: #1a73e8; text-decoration: none; display: flex; align-items: center; gap: 4px;">
                            <span class="material-symbols-outlined" style="font-size: 14px;">link</span>
                            Link ${i+1}
                        </a>
                    `).join('')}
                </div>
            </div>`;
    }

    const hasDates = currentVoyage && currentVoyage.start_date && currentVoyage.end_date;
    const addButton = hasDates ? `
            <button class="btn primary w-full p-sm" onclick="addRecommendationToItinerary('${rec.id}')">
                <span class="material-symbols-outlined icon-align" style="font-size: 18px; margin-right: 5px;">add_location_alt</span>
                Add to Itinerary
            </button>` : '';

    const content = `
        <div style="color: black; max-width: 280px; font-family: 'Lato', sans-serif; padding: 5px;">
            <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px;">
                <span class="material-symbols-outlined" style="color: ${style.color};">${style.icon}</span>
                <b style="font-size: 1.2rem; color: #1a73e8;">${rec.name}</b>
            </div>
            <span style="font-size: 0.85rem; color: ${style.color}; text-transform: uppercase; font-weight: 900; letter-spacing: 1px;">${style.label}</span><br>
            <p style="margin: 10px 0; font-size: 0.9rem; line-height: 1.5; color: #333;">${rec.description || ''}</p>
            <div style="background: ${style.color}1A; padding: 10px; border-radius: 6px; border-left: 3px solid ${style.color}; margin-bottom: 15px;">
                <p style="margin: 0; font-size: 0.85rem; font-style: italic; color: #555;">"${rec.reasoning || ''}"</p>
            </div>
            ${resourcesHtml}
            ${addButton}
        </div>`;
    
    activeInfoWindow = new InfoWindow({
        content: content,
        position: anchor.position
    });
    activeInfoWindow.open(map, anchor instanceof google.maps.marker.AdvancedMarkerElement ? anchor : null);
}

function showFacilityInfoWindow(f, anchor, color) {
    if (activeInfoWindow) activeInfoWindow.close();

    const { InfoWindow } = googleMapsLib;

    const addressHtml = f.address
        ? `<p style="margin: 4px 0 8px; font-size: 0.85rem; color: #666;">${f.address}</p>`
        : '';

    let ratingHtml = '';
    if (f.rating) {
        const stars = Math.round(f.rating);
        const filled = '★'.repeat(stars);
        const empty = '☆'.repeat(5 - stars);
        const count = f.user_rating_count ? ` (${f.user_rating_count.toLocaleString()})` : '';
        ratingHtml = `<p style="margin: 4px 0; font-size: 0.9rem; color: #555;">
            <span style="color: #F9A825;">${filled}${empty}</span>
            <span style="margin-left: 4px;">${f.rating.toFixed(1)}${count}</span>
        </p>`;
    }

    let statusHtml = '';
    if (f.business_status && f.business_status !== 'OPERATIONAL') {
        const label = f.business_status.replace(/_/g, ' ');
        statusHtml = `<p style="margin: 4px 0; font-size: 0.8rem; color: #C62828; font-weight: 700;">${label}</p>`;
    }

    const descriptionHtml = f.details?.description
        ? `<p style="margin: 10px 0; font-size: 0.9rem; line-height: 1.5; color: #333;">${f.details.description}</p>`
        : '';

    const detailRows = f.details
        ? Object.entries(f.details)
            .filter(([k, v]) => k !== 'description' && v && String(v).toLowerCase() !== 'n/a')
            .map(([k, v]) => `<p style="margin: 3px 0; font-size: 0.85rem; color: #444;"><strong>${k}:</strong> ${v}</p>`)
            .join('')
        : '';

    const websiteHtml = f.website ? `
        <a href="${f.website}" target="_blank" style="font-size: 0.85rem; color: #1a73e8; text-decoration: none; display: flex; align-items: center; gap: 4px;">
            <span class="material-symbols-outlined" style="font-size: 14px;">public</span>Visit Website
        </a>` : '';

    const content = `
        <div style="color: black; max-width: 280px; font-family: 'Lato', sans-serif; padding: 5px;">
            <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px;">
                <span class="material-symbols-outlined" style="color: ${color};">location_on</span>
                <b style="font-size: 1.2rem; color: #1a73e8;">${f.name}</b>
            </div>
            <span style="font-size: 0.85rem; color: ${color}; text-transform: uppercase; font-weight: 900; letter-spacing: 1px;">${f.type || 'Facility'}</span>
            ${addressHtml}
            ${ratingHtml}
            ${statusHtml}
            ${descriptionHtml}
            ${detailRows ? `<div style="background: ${color}1A; padding: 8px; border-radius: 6px; border-left: 3px solid ${color}; margin: 10px 0;">${detailRows}</div>` : ''}
            ${websiteHtml}
        </div>`;

    activeInfoWindow = new InfoWindow({ content, position: anchor.position });
    activeInfoWindow.open(map, anchor);
}

function clearPilotCircle() {
    if (pilotCircle) { pilotCircle.setMap(null); pilotCircle = null; }
    if (pilotCenterMarker) { pilotCenterMarker.map = null; pilotCenterMarker = null; }
    if (pilotRadiusMarker) { pilotRadiusMarker.map = null; pilotRadiusMarker = null; }
}

function clearRecommendationMarkers() {
    recommendationMarkers.forEach(({ marker }) => marker.map = null);
    recommendationMarkers = [];
    document.getElementById('pilot-controls').classList.add('hidden');
}

function clearRecommendations() {
    clearRecommendationMarkers();
    // Tear down any in-flight pilot research streams/polls
    if (_pilotEventSource) { _pilotEventSource.close(); _pilotEventSource = null; }
    if (_pilotProgressES) { _pilotProgressES.close(); _pilotProgressES = null; }
    if (_pilotPoll) { clearInterval(_pilotPoll); _pilotPoll = null; }
}

async function renderPilotCircle() {
    if (!map || !currentVoyage) return;
    if (pilotCircle) return; // Already rendered; avoid destroying an active drag handle
    if (pilotCenterMarker) pilotCenterMarker.map = null;
    if (pilotRadiusMarker) pilotRadiusMarker.map = null;

    const { Circle } = await importLibrary("maps");
    const { AdvancedMarkerElement, PinElement } = await importLibrary("marker");
    const { spherical } = await importLibrary("geometry");
    
    // 60 nm default = 111120 meters
    const radiusMeters = currentVoyage.search_radius ? currentVoyage.search_radius * 1852 : 111120;
    const center = { lat: currentVoyage.latitude, lng: currentVoyage.longitude };

    // Add Pilot Range Circle
    pilotCircle = new Circle({
        strokeColor: "#1a73e8",
        strokeOpacity: 0.5,
        strokeWeight: 2,
        fillColor: "#1a73e8",
        fillOpacity: 0.05,
        map: map,
        center: center,
        radius: radiusMeters,
        clickable: false,
        draggable: false,
        editable: false, 
        zIndex: 5
    });

    // Add Center Dot and Label
    const centerContainer = document.createElement('div');
    centerContainer.className = 'pilot-center-container';

    const centerPin = new PinElement({
        scale: 0.6,
        background: "#1a73e8",
        borderColor: "white",
        glyph: ""
    });
    centerContainer.appendChild(centerPin);

    const label = document.createElement('div');
    label.className = 'pilot-radius-label';
    centerContainer.appendChild(label);

    pilotCenterMarker = new AdvancedMarkerElement({
        map: map,
        position: center,
        content: centerContainer,
        title: "Research Center",
        gmpDraggable: true,
        zIndex: 10
    });

    // Add Radius Handle (on the East edge)
    const radiusHandlePin = new PinElement({
        scale: 0.4,
        background: "white",
        borderColor: "#1a73e8",
        glyph: ""
    });

    const edgePos = spherical.computeOffset(center, radiusMeters, 90);
    pilotRadiusMarker = new AdvancedMarkerElement({
        map: map,
        position: edgePos,
        content: radiusHandlePin,
        title: "Resize Research Area",
        gmpDraggable: true,
        zIndex: 11
    });

    const updateRadiusDisplay = () => {
        if (!pilotCircle) return;
        const rMeters = pilotCircle.getRadius();
        const rNm = Math.round(rMeters / 1852);
        label.textContent = `${rNm} nm`;
    };

    // Sync Center Marker -> Circle & Radius Marker
    pilotCenterMarker.addListener('drag', (e) => {
        const newCenter = e.latLng;
        const currentRadius = pilotCircle.getRadius();
        pilotCircle.setCenter(newCenter);
        pilotRadiusMarker.position = spherical.computeOffset(newCenter, currentRadius, 90);
        currentVoyage.latitude = newCenter.lat();
        currentVoyage.longitude = newCenter.lng();
        updateRadiusDisplay();
    });

    // Sync Radius Marker -> Circle Radius
    pilotRadiusMarker.addListener('drag', (e) => {
        const c = pilotCircle.getCenter();
        const newRadius = spherical.computeDistanceBetween(c, e.latLng);
        pilotCircle.setRadius(newRadius);
        currentVoyage.search_radius = Math.round(newRadius / 1852);
        updateRadiusDisplay();
    });

    pilotCenterMarker.addListener('dragend', () => syncToBackend());
    pilotRadiusMarker.addListener('dragend', () => syncToBackend());

    updateRadiusDisplay();
}

window.addRecommendationToItinerary = async function(recId) {
    const rec = voyageRecommendations.find(r => r.id === recId);
    if (!rec) return;

    if (!currentVoyage || !currentVoyage.start_date || !currentVoyage.end_date) {
        showNotification('Planning Mode Required', 'Please set voyage dates before adding specific spots to your daily itinerary.');
        return;
    }
        // Find first empty date
        const start = new Date(currentVoyage.start_date);
        const end = new Date(currentVoyage.end_date);
        let targetDate = null;

        for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
            const dateStr = d.toISOString().split('T')[0];
            if (!currentStops.some(s => s.target_date.startsWith(dateStr))) {
                targetDate = dateStr;
                break;
            }
        }

        if (!targetDate) {
            showNotification('Notice', "No empty dates left in your voyage!");
            return;
        }

        const stopData = {
            target_date: targetDate + 'T00:00:00Z',
            location_name: rec.name,
            precise_location: `${rec.name}, ${rec.type}`,
            latitude: rec.latitude,
            longitude: rec.longitude,
            search_radius: 5,
            search_radius_unit: 'nm',
            notes: rec.description
        };

        try {
            const created = await API.createStop(currentVoyage.id, stopData);
            currentStops.push(created);
            
            if (activeInfoWindow) {
                activeInfoWindow.close();
                activeInfoWindow = null;
            }

            renderItinerary();
            renderMapStops();
            await checkItineraryFullness(true);
            showNotification('Success', `Added ${rec.name} to your itinerary for ${new Date(targetDate).toLocaleDateString()}.`);

        } catch (err) {
            console.error(err);
            showNotification('Error', 'Failed to add recommendation to itinerary');
        }
}

function renderItinerary() {
    const list = document.getElementById('itinerary-list');
    list.innerHTML = '';
    
    if (!currentVoyage.start_date || !currentVoyage.end_date) {
        if (isPilotResearching) {
            list.innerHTML = '<div class="p-md text-center"><p class="text-gray">Researching area...</p></div>';
            return;
        }

        if (voyageRecommendations && voyageRecommendations.length > 0) {
            // Show Discovery Results in Sidebar
            const header = document.createElement('div');
            header.className = 'p-sm border-b text-gray font-xs uppercase tracking-wider';
            header.textContent = 'Recommended Hubs & Spots';
            list.appendChild(header);

            voyageRecommendations.forEach(rec => {
                const recColor = markerColor(rec.type);
                const tl = (rec.type || '').toLowerCase();
                let recIcon = 'location_on';
                if (tl.includes('anchor')) recIcon = 'anchor';
                else if (tl.includes('moor')) recIcon = 'crisis_alert';
                else if (tl.includes('hub') || tl.includes('marina')) recIcon = 'hub';

                const el = document.createElement('div');
                el.className = 'day-item day-item--stacked';
                el.innerHTML = DOMPurify.sanitize(`
                    <div style="display:flex;align-items:center;gap:8px;width:100%;margin-bottom:6px;">
                        <span class="material-symbols-outlined" style="color:${recColor};font-size:20px;">${recIcon}</span>
                        <span style="font-weight:700;flex:1;">${displayLocationName(rec.name)}</span>
                        <span style="font-size:0.7rem;font-weight:900;text-transform:uppercase;letter-spacing:1px;color:#fff;background:${recColor};padding:2px 7px;border-radius:20px;white-space:nowrap;">${rec.type || 'Spot'}</span>
                    </div>
                    ${rec.description ? `<p style="margin:0 0 6px;font-size:0.82rem;color:var(--text-color);line-height:1.4;">${rec.description}</p>` : ''}
                    ${rec.reasoning ? `
                        <div style="background:${recColor}1A;padding:7px 10px;border-radius:5px;border-left:3px solid ${recColor};width:100%;box-sizing:border-box;">
                            <p style="margin:0;font-size:0.78rem;font-style:italic;color:#555;">
                                <strong style="font-style:normal;color:${recColor};">Pilot's Reasoning:</strong> "${rec.reasoning}"
                            </p>
                        </div>` : ''}
                `);
                el.onclick = () => {
                    if (map) {
                        map.panTo({lat: rec.latitude, lng: rec.longitude});
                        map.setZoom(15);
                    }
                };
                list.appendChild(el);
            });
            return;
        }

        const container = document.createElement('div');
        container.className = 'p-md text-center';
        container.innerHTML = '<p class="text-gray mb-md">Use <b>Local Pilot Research</b> to explore the area. Set dates when you\'re ready to plan your daily itinerary.</p>';
        
        const btn = document.createElement('button');
        btn.className = 'btn secondary w-full';
        btn.innerHTML = '<span class="material-symbols-outlined icon-align">auto_awesome</span> Run Local Pilot Research';
        btn.onclick = () => handlePilotSuggestionsClick();
        
        container.appendChild(btn);
        list.appendChild(container);
        return;
    }

    let currentDate = new Date(currentVoyage.start_date);
    const endDate = new Date(currentVoyage.end_date);

    while (currentDate <= endDate) {
        const dateStr = currentDate.toISOString().split('T')[0];
        const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
        
        const el = document.createElement('div');
        el.className = `day-item ${selectedDate === dateStr ? 'selected' : ''}`;
        
        // Day Info
        let html = `
            <div class="day-info flex-1">
                <span class="day-date">${currentDate.toLocaleDateString(undefined, {month:'short', day:'numeric', timeZone: 'UTC'})}</span>
                <span class="day-location ${stop ? 'set' : ''}">${stop ? displayLocationName(stop.location_name) : 'No destination'}</span>
            </div>
        `;
        
        // Research Action
        if (stop) {
            html += `
                <div class="day-actions">
                    <button class="btn-icon research" title="Research" data-stop-id="${stop.id}">
                        <span class="material-symbols-outlined">science</span>
                    </button>
                    <button class="btn-icon delete-stop" title="Delete Stop">
                        <span class="material-symbols-outlined">delete</span>
                    </button>
                </div>
            `;
        }
        
        el.innerHTML = DOMPurify.sanitize(html);
        
        // Handlers
        el.addEventListener('click', () => selectDate(dateStr));
        
        if (stop) {
            const btnResearch = el.querySelector('.research');
            btnResearch.addEventListener('click', (e) => {
                e.stopPropagation();
                handleResearchClick(stop, btnResearch);
            });

            const btnDelete = el.querySelector('.delete-stop');
            btnDelete.addEventListener('click', async (e) => {
                e.stopPropagation();
                showNotification('Delete Stop', `Remove stop at ${displayLocationName(stop.location_name)}?`, [
                    {
                        label: 'Remove',
                        type: 'danger',
                        hideClose: true,
                        callback: async () => {
                            try {
                                await API.deleteStop(stop.id);
                                currentStops = currentStops.filter(s => s.id !== stop.id);
                                renderItinerary();
                                renderMapStops();
                                await checkItineraryFullness(true);
                            } catch (err) {
                                console.error(err);
                                showNotification('Error', 'Failed to delete stop');
                            }
                        }
                    },
                    {
                        label: 'Cancel',
                        type: 'secondary'
                    }
                ]);
            });
        }

        list.appendChild(el);
        
        currentDate.setUTCDate(currentDate.getUTCDate() + 1);
    }

    // Pagination Controls
    const paginationControls = document.createElement('div');
    paginationControls.className = 'flex justify-center align-center gap-md mt-md p-sm';
    
    const prevBtn = document.createElement('button');
    prevBtn.className = 'btn secondary';
    prevBtn.disabled = currentStopPage === 1;
    prevBtn.innerHTML = '<span class="material-symbols-outlined">chevron_left</span>';
    prevBtn.onclick = () => {
        if (currentStopPage > 1) {
            currentStopPage--;
            loadStops();
        }
    };

    const pageLabel = document.createElement('span');
    pageLabel.className = 'text-gray font-sm';
    pageLabel.textContent = `Stops Page ${currentStopPage}`;

    const nextBtn = document.createElement('button');
    nextBtn.className = 'btn secondary';
    nextBtn.disabled = currentStops.length < STOP_PAGE_LIMIT;
    nextBtn.innerHTML = '<span class="material-symbols-outlined">chevron_right</span>';
    nextBtn.onclick = () => {
        currentStopPage++;
        loadStops();
    };

    paginationControls.appendChild(prevBtn);
    paginationControls.appendChild(pageLabel);
    paginationControls.appendChild(nextBtn);

    if (currentStops.length > 0 || currentStopPage > 1) {
        list.appendChild(paginationControls);

        // Show Full Research Prompt if Full but research missing
        if (lastKnownItineraryFull && !lastKnownResearchDone && !isResearchAllRunning) {
            const container = document.createElement('div');
            container.className = 'p-md text-center border-t mt-md';
            container.innerHTML = '<p class="text-gray mb-md"><b>Itinerary Complete!</b> Run full voyage research to get weather, tides, and pilot info for every stop.</p>';
            list.appendChild(container);
        }
        }

}

async function handleResearchClick(stop, button) {
    const originalContent = button.innerHTML;
    
    try {
        const existing = await API.getBriefing(stop.id);
        if (existing) {
            showBriefing(existing);
            return;
        }

        // Show Modal Immediately with loading state
        const modal = document.getElementById('modal-briefing');
        const content = document.getElementById('briefing-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = `
            <div class="loading-state">
                <span class="material-symbols-outlined spin loading-icon">sync</span>
                <p class="font-xs">Checking weather, tides, and local charts.</p>
            </div>
        `;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        if (researchTicker) researchTicker.start();
        const resRes = await API.triggerResearch(stop.id);

        // Start radar sweep on this stop's position
        if (map) startStopSweepSequence([stop]);

        // Stream progress updates into the ticker
        if (resRes.session_id && researchTicker) {
            _stopProgressES = API.streamProgress(resRes.session_id, (evt) => {
                researchTicker.push(evt.message);
            });
        }

        // Poll
        _stopPoll = setInterval(async () => {
            try {
                const b = await API.getBriefing(stop.id);
                if (b) {
                    clearInterval(_stopPoll); _stopPoll = null;
                    if (_stopProgressES) { _stopProgressES.close(); _stopProgressES = null; }
                    clearStopSweeps();
                    if (researchTicker) researchTicker.stop();
                    button.innerHTML = originalContent;
                    showBriefing(b);
                    renderMapStops();
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);

    } catch (err) {
        console.error(err);
        clearInterval(_stopPoll); _stopPoll = null;
        if (_stopProgressES) { _stopProgressES.close(); _stopProgressES = null; }
        clearStopSweeps();
        if (researchTicker) researchTicker.stop();
        button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
        setTimeout(() => button.innerHTML = originalContent, 2000);
    }
}

/**
 * parseTidePoints — shared helper for tide chart functions.
 * Converts tide event data into Chart.js {x, y} points where x is hours
 * relative to midnight of targetDateStr (wall-clock local time).
 * @param {object} tideData - object with an `events` array
 * @param {string} targetDateStr - "YYYY-MM-DD" or ISO string
 * @param {number} [hourMin=-Infinity] - drop points before this hour offset
 * @param {number} [hourMax=Infinity]  - drop points after this hour offset
 */
function parseTidePoints(tideData, targetDateStr, hourMin = -Infinity, hourMax = Infinity) {
    const parseLocal = (s) => {
        if (!s) return new Date(NaN);
        const clean = s.replace('Z', '').replace(' ', 'T');
        const final = clean.length === 10 ? clean + 'T00:00:00' : clean;
        return new Date(final);
    };

    const targetStart = parseLocal(targetDateStr).getTime();
    const points = [];

    (tideData.events || []).forEach(e => {
        const d = parseLocal(e.time);
        if (!isNaN(d.getTime())) {
            const floatHours = (d.getTime() - targetStart) / (1000 * 60 * 60);
            if (floatHours >= hourMin && floatHours <= hourMax) {
                points.push({ x: floatHours, y: e.height_ft });
            }
        }
    });

    points.sort((a, b) => a.x - b.x);
    return points;
}

async function renderTideChart(canvasId, tideData, targetDateStr) {
    if (!tideData || !tideData.events) return;

    const points = parseTidePoints(tideData, targetDateStr);

    const Chart = await loadChart();
    if (chartInstances.has(canvasId)) { chartInstances.get(canvasId).destroy(); chartInstances.delete(canvasId); }
    const ctx = document.getElementById(canvasId).getContext('2d');

    chartInstances.set(canvasId, new Chart(ctx, {
        type: 'line',
        data: {
            datasets: [{
                label: 'Tide Height (ft)',
                data: points,
                borderColor: '#0077be',
                backgroundColor: 'rgba(0, 119, 190, 0.2)',
                borderWidth: 2,
                tension: 0.4, // Smooth Bezier
                pointRadius: 4,
                pointHoverRadius: 6,
                fill: 'start'
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    callbacks: {
                        title: (context) => {
                            const val = context[0].parsed.x;
                            const h = Math.floor(val);
                            const m = Math.round((val - h) * 60);
                            // Handle day overflow for label
                            let label = `${h.toString().padStart(2,'0')}:${m.toString().padStart(2,'0')}`;
                            if (h < 0) label += " (Prev Day)";
                            if (h >= 24) label += " (Next Day)";
                            return `Local Time: ${label}`;
                        }
                    }
                }
            },
            scales: {
                x: {
                    type: 'linear',
                    min: 0,
                    max: 24,
                    title: { display: true, text: 'Hour (Local Time)' },
                    ticks: {
                        stepSize: 3,
                        callback: (v) => {
                            if (v < 0 || v > 24) return '';
                            return `${v}:00`;
                        }
                    }
                },
                y: {
                    title: { display: true, text: 'Feet' },
                    grid: {
                        color: (context) => {
                            if (context.tick.value === 0) {
                                return '#333'; // Darker color for zero line
                            }
                            return 'rgba(0, 0, 0, 0.1)'; // Default grid color
                        },
                        lineWidth: (context) => {
                            if (context.tick.value === 0) {
                                return 2; // Thicker line for zero
                            }
                            return 1;
                        }
                    }
                }
            }
        }
    }));
}

async function showBriefing(briefing) {
    const modal = document.getElementById('modal-briefing');
    const content = document.getElementById('briefing-content');
    const btnClose = document.getElementById('btn-close-briefing');
    const btnRedo = document.getElementById('btn-redo-briefing');
    const modalOverlay = document.getElementById('modal-overlay');

    const isInvalid = (v) => {
        if (!v) return true;
        const sv = String(v).toLowerCase().trim();
        return sv === 'n/a' || sv === 'unknown' || sv === 'not specified';
    };

    const renderReferences = (refs) => {
        if (!refs || refs.length === 0) return '';
        return `<div class="ref-link">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
        </div>`;
    };

    // Determine Target Date for Filtering & Charting
    const stop = currentStops.find(s => s.id === briefing.stop_id);
    const targetDateFull = stop ? stop.target_date : new Date().toISOString();
    const targetDateYMD = targetDateFull.split('T')[0]; // "YYYY-MM-DD"

    // Weather
    const weather = briefing.weather_summary || {};
    const weatherHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">${getIconForWeather(weather.condition)}</span>
                Weather
            </h3>
            <div class="weather-box">
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Summary</th>
                        <td class="briefing-td">${isInvalid(weather.summary) ? 'N/A' : weather.summary}</td>
                    </tr>
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Conditions</th>
                        <td class="briefing-td briefing-td-icon">
                            <span class="material-symbols-outlined icon-lg">${getIconForWeather(weather.condition)}</span>
                            ${isInvalid(weather.condition) ? 'N/A' : weather.condition}
                        </td>
                    </tr>
                    ${(weather.temp_max_f || weather.temp_min_f) ? `
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Temp</th>
                        <td class="briefing-td">High: ${Math.round(weather.temp_max_f)}°F &nbsp;|&nbsp; Low: ${Math.round(weather.temp_min_f)}°F</td>
                    </tr>
                    ` : ''}
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Wind</th>
                        <td class="briefing-td">${isInvalid(weather.wind_direction) ? 'N/A' : weather.wind_direction} ${weather.wind_speed_kt || '0'} kt</td>
                    </tr>
                    ${weather.wave_height_ft > 0 ? `
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Waves</th>
                        <td class="briefing-td">${weather.wave_height_ft} ft</td>
                    </tr>
                    ` : ''}
                </table>
            </div>
        </div>
    `;

    // Sun Phase
    const sun = briefing.sun_phase || {};
    let sunHtml = '';
    if (sun.sunrise || sun.sunset) {
        // Format times to be more readable if they are full date strings
        const formatTime = (t) => {
            if (!t) return 'N/A';
            try {
                const d = new Date(t);
                if (isNaN(d.getTime())) return t;
                return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
            } catch (e) {
                return t;
            }
        };

        sunHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">wb_twilight</span>
                Sun Phase
            </h3>
            <div class="weather-box"> <!-- Reuse weather box style -->
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Sunrise</th>
                        <td class="briefing-td">${formatTime(sun.sunrise)}</td>
                    </tr>
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Sunset</th>
                        <td class="briefing-td">${formatTime(sun.sunset)}</td>
                    </tr>
                </table>
            </div>
        </div>
        `;
    }

    // Tides
    // Filter events to only show the target date in the LIST
    const tides = briefing.tides || {};
    const allEvents = tides.events || [];
    const displayEvents = allEvents.filter(e => e.time.startsWith(targetDateYMD));
    
    // Fix: Define displayDateHeader
    const [y, m, d] = targetDateYMD.split('-');
    const displayDateHeader = `${m}/${d}/${y}`;

    const tideEventsHtml = displayEvents.map(e => {
        let timeStr = e.time;
        try {
            const d = new Date(e.time.replace(' ', 'T'));
            if (!isNaN(d.getTime())) {
                let hours = d.getHours();
                const minutes = String(d.getMinutes()).padStart(2, '0');
                const ampm = hours >= 12 ? 'pm' : 'am';
                hours = hours % 12;
                hours = hours ? hours : 12;
                timeStr = `${hours}:${minutes} ${ampm}`;
            }
        } catch (ignore) {}

        return `<tr>
            <td class="briefing-td">${timeStr}</td>
            <td class="briefing-td">${e.type}</td>
            <td class="briefing-td">${e.height_ft} ft</td>
        </tr>`;
    }).join('');
    
    const tidesHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">waves</span>
                Tides (${tides.station_name || 'Unknown Station'}) - ${displayDateHeader}
            </h3>
            <div class="tide-box mb-md">
                <div class="tide-chart-container">
                    <canvas id="tideChartModal"></canvas>
                </div>
                <table class="briefing-table">
                    <thead>
                        <tr>
                            <th class="briefing-th">Time</th>
                            <th class="briefing-th">Type</th>
                            <th class="briefing-th">Height</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${tideEventsHtml || '<tr><td colspan="3" class="briefing-no-data">No tide data for this date</td></tr>'}
                    </tbody>
                </table>
            </div>
        </div>
    `;

    // Facilities
    const facilities = briefing.facilities || [];
    const facilHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">warehouse</span>
                Facilities
            </h3>
            <ul class="facility-list">
                ${facilities.map(f => {
                    // Icon Mapping
                    let icon = 'place';
                    const typeLower = (f.type || '').toLowerCase();
                    if (typeLower.includes('anchorage')) icon = 'anchor';
                    else if (typeLower.includes('marina')) icon = 'storefront';
                    else if (typeLower.includes('mooring')) icon = 'crisis_alert';
                    else if (typeLower.includes('bar')) icon = 'local_bar';
                    else if (typeLower.includes('restaurant')) icon = 'restaurant';

                    let detailsHtml = '';
                    if (typeof f.details === 'string') {
                        detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                    } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        const starHtml = (rating) => {
                            const stars = Math.round(rating);
                            return '★'.repeat(stars) + '☆'.repeat(5 - stars);
                        };

                        let rows = `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Type</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${f.type}</td>
                            </tr>
                        `;
                        if (f.address) rows += `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Address</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${f.address}</td>
                            </tr>`;
                        if (f.rating) rows += `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Rating</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top; color: #F9A825;">${starHtml(f.rating)} <span style="color: #333;">${f.rating.toFixed(1)}${f.user_rating_count ? ` (${f.user_rating_count.toLocaleString()} reviews)` : ''}</span></td>
                            </tr>`;
                        if (f.business_status && f.business_status !== 'OPERATIONAL') rows += `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Status</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top; color: #C62828; font-weight: 700;">${f.business_status.replace(/_/g, ' ')}</td>
                            </tr>`;
                        if (f.website) rows += `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Website</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;"><a href="${f.website}" target="_blank">${f.website}</a></td>
                            </tr>`;

                        rows += Object.entries(f.details)
                            .filter(([_, v]) => {
                                if (!v) return false;
                                const sv = String(v).toLowerCase().trim();
                                return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                            })
                            .map(([k, v]) => `
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width capitalize" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">${k.replace(/_/g, ' ')}</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${v}</td>
                                </tr>
                            `).join('');
                        
                        detailsHtml = `<table class="briefing-table mt-0" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: "Lato", sans-serif; font-size: 0.9em; margin-top: 0.5rem;">${rows}</table>`;
                    }

                    const typeColor = markerColor(f.type);
                    return `
                        <li class="facility-item">
                            <h4 class="briefing-header-icon" style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
                                <span class="material-symbols-outlined icon-lg" style="color:${typeColor};">${icon}</span>
                                ${f.name}
                                <span style="font-size:0.75rem;font-weight:900;text-transform:uppercase;letter-spacing:1px;color:#fff;background:${typeColor};padding:2px 8px;border-radius:20px;margin-left:auto;">${f.type || 'Facility'}</span>
                            </h4>
                            ${detailsHtml}
                            ${renderReferences(f.references)}
                        </li>
                    `;
                }).join('') || '<li>No facilities found</li>'}
            </ul>
        </div>
    `;

    content.innerHTML = DOMPurify.sanitize(weatherHtml + sunHtml + tidesHtml + facilHtml);
    
    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoBriefing(briefing, btnRedo);
    }

    // Show Modal
    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');

    // Render Chart (must happen after modal is visible for size calc)
    // Pass ALL events to chart for smooth interpolation
    if (allEvents.length > 0) {
        await renderTideChart('tideChartModal', tides, targetDateFull);
    }

    const hide = () => {
        modal.classList.add('hidden');
        modalOverlay.classList.add('hidden');
    };

    btnClose.onclick = hide;
    modalOverlay.onclick = hide; 
}

async function redoBriefing(oldBriefing, btn) {
    const content = document.getElementById('briefing-content');
    content.innerHTML = `
        <div class="loading-state">
            <span class="material-symbols-outlined spin loading-icon">sync</span>
            <p class="font-xs">Checking weather, tides, and local charts.</p>
        </div>
    `;
    btn.disabled = true;

    try {
        const redoRes = await API.triggerResearch(oldBriefing.stop_id);

        let redoProgressES = null;
        if (redoRes && redoRes.session_id && researchTicker) {
            researchTicker.start();
            redoProgressES = API.streamProgress(redoRes.session_id, (evt) => {
                researchTicker.push(evt.message);
            });
        }

        const oldTime = new Date(oldBriefing.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 45000; // 45 seconds

        // Poll
        const poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                if (redoProgressES) redoProgressES.close();
                if (researchTicker) researchTicker.stop();
                content.innerHTML = '<div class="error-state"><p><strong>Research timed out.</strong></p><p>The agent is taking too long or encountered an error.</p></div>';
                btn.disabled = false;
                return;
            }

            try {
                const b = await API.getBriefing(oldBriefing.stop_id);
                if (b) {
                    const newTime = new Date(b.created_at).getTime();
                    // Wait for newer timestamp
                    if (newTime > oldTime) {
                        clearInterval(poll);
                        if (redoProgressES) redoProgressES.close();
                        if (researchTicker) researchTicker.stop();
                        btn.disabled = false;
                        showBriefing(b); // Re-render with new data
                        renderMapStops();
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        content.innerHTML = '<div class="error-state error-text"><p>Failed to redo research.</p></div>';
        btn.disabled = false;
    }
}


function selectDate(dateStr) {
    if (!currentVoyage || !currentVoyage.start_date || !currentVoyage.end_date) {
        showNotification('Planning Mode Required', 'Please set voyage dates to begin building your daily itinerary.');
        return;
    }

    if (selectedDate === dateStr) {
        selectedDate = null; // Toggle off
    } else {
        selectedDate = dateStr; // Select new
    }
    
    renderItinerary(); // Re-render to show selection highlight
    
    // Auto-close menu on mobile
    document.getElementById('app').classList.remove('menu-open');
    
    // Zoom to existing stop if present (only if selected)
    if (selectedDate) {
        const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));
        if (stop && map) {
            map.panTo({ lat: stop.latitude, lng: stop.longitude });
            map.setZoom(14);
        }
    }
}

async function initMap() {
  if (!GOOGLE_MAPS_API_KEY) {
    console.error('Google Maps API key is missing. Please set NAVALPLAN_FRONTEND_MAPS_API_KEY environment variable during build.');
    const mapContainer = document.getElementById('map-container');
    if (mapContainer) {
        mapContainer.innerHTML = `
            <div class="flex flex-col items-center justify-center h-full text-center p-xl">
                <span class="material-symbols-outlined icon-xl text-gray mb-md">map</span>
                <h2 class="text-dark">Map Configuration Missing</h2>
                <p class="text-gray max-w-sm">The Google Maps API key is not set. Please configure <code>NAVALPLAN_FRONTEND_MAPS_API_KEY</code> in your environment and rebuild the application.</p>
            </div>
        `;
    }
    return;
  }

  const { Map } = await loadGoogleMaps();
  const { Geocoder } = await importLibrary("geocoding");

  map = new Map(document.getElementById("map-container"), {
    center: { lat: 20, lng: 0 },
    zoom: 3,
    mapId: __GOOGLE_MAPS_MAP_ID__,
    disableDefaultUI: false,
    clickableIcons: false
  });

  // Global Data Layer Styling
  map.data.setStyle((feature) => {
      const type = feature.getProperty('type');

      // If it's a point in the data layer (default), hide it because we use AdvancedMarkers
      if (feature.getGeometry().getType() === 'Point') {
          return { visible: false };
      }
      // 2. Discovery Regions
      const tier = feature.getProperty('tier');
      if (tier) {
          let color = '#0077be'; // Standard Blue
          let strokeColor = '#005fa3';

          if (tier === 'Hidden Gem') {
              color = '#9c27b0'; // Purple
              strokeColor = '#6a1b9a';
          } else if (tier === 'Regional Favorite') {
              color = '#ff9800'; // Orange
              strokeColor = '#ef6c00';
          } else if (tier === 'Challenging') {
              color = '#d32f2f'; // Red
              strokeColor = '#b71c1c';
          }

          return {
              fillColor: color,
              fillOpacity: 0.6,
              strokeColor: strokeColor,
              strokeWeight: 2,
              zIndex: 10
          };
      }

      // Default
      return {
          fillColor: '#4285F4',
          strokeWeight: 1,
          fillOpacity: 0.2
      };
  });

  // Global Data Layer Click Handler
  map.data.addListener('click', (event) => {
      if (event.feature.getProperty('tier')) {
          // Discovery Click
          const props = {
              id: event.feature.getProperty('id'),
              name: event.feature.getProperty('name'),
              tier: event.feature.getProperty('tier'),
              suitability_score: event.feature.getProperty('suitability_score'),
              is_hidden_gem: event.feature.getProperty('is_hidden_gem'),
              summary: event.feature.getProperty('summary'),
              avg_wind_speed_knots: event.feature.getProperty('avg_wind_speed_knots'),
              avg_temp_c: event.feature.getProperty('avg_temp_c'),
              deep_cut_reasoning: event.feature.getProperty('deep_cut_reasoning')
          };
          const month = document.getElementById('month-slider').value;
          showRegionBriefing(props, month);
      }
  });

  console.log('NavalPlan: Map Loaded Successfully');
  map.addListener('click', async (e) => {
      if (!currentVoyage) return;

      if (!currentVoyage.start_date || !currentVoyage.end_date) {
          showNotification('Planning Mode Required', 'To add specific stops to your itinerary, please set your voyage dates first.');
          return;
      }

      if (!selectedDate) {
          showNotification('Select a Date', 'Please select a date from the itinerary sidebar before placing a stop on the map.');
          return;
      }
  
      const lat = e.latLng.lat();
      const lng = e.latLng.lng();
      const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));
  
      // Reverse Geocoding
      let locationName = `Location ${lat.toFixed(3)}, ${lng.toFixed(3)}`;
      let preciseLocation = "";
      try {
          ({ locationName, preciseLocation } = await reverseGeocode(e.latLng));
      } catch (err) {
          console.error("Geocoding failed: " + err);
      }
  
      // Create or Update
      const stopData = {
          target_date: selectedDate + 'T00:00:00Z',
          location_name: locationName,
          precise_location: preciseLocation,
          latitude: lat,
          longitude: lng,
          search_radius: 5,
          search_radius_unit: 'nm',
          notes: ''
      };
  
      try {
          if (stop) {
              const updated = await API.updateStop(stop.id, stopData);
              const idx = currentStops.findIndex(s => s.id === stop.id);
              currentStops[idx] = updated;
          } else {
              const created = await API.createStop(currentVoyage.id, stopData);
              currentStops.push(created);
          }
          renderItinerary();
          renderMapStops();
          await checkItineraryFullness(true);
          // Auto-advance to next empty date

          if (currentVoyage && selectedDate) {
              const current = new Date(selectedDate);
              const next = new Date(current);
              next.setDate(next.getDate() + 1);
              
              // Helper to format YYYY-MM-DD
              const nextStr = next.toISOString().split('T')[0];
              const endStr = new Date(currentVoyage.end_date).toISOString().split('T')[0];

              if (nextStr <= endStr) {
                  const nextStop = currentStops.find(s => s.target_date.startsWith(nextStr));
                  if (!nextStop) {
                      selectDate(nextStr);
                  }
              }
          }
      } catch (err) {
          console.error(err);
          showNotification('Error', 'Failed to save stop');
      }
    });
  }
  
  async function renderMapStops() {
    clearMap();
    if (!map) return;

    const { AdvancedMarkerElement, PinElement } = await importLibrary("marker");
    const { InfoWindow } = await importLibrary("maps");
    const { Polyline } = await importLibrary("maps");

    // Sort stops by date
    const sortedStops = [...currentStops].sort((a, b) => 
        new Date(a.target_date) - new Date(b.target_date)
    );

    // Add Markers
    sortedStops.forEach((stop, index) => {
        const pin = new PinElement({
            glyphText: `${index + 1}`,
            glyphColor: "white",
            background: "#EA4335", // Google Maps Red
            borderColor: "#B31412",
        });

        const marker = new AdvancedMarkerElement({
            map: map,
            position: { lat: stop.latitude, lng: stop.longitude },
            content: pin,
            title: `${displayLocationName(stop.location_name)} (Day ${index + 1})`,
            zIndex: 100
        });
        
        marker.addListener('gmp-click', () => {
             if (activeInfoWindow) activeInfoWindow.close();
             activeInfoWindow = new InfoWindow({
                content: `<div style="color: black;"><b>${displayLocationName(stop.location_name)}</b><br>Day ${index + 1}</div>`
             });
             activeInfoWindow.open(map, marker);
        });

        markers.push(marker);
    });

    // Fetch and Draw Facilities
    (async () => {
        for (const stop of sortedStops) {
            try {
                const b = await API.getBriefing(stop.id);
                if (b && b.facilities) {
                    b.facilities.forEach(f => {
                         if (f.latitude && f.longitude) {
                             const type = (f.type || '').toLowerCase();
                             let iconName = 'location_on';
                             if (type.includes('anchorage')) iconName = 'anchor';
                             else if (type.includes('marina')) iconName = 'directions_boat';
                             else if (type.includes('bar')) iconName = 'local_bar';
                             else if (type.includes('restaurant')) iconName = 'restaurant';

                             const filterKey = type.includes('anchor') ? 'anchorage'
                                 : type.includes('marina') ? 'marina'
                                 : type.includes('moor') ? 'mooring'
                                 : type.includes('restaurant') ? 'restaurant'
                                 : type.includes('bar') ? 'bar'
                                 : 'other';

                             const iconDiv = document.createElement('div');
                             iconDiv.className = 'map-marker-icon map-marker-icon--sm';
                             iconDiv.style.backgroundColor = markerColor(f.type);
                             iconDiv.innerHTML = `<span class="material-symbols-outlined map-icon-glyph">${iconName}</span>`;

                             const fMarker = new AdvancedMarkerElement({
                                 map: activeFacilityFilters.has(filterKey) ? map : null,
                                 position: { lat: f.latitude, lng: f.longitude },
                                 content: iconDiv,
                                 title: f.name,
                                 zIndex: 1
                             });

                             fMarker.addListener('gmp-click', () => {
                                 showFacilityInfoWindow(f, fMarker, markerColor(f.type));
                             });
                             facilityMarkers.push({ marker: fMarker, type: filterKey });
                         }
                    });
                }
            } catch (err) {
               // Ignore errors
            }
        }
        if (facilityMarkers.length > 0) {
            document.getElementById('facility-controls').classList.remove('hidden');
        }
    })();

    // Draw Line
    const coords = sortedStops.map(s => ({ lat: s.latitude, lng: s.longitude }));
    
    routePolyline = new Polyline({
      path: coords,
      geodesic: true,
      strokeColor: "#314c3b",
      strokeOpacity: 0,
      icons: [{
        icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
        offset: '0',
        repeat: '20px'
      }],
      map: map
    });
}
  
  function clearMap() {
      if (activeInfoWindow) activeInfoWindow.close();
      markers.forEach(m => m.map = null);
      markers = [];

      if (routePolyline) {
          routePolyline.setMap(null);
          routePolyline = null;
      }

      facilityMarkers.forEach(({ marker }) => marker.map = null);
      facilityMarkers = [];
      document.getElementById('facility-controls').classList.add('hidden');

      if (map && map.data) {
          map.data.forEach((feature) => {
              // Sparce recommendation blobs and discovery regions
              const type = feature.getProperty('type');
              if (type !== 'recommendation' && type !== 'discovery') {
                  map.data.remove(feature);
              }
          });
      }
  }
  
async function renderMiniTideChart(canvasId, tideData, targetDateStr) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;

    if (!tideData || !tideData.events) return;

    // Allow a buffer around the day so the line extends to edges
    const points = parseTidePoints(tideData, targetDateStr, -6, 30);

    const Chart = await loadChart();
    const tideLevels = {
  
          id: 'tideLevels',
  
          afterDraw: (chart) => {
  
              const { ctx, scales: { x, y } } = chart;
  
              const yZero = y.getPixelForValue(0);
  
              
  
              // 1. Draw Zero Line (if within chart area)
  
              if (yZero >= y.top && yZero <= y.bottom) {
  
                  ctx.save();
  
                  ctx.beginPath();
  
                  ctx.setLineDash([2, 2]);
  
                  ctx.strokeStyle = 'rgba(0, 0, 0, 0.3)';
  
                  ctx.lineWidth = 1;
  
                  ctx.moveTo(x.left, yZero);
  
                  ctx.lineTo(x.right, yZero);
  
                  ctx.stroke();
  
                  ctx.restore();
  
              }
  
  
  
              // 2. Find and Label High/Low for the VISIBLE day (0 to 24)
  
              let maxPt = null;
  
              let minPt = null;
  
  
  
              points.forEach(p => {
  
                  if (p.x >= 0 && p.x <= 24) {
  
                      if (!maxPt || p.y > maxPt.y) maxPt = p;
  
                      if (!minPt || p.y < minPt.y) minPt = p;
  
                  }
  
              });
  
  
  
              const drawLabel = (pt, color, baseline) => {
  
                  if (!pt) return;
  
                  const xPos = x.getPixelForValue(pt.x);
  
                  const yPos = y.getPixelForValue(pt.y);
  
  
  
                  // Draw horizontal dash
  
                  ctx.save();
  
                  ctx.beginPath();
  
                  ctx.setLineDash([2, 2]);
  
                  ctx.strokeStyle = color;
  
                  ctx.lineWidth = 1;
  
                  // Short line around the point
  
                  ctx.moveTo(xPos - 10, yPos);
  
                  ctx.lineTo(xPos + 10, yPos);
  
                  ctx.stroke();
  
  
  
                  // Draw Text
  
                  ctx.font = 'bold 9px sans-serif';
  
                  ctx.fillStyle = color;
  
                  ctx.textAlign = 'center';
  
                  ctx.textBaseline = baseline;
  
                  
  
                  // Offset text slightly
  
                  const offset = baseline === 'bottom' ? -4 : 12;
  
                  ctx.fillText(`${pt.y.toFixed(1)}`, xPos, yPos + offset);
  
                  ctx.restore();
  
              };
  
  
  
              // Draw High
  
              if (maxPt) drawLabel(maxPt, '#d9534f', 'bottom');
  
              // Draw Low
  
              if (minPt) drawLabel(minPt, '#314c3b', 'top');
  
          }
  
      };
  
  
  
      if (chartInstances.has(canvasId)) { chartInstances.get(canvasId).destroy(); chartInstances.delete(canvasId); }
      const ctx = canvas.getContext('2d');

      chartInstances.set(canvasId, new Chart(ctx, {
  
          type: 'line',
  
          data: {
  
              datasets: [{
  
                  data: points,
  
                  borderColor: '#0077be',
  
                  backgroundColor: 'rgba(0, 119, 190, 0.1)',
  
                  borderWidth: 2,
  
                  tension: 0.4,
  
                  pointRadius: 0,
  
                  fill: 'start'
  
              }]
  
          },
  
          options: {
  
              responsive: true,
  
              maintainAspectRatio: false,
  
              plugins: { legend: { display: false }, tooltip: { enabled: false } },
  
              scales: {
  
                  x: { type: 'linear', display: false, min: 0, max: 24 },
  
                  y: { 
  
                      display: false,
  
                      grace: '20%' // Add space for labels
  
                  }
  
              },
  
              layout: { padding: { top: 10, bottom: 10, left: 5, right: 5 } },
  
              animation: false
  
          },
  
          plugins: [tideLevels]

      }));

  }

async function captureAndUploadMap(voyageId) {
    if (!currentVoyage) return false;

    // Construct Static Map URL
    const baseUrl = "https://maps.googleapis.com/maps/api/staticmap";
    const size = "600x400";
    const scale = "2";
    const mapType = "roadmap";
    const key = GOOGLE_MAPS_API_KEY;

    let pathParam = "";
    let markersParam = "";

    const sortedStops = [...(currentStops || [])].sort((a, b) =>
        new Date(a.target_date) - new Date(b.target_date)
    );

    if (sortedStops.length > 0) {
        const stopsToDraw = sortedStops.slice(0, 15); // Limit to avoid URL overflow

        // Draw path first so stop markers render on top
        pathParam = "&path=color:0x999999ff|weight:1";
        stopsToDraw.forEach(s => {
            pathParam += `|${s.latitude},${s.longitude}`;
        });

        // If the last stop is very close to the first, omit it to avoid overlap
        const first = stopsToDraw[0];
        const last = stopsToDraw[stopsToDraw.length - 1];
        const dlat = Math.abs(last.latitude - first.latitude);
        const dlng = Math.abs(last.longitude - first.longitude);
        const lastIsNearFirst = stopsToDraw.length > 1 && dlat < 0.05 && dlng < 0.05;

        // Add stops 2..N first, then green (stop 1) last so it renders on top
        // First stop = green, last stop = red (if not near first), others = blue
        for (let i = 1; i < stopsToDraw.length; i++) {
            if (i === stopsToDraw.length - 1 && lastIsNearFirst) continue;
            const s = stopsToDraw[i];
            const color = i === stopsToDraw.length - 1 ? 'red' : 'blue';
            markersParam += `&markers=size:small%7Ccolor:${color}%7C${s.latitude},${s.longitude}`;
        }
        markersParam += `&markers=size:small%7Ccolor:green%7C${first.latitude},${first.longitude}`;
    } else if (currentVoyage.latitude && currentVoyage.longitude) {
        // No stops yet — show the voyage hub so the map isn't blank
        markersParam += `&markers=color:blue%7C${currentVoyage.latitude},${currentVoyage.longitude}`;
    }

    // Omit center and zoom to allow Google to auto-fit the route stops
    const url = `${baseUrl}?size=${size}&scale=${scale}&maptype=${mapType}${pathParam}${markersParam}&key=${key}`;

    try {
        const response = await fetch(url);
        if (!response.ok) throw new Error('Failed to fetch static map');
        const blob = await response.blob();
        await API.uploadVoyageMap(voyageId, blob);
        return true;
    } catch (err) {
        console.error('Failed to upload map image', err);
        return false;
    }
}

    async function handleShowReport() {
        if (!currentVoyage) return;

        const btn = document.getElementById("btn-export-voyage");
        const originalContent = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = "<span class=\"material-symbols-outlined spin\">sync</span>";

        try {
            // 1. Fetch Pilot Report data (Voyage, Guide, Recommendations)
            const pilotReport = await API.getPilotReport(currentVoyage.id);
            let guide = pilotReport.guide;
            let mapURL = pilotReport.map_url;
            const recommendations = pilotReport.recommendations;

            // 2. Check/Capture Map
            if (!mapURL) {
                 const captured = await captureAndUploadMap(currentVoyage.id);
                 if (captured) {
                     // Check again to get the fresh URL
                     const resp = await API.getVoyageGuide(currentVoyage.id).catch(() => null);
                     if (resp) {
                         guide = resp.guide;
                         mapURL = resp.map_url;
                     }
                 }
            }

            // 3. Fetch Stops and Briefings
            const sortedStops = [...currentStops].sort((a, b) =>
                new Date(a.target_date) - new Date(b.target_date)
            );

            const briefingPromises = sortedStops.map(s => API.getBriefing(s.id).catch(() => null));
            const briefings = await Promise.all(briefingPromises);

            // 4. Determine if we should show stop briefings (only if some have been researched)
            const hasBriefings = briefings.some(b => b !== null);

            // 5. Build HTML using consolidated generator
            const html = generateReportHTML(currentVoyage, sortedStops, briefings, guide, recommendations, hasBriefings, mapURL);

            // 6. Show Modal
            const modal = document.getElementById("modal-report");
            const content = document.getElementById("report-content");
            const modalOverlay = document.getElementById("modal-overlay");

            content.innerHTML = DOMPurify.sanitize(html, { ADD_ATTR: ["target"] });
            modal.classList.remove("hidden");
            modalOverlay.classList.remove("hidden");

            // Inject Share button
            const headerControls = modal.querySelector('.modal-header-row .flex.gap-sm');
            let btnShare = document.getElementById('btn-share-report');
            if (!btnShare) {
                btnShare = document.createElement('button');
                btnShare.id = 'btn-share-report';
                btnShare.className = 'btn secondary p-xs font-sm';
                btnShare.title = 'Share Report';
                btnShare.innerHTML = '<span class="material-symbols-outlined icon-lg icon-align">share</span>';
                headerControls.insertBefore(btnShare, headerControls.firstChild);
            }
            btnShare.onclick = () => handleShareClick(guide);

            // 7. Render charts if stop briefings are included
            if (hasBriefings) {
                for (const [idx, stop] of sortedStops.entries()) {
                    const b = briefings[idx];
                    if (b && b.tides && b.tides.events) {
                        await renderTideChart(`reportTideChart_${idx}`, b.tides, stop.target_date);
                        await renderMiniTideChart(`reportMiniTideChart_${idx}`, b.tides, stop.target_date);
                    }
                }
            }

        } catch (err) {
            console.error(err);
            showNotification('Error', "Failed to generate report.");
        } finally {
            btn.disabled = false;
            btn.innerHTML = originalContent;
        }
    }

function getIconForWeather(description) {
    const d = (description || '').toLowerCase();
    if (d.includes('clear')) return 'clear_day';
    if (d.includes('partly cloudy')) return 'partly_cloudy_day';
    if (d.includes('overcast')) return 'cloud';
    if (d.includes('drizzle')) return 'weather_mix';
    if (d.includes('rain')) return 'rainy';
    return 'cloud';
}


async function handleGuideClick(voyage, button) {
    const originalContent = button.innerHTML;
    
    try {
        const resp = await API.getVoyageGuide(voyage.id);
        if (resp && resp.guide && resp.guide.summary) {
            showVoyageGuide(resp);
            return;
        }

        // Show Modal Immediately with loading state
        const modal = document.getElementById('modal-guide');
        const content = document.getElementById('guide-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = `
            <div class="loading-state">
                <span class="material-symbols-outlined spin loading-icon">sync</span>
                <p class="font-xs">Gathering local knowledge, seasonal data, and regional hazards.</p>
            </div>
        `;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        const guideRes = await API.triggerVoyageGuideResearch(voyage.id);

        // Stream progress updates into ticker
        if (guideRes && guideRes.session_id && researchTicker) {
            researchTicker.start();
            _guideProgressES = API.streamProgress(guideRes.session_id, (evt) => {
                researchTicker.push(evt.message);
            });
        }

        // Poll
        _guidePoll = setInterval(async () => {
            try {
                const resp = await API.getVoyageGuide(voyage.id);
                if (resp && resp.guide && resp.guide.summary && resp.guide.summary.length > 0) {
                    clearInterval(_guidePoll); _guidePoll = null;
                    if (_guideProgressES) { _guideProgressES.close(); _guideProgressES = null; }
                    if (researchTicker) researchTicker.stop();
                    button.innerHTML = originalContent;
                    showVoyageGuide(resp);
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);

    } catch (err) {
        console.error(err);
        clearInterval(_guidePoll); _guidePoll = null;
        if (_guideProgressES) { _guideProgressES.close(); _guideProgressES = null; }
        button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
        setTimeout(() => button.innerHTML = originalContent, 2000);
    }
}

function showVoyageGuide(resp) {
    const guide = resp.guide;
    const mapURL = resp.map_url;

    const modal = document.getElementById('modal-guide');
    const content = document.getElementById('guide-content');
    const btnRedo = document.getElementById('btn-redo-guide');
    const modalOverlay = document.getElementById('modal-overlay');

    // Inject Share/Snapshot controls if not present (create only once, update handler always)
    const headerControls = modal.querySelector('.modal-header-row .flex.gap-sm');
    
    let btnSnapshot = document.getElementById('btn-snapshot-guide');
    if (!btnSnapshot) {
        btnSnapshot = document.createElement('button');
        btnSnapshot.id = 'btn-snapshot-guide';
        btnSnapshot.className = 'btn secondary p-xs font-sm';
        btnSnapshot.title = 'Update Map Snapshot';
        btnSnapshot.innerHTML = '<span class="material-symbols-outlined icon-lg icon-align">camera_alt</span>';
        headerControls.insertBefore(btnSnapshot, headerControls.firstChild);
    }

    // Always update handler to use current guide context
    btnSnapshot.onclick = async () => {
            btnSnapshot.disabled = true;
            const icon = btnSnapshot.querySelector('span');
            icon.classList.add('spin');
            icon.textContent = 'sync';
            
            // We need to briefly hide the modal to capture the map if it's behind
            modal.classList.add('hidden');
            modalOverlay.classList.add('hidden');
            
            // Small delay to allow render
            await new Promise(r => setTimeout(r, 200));

            const success = await captureAndUploadMap(guide.voyage_id);
            
            modal.classList.remove('hidden');
            modalOverlay.classList.remove('hidden');
            
            icon.classList.remove('spin');
            icon.textContent = 'camera_alt';
            btnSnapshot.disabled = false;
            
            if (success) {
                showNotification('Snapshot Saved', 'The map view has been updated for the public report.');
                // Refresh image in modal if present
                const img = document.querySelector('#guide-content .report-map-img');
                if (img) {
                    // Cache bust
                    const src = img.src.split('?')[0];
                    img.src = `${src}?t=${Date.now()}`;
                }
            } else {
                showNotification('Error', 'Failed to capture map. Ensure the map is visible.');
            }
    };

    let html = '';
    // Map Snapshot
    if (mapURL) {
        const sep = mapURL.includes('?') ? '&' : '?';
        const url = `${mapURL}${sep}t=${Date.now()}`;
        html += `
            <div class="briefing-section">
                 <img src="${url}" alt="Voyage Map" class="report-map-img" style="width:100%; border-radius: 4px; border: 1px solid #ccc; display: block; margin-bottom: 1rem;" />
            </div>
        `;
    }

    html += generateGuideHTML(guide);

    content.innerHTML = DOMPurify.sanitize(html);

    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoGuide(guide, btnRedo);
    }

    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');
}

async function redoGuide(oldGuide, btn) {
    const content = document.getElementById('guide-content');
    content.innerHTML = `
        <div class="loading-state">
            <span class="material-symbols-outlined spin loading-icon">sync</span>
            <p><strong>Agent is researching...</strong></p>
        </div>
    `;
    btn.disabled = true;

    try {
        const redoGuideRes = await API.triggerVoyageGuideResearch(oldGuide.voyage_id);

        let redoGuideProgressES = null;
        if (redoGuideRes && redoGuideRes.session_id && researchTicker) {
            researchTicker.start();
            redoGuideProgressES = API.streamProgress(redoGuideRes.session_id, (evt) => {
                researchTicker.push(evt.message);
            });
        }

        const oldTime = new Date(oldGuide.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 60000;

        const poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                if (redoGuideProgressES) redoGuideProgressES.close();
                if (researchTicker) researchTicker.stop();
                content.innerHTML = '<div class="error-state"><p><strong>Research timed out.</strong></p></div>';
                btn.disabled = false;
                return;
            }
            try {
                const g = await API.getVoyageGuide(oldGuide.voyage_id);
                if (g) {
                    const newTime = new Date(g.created_at).getTime();
                    if (newTime > oldTime) {
                        clearInterval(poll);
                        if (redoGuideProgressES) redoGuideProgressES.close();
                        if (researchTicker) researchTicker.stop();
                        btn.disabled = false;
                        showVoyageGuide(g);
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        btn.disabled = false;
    }
}

function showNotification(title, message, actions = null) {
    const modal = document.getElementById('modal-notification');
    const modalOverlay = document.getElementById('modal-overlay');
    const titleEl = document.getElementById('notification-title');
    const msgEl = document.getElementById('notification-message');
    const actionsContainer = document.getElementById('notification-actions');
    const closeBtn = document.getElementById('btn-close-notification');

    if (modal && titleEl && msgEl) {
        titleEl.textContent = title;
        msgEl.textContent = message;
        
        // Clear previous custom actions (keep close btn)
        if (actionsContainer) {
            const customBtns = actionsContainer.querySelectorAll('.custom-action');
            customBtns.forEach(b => b.remove());

            if (actions) {
                // If it's a single action object, wrap it in an array
                const actionList = Array.isArray(actions) ? actions : [actions];
                
                // Hide Close if any action is a cancel/dismiss type, or if hideClose is set
                const hasCancelAction = actionList.some(a =>
                    a.hideClose ||
                    a.type === 'secondary' ||
                    (a.label || '').toLowerCase() === 'cancel'
                );
                if (hasCancelAction) {
                    closeBtn.classList.add('hidden');
                } else {
                    closeBtn.classList.remove('hidden');
                }

                actionList.forEach(action => {
                    const actionBtn = document.createElement('button');
                    actionBtn.className = `btn ${action.type || 'primary'} w-full custom-action`;
                    actionBtn.textContent = action.label;
                    actionBtn.onclick = () => {
                        modal.classList.add('hidden');
                        modalOverlay.classList.add('hidden');
                        if (action.callback) action.callback();
                    };
                    
                    actionsContainer.insertBefore(actionBtn, closeBtn);
                });
            } else {
                closeBtn.classList.remove('hidden');
            }
        }

        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');
    } else {
        alert(`${title}\n\n${message}`);
    }
}

async function toggleDiscoveryMode(active) {
    currentMode = active ? 'discovery' : 'planner';
    const discoveryControls = document.getElementById('discovery-controls');
    const sidebar = document.getElementById('sidebar');
    
    if (active) {
        discoveryControls.classList.remove('hidden');
        sidebar.classList.add('hidden');
        const currentMonth = new Date().getMonth() + 1;
        document.getElementById('month-slider').value = currentMonth;
        const months = [
            'January', 'February', 'March', 'April', 'May', 'June',
            'July', 'August', 'September', 'October', 'November', 'December'
        ];
        document.getElementById('month-display').textContent = months[currentMonth - 1];
        
        clearRecommendations();
        clearPilotCircle();
        clearMap(); // Clear existing markers/routes
        loadDiscoveryRegions(currentMonth);
        
        // Zoom out to world view
        if (map) {
             map.panTo({ lat: 20, lng: 0 });
             map.setZoom(3);
        }

        // Show Intro Modal if first time
        if (!localStorage.getItem('seenDiscoveryIntro')) {
            const introModal = document.getElementById('modal-discovery-intro');
            const overlay = document.getElementById('modal-overlay');
            if (introModal && overlay) {
                introModal.classList.remove('hidden');
                overlay.classList.remove('hidden');
                localStorage.setItem('seenDiscoveryIntro', 'true');
            }
        }
    } else {
        discoveryControls.classList.add('hidden');
        sidebar.classList.remove('hidden');
        
        // Remove discovery layers
        if (map && map.data) {
             map.data.forEach((feature) => {
                map.data.remove(feature);
            });
        }
        
        if (currentVoyage) {
            selectVoyage(currentVoyage); // Restore voyage view
        } else {
            showVoyageList();
        }
    }
}

async function loadDiscoveryRegions(month) {
    try {
        discoveryRegions = await API.getDiscoveryRegions(month);
        renderDiscoveryLayer();
    } catch (err) {
        console.error('Failed to load discovery regions:', err);
    }
}

async function renderDiscoveryLayer() {
    if (!map) return;

    // Clear existing data
    map.data.forEach((feature) => {
        map.data.remove(feature);
    });

    // Convert to GeoJSON
    const geojson = {
        type: 'FeatureCollection',
        features: discoveryRegions.map(r => ({
            type: 'Feature',
            geometry: {
                type: 'Polygon',
                coordinates: r.geometry.coordinates.map(ring => smoothPolygon(ring, 3))
            },
            properties: {
                type: 'discovery',
                id: r.id,
                name: r.name,
                tier: r.tier,
                suitability_score: r.suitability_score,
                is_hidden_gem: r.is_hidden_gem,
                summary: r.summary,
                avg_wind_speed_knots: r.avg_wind_speed_knots,
                avg_temp_c: r.avg_temp_c,
                deep_cut_reasoning: r.deep_cut_reasoning
            }
        }))
    };

    map.data.addGeoJson(geojson);

    // Hover effect
    map.data.addListener('mouseover', () => {
        map.setOptions({ draggableCursor: 'pointer' });
    });
    map.data.addListener('mouseout', () => {
        map.setOptions({ draggableCursor: '' });
    });
}


async function showRegionBriefing(props, month) {
    const modal = document.getElementById('modal-region-briefing');
    const title = document.getElementById('region-title');
    const content = document.getElementById('region-briefing-content');
    const overlay = document.getElementById('modal-overlay');

    // For now, use the data we already have from the list
    // In a full implementation, we might fetch detailed stats
    const region = discoveryRegions.find(r => r.id === props.id);
    
    title.textContent = props.name;
    
    content.innerHTML = DOMPurify.sanitize(`
        <div class="briefing-section">
            <div class="flex justify-between align-center mb-md">
                <span class="badge ${props.tier === 'Hidden Gem' ? 'badge-gem' : props.tier === 'Regional Favorite' ? 'badge-regional' : props.tier === 'Challenging' ? 'badge-challenging' : 'badge-standard'}">
                    ${props.tier || (props.is_hidden_gem ? 'Hidden Gem' : 'Standard Destination')}
                </span>
                <span class="font-sm text-gray">Suitability: <strong>${props.suitability_score}/100</strong></span>
            </div>
            
            <p class="mb-lg"><strong>Summary:</strong> ${props.summary}</p>
            
            ${region && region.deep_cut_reasoning ? `
                <div class="report-guide-bg p-md border-radius">
                    <h4 class="mt-0">The Deep Cut Factor</h4>
                    <p class="mb-0">${region.deep_cut_reasoning}</p>
                </div>
            ` : ''}

            <div class="weather-box mt-lg">
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th">Typical Wind</th>
                        <td class="briefing-td">${region?.avg_wind_speed_knots || '??'} knots</td>
                    </tr>
                    <tr>
                        <th class="briefing-th">Avg Temp</th>
                        <td class="briefing-td">${region?.avg_temp_c || '??'}°C (${Math.round((region?.avg_temp_c || 0) * 9/5 + 32)}°F)</td>
                    </tr>
                </table>
            </div>

            ${(currentUser && currentUser.is_admin) ? `
            <div class="mt-xl flex justify-end">
                <button id="btn-delete-region-seasonality" class="btn-text btn-danger font-sm">
                    <span class="material-symbols-outlined font-md">delete</span>
                    Remove for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}
                </button>
            </div>` : ''}
        </div>
    `);

    modal.classList.remove('hidden');
    overlay.classList.remove('hidden');

    const hide = () => {
        modal.classList.add('hidden');
        overlay.classList.add('hidden');
    };

    document.getElementById('btn-close-region-briefing').onclick = hide;
    overlay.onclick = hide;

    const btnDelete = document.getElementById('btn-delete-region-seasonality');
    if (btnDelete) {
        btnDelete.onclick = async () => {
            showNotification('Remove Seasonality', `Are you sure you want to remove ${props.name} from the discovery list for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}?`, [
                {
                    label: 'Remove',
                    type: 'danger',
                    hideClose: true,
                    callback: async () => {
                        try {
                            await API.deleteDiscoverySeasonality(props.id, month);
                            hide();
                            // Refresh discovery regions
                            loadDiscoveryRegions(month);
                        } catch (err) {
                            console.error('Failed to delete region seasonality:', err);
                            showNotification('Error', 'Failed to remove region. Please try again.');
                        }
                    }
                },
                {
                    label: 'Cancel',
                    type: 'secondary'
                }
            ]);
        };
    }
}

async function initSharedMode(token) {
    document.body.classList.add('shared-view');
    const app = document.getElementById('app');
    // Clear existing UI
    app.innerHTML = "<div class=\"loading-state\"><span class=\"material-symbols-outlined spin loading-icon\">sync</span><p>Loading Captain's Report...</p></div>";

    try {
        const resp = await API.getPublicVoyageGuide(token);
        renderSharedReport(resp, app);
    } catch (err) {
        console.error(err);
        app.innerHTML = '<div class="error-state text-center p-xl"><h2 class="text-dark">Report Not Found</h2><p>This link may have expired or is invalid.</p></div>';
    }
}

async function handleShareClick(guide) {
    if (!currentVoyage || currentVoyage.id !== guide.voyage_id) {
         showNotification('Error', 'Error: Voyage context lost.');
         return;
    }

    const content = `
        <div class="text-left">
            <h3 class="mt-0">Public Sharing</h3>
            <p class="text-gray mb-md">Share this report with friends and crew.</p>
            
            <div class="form-group">
                <label class="flex align-center gap-sm" style="cursor:pointer;">
                    <input type="checkbox" id="chk-share-public">
                    <strong>Enable Public Link</strong>
                </label>
            </div>

            <div id="share-link-container" class="hidden mt-md">
                <label>Public Link</label>
                <div class="flex gap-sm">
                    <input type="text" id="share-link-input" readonly value="" class="w-full p-sm border-radius border">
                    <button id="btn-copy-share" class="btn secondary">Copy</button>
                </div>
            </div>
            
            <div class="mt-xl text-right">
                <button id="btn-close-share" class="btn primary">Done</button>
            </div>
        </div>
    `;
    
    let shareModal = document.getElementById('modal-share-dynamic');
    if (!shareModal) {
        shareModal = document.createElement('div');
        shareModal.id = 'modal-share-dynamic';
        shareModal.className = 'modal hidden';
        document.body.appendChild(shareModal);
    }
    
    shareModal.innerHTML = DOMPurify.sanitize(content);
    
    const chk = shareModal.querySelector('#chk-share-public');
    const linkInput = shareModal.querySelector('#share-link-input');
    const linkContainer = shareModal.querySelector('#share-link-container');

    // Init State
    chk.checked = currentVoyage.is_public;
    if (currentVoyage.is_public) {
        linkInput.value = `${window.location.origin}/shared/${currentVoyage.share_token}`;
        linkContainer.classList.remove('hidden');
    }
    
    document.getElementById('modal-overlay').classList.remove('hidden');
    shareModal.classList.remove('hidden');
    
    chk.onchange = async () => {
        try {
            if (chk.checked) {
                const res = await API.enableSharing(currentVoyage.id);
                currentVoyage.is_public = true;
                currentVoyage.share_token = res.share_token;
                
                linkInput.value = `${window.location.origin}/shared/${res.share_token}`;
                linkContainer.classList.remove('hidden');
            } else {
                await API.disableSharing(currentVoyage.id);
                currentVoyage.is_public = false;
                currentVoyage.share_token = null;
                linkContainer.classList.add('hidden');
            }
        } catch (err) {
            console.error(err);
            showNotification('Error', 'Failed to update sharing settings');
            chk.checked = !chk.checked;
        }
    };
    
    shareModal.querySelector('#btn-copy-share').onclick = () => {
        const btn = shareModal.querySelector('#btn-copy-share');
        const orig = btn.textContent;
        navigator.clipboard.writeText(linkInput.value).then(() => {
            btn.textContent = 'Copied!';
            setTimeout(() => btn.textContent = orig, 2000);
        }).catch(() => {
            // Fallback for browsers without clipboard API
            linkInput.select();
            document.execCommand('copy');
            btn.textContent = 'Copied!';
            setTimeout(() => btn.textContent = orig, 2000);
        });
    };
    
    shareModal.querySelector('#btn-close-share').onclick = () => {
        shareModal.classList.add('hidden');
        if (document.getElementById('modal-guide').classList.contains('hidden') &&
            document.getElementById('modal-report').classList.contains('hidden')) {
             document.getElementById('modal-overlay').classList.add('hidden');
        }
    };
}

// --- Geometry Smoothing (Chaikin's Algorithm) ---

function smoothGeoJSON(geojson) {
    if (!geojson || !geojson.features) return geojson;
    
    geojson.features.forEach(feature => {
        if (!feature.geometry) return;
        
        const type = feature.geometry.type;
        const coords = feature.geometry.coordinates;
        
        if (type === 'Polygon') {
            feature.geometry.coordinates = coords.map(ring => smoothRing(ring));
        } else if (type === 'MultiPolygon') {
            feature.geometry.coordinates = coords.map(poly => poly.map(ring => smoothRing(ring)));
        }
    });
    
    return geojson;
}

function smoothRing(ring) {
    // Chaikin's algorithm for closed paths (iterations=3 for a natural, softened look)
    let currentRing = ring;
    for (let i = 0; i < 3; i++) {
        const nextRing = [];
        const len = currentRing.length;
        if (len < 3) return currentRing; // Cannot smooth line with < 3 points

        // We assume the ring is closed (first point == last point)
        // Process segments
        for (let j = 0; j < len - 1; j++) {
            const p0 = currentRing[j];
            const p1 = currentRing[j + 1];
            
            // Q = 0.75*P0 + 0.25*P1
            const q = [
                0.75 * p0[0] + 0.25 * p1[0],
                0.75 * p0[1] + 0.25 * p1[1]
            ];
            
            // R = 0.25*P0 + 0.75*P1
            const r = [
                0.25 * p0[0] + 0.75 * p1[0],
                0.25 * p0[1] + 0.75 * p1[1]
            ];
            
            nextRing.push(q);
            nextRing.push(r);
        }
        
        // Close the ring
        nextRing.push(nextRing[0]);
        currentRing = nextRing;
    }
    return currentRing;
}

// --- Helpers ---

function renderReferences(refs) {
    if (!refs || refs.length === 0) return '';
    return `<div class="ref-link">
        <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
    </div>`;
}

function generateGuideHTML(guide) {
    let html = '';
    
    html += `
        <div class="briefing-section">
            <h3>Overview</h3>
            <p>${guide.summary || 'No summary available.'}</p>
        </div>
    `;

    // Sailing Season
    if (guide.sailing_season) {
        const s = guide.sailing_season;
        html += `
            <div class="briefing-section">
                <h3>Sailing Season</h3>
                <table class="briefing-table">
                    <tr><th class="briefing-th">Best Months</th><td class="briefing-td">${(s.primary_season_months || []).join(', ') || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Storm Season</th><td class="briefing-td">${(s.storm_season_months || []).join(', ') || 'N/A'} (${s.storm_risk_level || 'Unknown Risk'})</td></tr>
                    <tr><th class="briefing-th">Notes</th><td class="briefing-td">${s.notes || ''} ${renderReferences(s.references)}</td></tr>
                </table>
            </div>
        `;
    }

    // Hazards
    if (guide.hazards && guide.hazards.length > 0) {
        html += `<div class="briefing-section"><h3>Regional Hazards</h3><ul class="facility-list">`;
        guide.hazards.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Info)</a>` : '';
            html += `<li class="facility-item">
                <h4>${h.title}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Security & Safety
    if (guide.security_safety && (guide.security_safety.summary || guide.security_safety.crime_report)) {
        const s = guide.security_safety;
        let tipsHtml = '';
        if (s.safety_tips && s.safety_tips.length > 0) {
            tipsHtml = `<div class="mt-sm"><strong>Safety Tips:</strong> <ul class="font-sm">${s.safety_tips.map(t => `<li>${t}</li>`).join('')}</ul></div>`;
        }
        
        const riskClass = (s.risk_level || '').toLowerCase() === 'high' ? 'text-red' : (s.risk_level || '').toLowerCase() === 'medium' ? 'text-orange' : 'text-green';

        html += `
            <div class="briefing-section">
                <h3>Security & Safety</h3>
                <p><strong>Risk Level:</strong> <span class="${riskClass} font-bold">${s.risk_level || 'Low'}</span></p>
                <p class="mt-xs">${s.summary || ''}</p>
                ${s.crime_report ? `<div class="mt-sm"><strong>Crime Report:</strong> <p class="font-sm">${s.crime_report}</p></div>` : ''}
                ${tipsHtml}
                ${renderReferences(s.references)}
            </div>
        `;
    }

    // Hubs
    if (guide.hubs && guide.hubs.length > 0) {
        html += `<div class="briefing-section"><h3>Major Hubs</h3><ul class="facility-list">`;
        guide.hubs.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Website)</a>` : '';
            html += `<li class="facility-item">
                <h4>${h.name}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }
    
    // Charter Info
    if (guide.charter_info) {
        const c = guide.charter_info;
        
        let companiesHtml = 'None listed';
        if (c.companies && c.companies.length > 0) {
             companiesHtml = '<ul class="charter-list">' + 
             c.companies.map(comp => {
                if (typeof comp === 'string') return `<li>${comp}</li>`;
                const nameLink = comp.url ? `<a href="${comp.url}" target="_blank">${comp.name}</a>` : comp.name;
                return `<li>${nameLink} ${renderReferences(comp.references)}</li>`;
             }).join('') + 
             '</ul>';
        }

        html += `
            <div class="briefing-section">
                <h3>Charter Info</h3>
                <p><strong>Available:</strong> ${c.is_charter_destination ? 'Yes' : 'No'}</p>
                <div class="mt-sm"><strong>Companies:</strong> ${companiesHtml}</div>
            </div>
        `;
    }

    // Country Info & Currency
    if (guide.country_info || guide.currencies) {
        const c = guide.country_info || {};
        const curs = guide.currencies || [];
        
        let currencyHtml = 'N/A';
        if (curs.length > 0) {
            currencyHtml = curs.map(cur => `${cur.name} (${cur.code}) - ${cur.symbol || ''}`).join(', ');
        }

        html += `
            <div class="briefing-section">
                <h3>Country & Culture</h3>
                <table class="briefing-table">
                    <tr><th class="briefing-th">Country</th><td class="briefing-td">${c.name || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Language</th><td class="briefing-td">${c.languages ? c.languages.join(', ') : 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Timezone</th><td class="briefing-td">${c.timezone || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Emergency</th><td class="briefing-td">${c.emergency_numbers ? Object.entries(c.emergency_numbers).map(([k,v]) => `${k}: ${v}`).join(', ') : 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Currency</th><td class="briefing-td">${currencyHtml}</td></tr>
                </table>
            </div>
        `;
    }

    // Airports
    if (guide.airports && guide.airports.length > 0) {
        html += `<div class="briefing-section"><h3>Nearest Airports</h3><ul class="facility-list">`;
        guide.airports.forEach(a => {
            const formattedType = a.type ? a.type.split('_').map(word => word.charAt(0).toUpperCase() + word.slice(1)).join(' ') : 'Unknown';
            html += `<li class="facility-item">
                <h4>${a.name} (${a.iata_code || 'N/A'})</h4>
                <p><strong>Type:</strong> ${formattedType}</p>
                <p><strong>Distance:</strong> ${a.distance_km ? a.distance_km + ' km' : 'Unknown'}</p>
                ${renderReferences(a.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Points of Interest
    if (guide.points_of_interest && guide.points_of_interest.length > 0) {
        html += `<div class="briefing-section"><h3>Points of Interest</h3><ul class="facility-list">`;
        guide.points_of_interest.forEach(poi => {
            const link = poi.url ? ` <a href="${poi.url}" target="_blank" class="font-sm ml-sm">(Website)</a>` : '';
            html += `<li class="facility-item">
                <h4>${poi.name}${link}</h4>
                <p>${poi.description}</p>
                ${renderReferences(poi.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    return html;
}

function generateReportHTML(voyage, stops, briefings, guide, recommendations, hasBriefings, mapURL) {
    const sortedStops = [...stops].sort((a, b) =>
        new Date(a.target_date) - new Date(b.target_date)
    );

    let dateDisplay = 'Dates Pending';
    if (voyage.start_date && voyage.end_date) {
        dateDisplay = `${new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'})} - ${new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}`;
    }

    let html = `
        <h1 class="report-title">${DOMPurify.sanitize(voyage.title)}</h1>
        <p class="report-dates text-center mb-lg">${dateDisplay}</p>
        <p class="report-location text-center mb-lg"><strong>Area:</strong> ${DOMPurify.sanitize(displayLocationName(voyage.location_name))}</p>
        <hr />
    `;

    // Map Snapshot (Show if present, even if no guide)
    if (mapURL) {
        // Cache bust
        const sep = mapURL.includes('?') ? '&' : '?';
        const url = `${mapURL}${sep}t=${Date.now()}`;
        html += `
            <div class="report-section-wrapper">
                 <img src="${url}" alt="Voyage Map" class="report-map-img" style="width:100%; border-radius: 4px; border: 1px solid #ccc; display: block; margin-bottom: 2rem;" />
            </div>
            <hr />
        `;
    }

    // --- Voyage Overview (Consolidated View) ---
    if (hasBriefings) {
        const gridCols = Math.min(sortedStops.length, 4);
        html += `<div class="report-section-wrapper">
            <h2 class="report-day-header brand-blue">Voyage Overview</h2>
            <div class="overview-grid grid-cols-${gridCols}">
        `;

        sortedStops.forEach((stop, idx) => {
            const briefing = briefings.find(br => br.stop_id === stop.id) || {};
            const date = new Date(stop.target_date);
            const dateStr = date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });

            // Weather
            const w = briefing.weather_summary || {};
            const weatherIcon = getIconForWeather(w.condition);
            const temp = (w.temp_max_f && w.temp_min_f) ? `${Math.round(w.temp_max_f)}° / ${Math.round(w.temp_min_f)}°` : '--';

            // Sun
            const sun = briefing.sun_phase || {};
            const formatSunTime = (t) => {
                if (!t) return '--:--';
                const d = new Date(t);
                return isNaN(d.getTime()) ? t : d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
            };
            const sunrise = formatSunTime(sun.sunrise);
            const sunset = formatSunTime(sun.sunset);

            const canvasId = `reportMiniTideChart_${idx}`;

            html += `
                <div class="overview-card">
                    <div class="overview-date">
                        ${dateStr}
                    </div>
                    <div class="overview-location" title="${DOMPurify.sanitize(stop.location_name)}">
                        ${DOMPurify.sanitize(displayLocationName(stop.location_name).split(',')[0].trim())}
                    </div>

                    <div class="overview-weather">
                        <span class="material-symbols-outlined" style="font-size: 20px; color: #555;">${weatherIcon}</span>
                        <span class="overview-temp">${temp}</span>
                    </div>

                    <div class="overview-sun">
                        <div title="Sunrise"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">wb_twilight</span> ${sunrise}</div>
                        <div title="Sunset"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">bedtime</span> ${sunset}</div>
                    </div>

                    <div class="overview-chart">
                        <canvas id="${canvasId}" data-tide-json='${JSON.stringify(briefing.tides || {}).replace(/'/g, "&apos;")}' data-date="${stop.target_date}"></canvas>
                    </div>
                </div>
            `;
        });
        html += `</div></div><hr />`;
    }

    // --- Destination Guide ---
    if (guide) {
        html += `
            <div class="report-section-wrapper report-guide-bg">
                <h2 class="report-day-header brand-green">Destination Guide</h2>
                ${generateGuideHTML(guide)}
            </div>
            <hr />
        `;
    }

    // --- Recommendations ---
    // Only show if we don't have specific stop briefings (Discovery Mode vs Planning Mode)
    if (!hasBriefings && recommendations && recommendations.length > 0) {
        html += `<div class="report-section-wrapper">
                    <h2 class="report-day-header brand-blue">Resource Hubs & Recommended Spots</h2>`;

        // Group by normalized type
        const groups = {
            marina: { title: 'Resource Hubs & Marinas', items: [] },
            anchorage: { title: 'Recommended Anchorages', items: [] },
            mooring: { title: 'Mooring Fields', items: [] },
            other: { title: 'Other Recommendations', items: [] }
        };

        recommendations.forEach(rec => {
            const t = (rec.type || '').toLowerCase();
            if (t.includes('marina') || t.includes('hub')) groups.marina.items.push(rec);
            else if (t.includes('anchor')) groups.anchorage.items.push(rec);
            else if (t.includes('mooring')) groups.mooring.items.push(rec);
            else groups.other.items.push(rec);
        });

        // Render in specific order
        ['marina', 'anchorage', 'mooring', 'other'].forEach(key => {
            const group = groups[key];
            if (group.items.length === 0) return;

            html += `<div class="recommendation-group mb-xl">
                        <h3 class="group-header border-b pb-xs mb-md text-brand-medium">${group.title}</h3>
                        <div class="recommendations-list">`;

            group.items.forEach(rec => {
                const type = rec.type || 'Spot';
                const recColor = markerColor(type);
                const tl = type.toLowerCase();
                let recIcon = 'location_on';
                if (tl.includes('anchor')) recIcon = 'anchor';
                else if (tl.includes('moor')) recIcon = 'crisis_alert';
                else if (tl.includes('hub') || tl.includes('marina')) recIcon = 'hub';

                html += `
                    <div class="recommendation-item mb-lg p-md border-radius border">
                        <h4 class="m-0 mb-sm briefing-header-icon">
                            <span class="material-symbols-outlined icon-lg" style="color:${recColor};">${recIcon}</span>
                            ${DOMPurify.sanitize(rec.name)}
                            <span style="font-size:0.75rem;font-weight:900;text-transform:uppercase;letter-spacing:1px;color:#fff;background:${recColor};padding:2px 8px;border-radius:20px;margin-left:auto;">${DOMPurify.sanitize(type)}</span>
                        </h4>
                        <p class="mb-sm">${DOMPurify.sanitize(rec.description)}</p>
                        <div style="background:${recColor}1A;padding:10px 12px;border-radius:6px;border-left:3px solid ${recColor};margin-top:8px;">
                            <p style="margin:0;font-size:0.85rem;font-style:italic;color:#555;">
                                <strong style="font-style:normal;color:${recColor};">Pilot's Reasoning:</strong> "${DOMPurify.sanitize(rec.reasoning)}"
                            </p>
                        </div>
                    </div>
                `;
            });

            html += `</div></div>`;
        });

        html += `</div><hr />`;
    }

    // --- Daily Itinerary ---
    if (hasBriefings) {
        sortedStops.forEach((stop, idx) => {
            const b = briefings.find(br => br.stop_id === stop.id);
            if (!b) return;

            const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, {timeZone: 'UTC', weekday: 'long', month: 'long', day: 'numeric'});

            html += `
                <div class="report-daily-wrapper">
                    <h2 class="report-day-header">Day ${idx + 1}: ${DOMPurify.sanitize(displayLocationName(stop.location_name))}</h2>
                    <p class="report-day-date"><strong>Date:</strong> ${dateStr}</p>
            `;

            const isInvalid = (v) => {
                if (!v) return true;
                const sv = String(v).toLowerCase().trim();
                return sv === 'n/a' || sv === 'unknown' || sv === 'not specified';
            };

            // Weather
            const w = b.weather_summary || {};
            if (w && !isInvalid(w.condition)) {
                const weatherIcon = getIconForWeather(w.condition);
                html += `
                    <div class="briefing-section">
                        <h3>Weather Outlook</h3>
                        <div class="flex align-center gap-md">
                            <span class="material-symbols-outlined" style="font-size: 48px; color: var(--brand-dark);">${weatherIcon}</span>
                            <div>
                                <p class="m-0"><strong>Condition:</strong> ${w.condition}</p>
                                ${(w.temp_max_f != null && w.temp_min_f != null) ? `<p class="m-0"><strong>Temperature:</strong> ${Math.round(w.temp_max_f)}°F / ${Math.round(w.temp_min_f)}°F</p>` : ''}
                                ${w.precip_prob != null ? `<p class="m-0"><strong>Precipitation:</strong> ${w.precip_prob}%</p>` : ''}
                            </div>
                        </div>
                    </div>
                `;
            }

            // Tides
            if (b.tides && b.tides.events) {
                html += `
                    <div class="briefing-section">
                        <h3>Tides & Currents</h3>
                        <div style="height: 300px; margin-bottom: 1rem;">
                            <canvas id="reportTideChart_${idx}"></canvas>
                        </div>
                    </div>
                `;
            }

            // Facilities — skip for the last stop if it's within 1 NM of the first stop (return voyage)
            const firstStop = sortedStops[0];
            const isLastStop = idx === sortedStops.length - 1 && sortedStops.length > 1;
            const isReturnStop = isLastStop && (() => {
                const toRad = d => d * Math.PI / 180;
                const R = 3440.065; // nautical miles
                const dLat = toRad(stop.latitude - firstStop.latitude);
                const dLon = toRad(stop.longitude - firstStop.longitude);
                const a = Math.sin(dLat/2)**2 + Math.cos(toRad(firstStop.latitude)) * Math.cos(toRad(stop.latitude)) * Math.sin(dLon/2)**2;
                return 2 * R * Math.asin(Math.sqrt(a)) < 1;
            })();

            if (isReturnStop) {
                html += `<div class="briefing-section"><p class="text-gray italic">Local facilities omitted — this stop returns to the voyage's starting area.</p></div>`;
            } else if (b.facilities && b.facilities.length > 0) {
                html += `<div class="briefing-section"><h3>Local Facilities</h3><ul class="facility-list">`;
                b.facilities.forEach(f => {
                    const name = DOMPurify.sanitize(f.name);

                    const isInvalid = v => !v || ['n/a', '', 'unknown', 'not specified'].includes(String(v).toLowerCase().trim());

                    let metaRows = '';
                    if (f.address) metaRows += `
                        <tr><th class="briefing-th capitalize" style="text-align:left;background:#f9f9f9;width:120px;">Address</th>
                        <td class="briefing-td">${DOMPurify.sanitize(f.address)}</td></tr>`;
                    if (f.rating) {
                        const stars = '★'.repeat(Math.round(f.rating)) + '☆'.repeat(5 - Math.round(f.rating));
                        const count = f.user_rating_count ? ` (${f.user_rating_count.toLocaleString()} reviews)` : '';
                        metaRows += `
                        <tr><th class="briefing-th capitalize" style="text-align:left;background:#f9f9f9;width:120px;">Rating</th>
                        <td class="briefing-td"><span style="color:#F9A825;">${stars}</span> ${f.rating.toFixed(1)}${count}</td></tr>`;
                    }
                    if (f.business_status && f.business_status !== 'OPERATIONAL') metaRows += `
                        <tr><th class="briefing-th capitalize" style="text-align:left;background:#f9f9f9;width:120px;">Status</th>
                        <td class="briefing-td" style="color:#C62828;font-weight:700;">${f.business_status.replace(/_/g, ' ')}</td></tr>`;
                    if (f.website) metaRows += `
                        <tr><th class="briefing-th capitalize" style="text-align:left;background:#f9f9f9;width:120px;">Website</th>
                        <td class="briefing-td"><a href="${f.website}" target="_blank">${DOMPurify.sanitize(f.website)}</a></td></tr>`;

                    let detailRows = '';
                    if (f.details && typeof f.details === 'object') {
                        detailRows = Object.entries(f.details)
                            .filter(([_, v]) => !isInvalid(v))
                            .map(([k, v]) => `
                                <tr><th class="briefing-th capitalize" style="text-align:left;background:#f9f9f9;width:120px;">${k.replace(/_/g, ' ')}</th>
                                <td class="briefing-td">${DOMPurify.sanitize(String(v))}</td></tr>
                            `).join('');
                    }

                    const allRows = metaRows + detailRows;
                    const detailsHtml = allRows
                        ? `<table class="briefing-table mt-sm" style="width:100%;font-size:0.85em;">${allRows}</table>`
                        : '';

                    const typeColor = markerColor(f.type);
                    const typeLowerR = (f.type || '').toLowerCase();
                    let iconR = 'place';
                    if (typeLowerR.includes('anchorage')) iconR = 'anchor';
                    else if (typeLowerR.includes('marina')) iconR = 'storefront';
                    else if (typeLowerR.includes('mooring')) iconR = 'crisis_alert';
                    else if (typeLowerR.includes('bar')) iconR = 'local_bar';
                    else if (typeLowerR.includes('restaurant')) iconR = 'restaurant';

                    html += `<li class="facility-item mb-xl">
                        <h4 class="mb-xs briefing-header-icon">
                            <span class="material-symbols-outlined icon-lg" style="color:${typeColor};">${iconR}</span>
                            ${name}
                            <span style="font-size:0.75rem;font-weight:900;text-transform:uppercase;letter-spacing:1px;color:#fff;background:${typeColor};padding:2px 8px;border-radius:20px;margin-left:auto;">${DOMPurify.sanitize(f.type || 'Facility')}</span>
                        </h4>
                        ${detailsHtml}
                        ${renderReferences(f.references)}
                    </li>`;
                });
                html += `</ul></div>`;
            }

            html += `</div><hr />`;
        });
    }

    html += `
        <footer class="mt-xl text-center text-gray font-sm p-lg">
            <p>Generated by NavalPlan</p>
        </footer>
    `;

    return html;
}

function renderSharedReport(data, container) {
    const guide = data.guide || {};
    const voyage = data.voyage || {};
    const stops = data.stops || [];
    const briefings = data.briefings || [];
    const recommendations = data.recommendations || [];

    const hasBriefings = briefings.some(b => b !== null);
    const mapURL = data.map_url;

    const html = generateReportHTML(voyage, stops, briefings, guide, recommendations, hasBriefings, mapURL);

    // Inject and Render - Wrapped in a container for styling (max-width etc)
    container.innerHTML = DOMPurify.sanitize(`<div class="shared-report-content">${html}</div>`, { ADD_ATTR: ['target'] });

    // ... (Chart rendering logic preserved below) ...
    setTimeout(() => {
        const sortedStops = [...stops].sort((a, b) => new Date(a.target_date) - new Date(b.target_date));

        sortedStops.forEach((stop, idx) => {
            const b = briefings.find(br => br.stop_id === stop.id) || {};
            // Main Chart
            if (b.tides && b.tides.events) {
                renderTideChart(`reportTideChart_${idx}`, b.tides, stop.target_date);
            }
            // Mini Chart
            if (b.tides && b.tides.events) {
                renderMiniTideChart(`reportMiniTideChart_${idx}`, b.tides, stop.target_date);
            }
        });
    }, 100);
}
let currentAdminPage = 1;
const ADMIN_PAGE_LIMIT = 20;

function initAdminUI() {
    // Listen for Admin Button (delegation)
    document.addEventListener('click', async (e) => {
        const btn = e.target.closest('#btn-open-admin');
        if (btn) {
            const modal = document.getElementById('modal-admin');
            const overlay = document.getElementById('modal-overlay');
            if (modal && overlay) {
                modal.classList.remove('hidden');
                overlay.classList.remove('hidden');
                currentAdminPage = 1;
                loadAdminUsers();
            }
        }
    });

    // Close Handler
    const btnClose = document.getElementById('btn-close-admin');
    if (btnClose) {
        btnClose.addEventListener('click', () => {
             document.getElementById('modal-admin').classList.add('hidden');
             document.getElementById('modal-overlay').classList.add('hidden');
        });
    }

    // Pagination Handlers
    const btnPrev = document.getElementById('btn-admin-prev');
    const btnNext = document.getElementById('btn-admin-next');
    
    if (btnPrev) {
        btnPrev.addEventListener('click', () => {
            if (currentAdminPage > 1) {
                currentAdminPage--;
                loadAdminUsers();
            }
        });
    }

    if (btnNext) {
        btnNext.addEventListener('click', () => {
            currentAdminPage++;
            loadAdminUsers();
        });
    }

    // Invite Form
    const formInvite = document.getElementById('form-invite-user');
    if (formInvite) {
        formInvite.addEventListener('submit', async (e) => {
            e.preventDefault();
            const input = document.getElementById('invite-email');
            const email = input.value;
            const btn = formInvite.querySelector('button');
            const originalText = btn.textContent;
            
            btn.disabled = true;
            btn.textContent = 'Inviting...';

            try {
                await API.inviteUser(email);
                input.value = '';
                loadAdminUsers();
                showNotification('User Invited', `${email} has been added to the allowlist.`);
            } catch (err) {
                console.error(err);
                showNotification('Error', 'Failed to invite user');
            } finally {
                btn.disabled = false;
                btn.textContent = originalText;
            }
        });
    }
}

async function loadAdminUsers() {
    const tbody = document.getElementById('admin-users-list');
    const btnPrev = document.getElementById('btn-admin-prev');
    const btnNext = document.getElementById('btn-admin-next');
    const pageDisplay = document.getElementById('admin-page-display');

    tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center">Loading...</td></tr>';
    
    try {
        const data = await API.listAdminUsers(currentAdminPage, ADMIN_PAGE_LIMIT);
        const users = data.users.data || [];
        const total = data.users.total || 0;
        const invites = data.invites || [];
        
        tbody.innerHTML = '';
        
        // Show Invites First (if on page 1)
        if (currentAdminPage === 1 && invites.length > 0) {
            invites.forEach(i => {
                const tr = document.createElement('tr');
                tr.className = 'border-b bg-gray-light';
                const safeEmail = DOMPurify.sanitize(i.email);
                tr.innerHTML = `
                    <td class="p-sm">${safeEmail}</td>
                    <td class="p-sm"><span class="badge badge-standard">Pending Invite</span></td>
                    <td class="p-sm"><button class="btn-text btn-danger font-sm p-0" onclick="revokeInvite('${safeEmail}')">Revoke</button></td>
                `;
                tbody.appendChild(tr);
            });
        }

        if (users.length === 0 && invites.length === 0) {
            tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center">No users found</td></tr>';
        } else {
            users.forEach(u => {
                const tr = document.createElement('tr');
                tr.className = 'border-b';
                
                let status = '<span class="badge badge-regional">Active User</span>';
                if (u.is_admin) status += ' <span class="badge badge-gem">Admin</span>';
                
                const safeEmail = DOMPurify.sanitize(u.email);
                tr.innerHTML = `
                    <td class="p-sm truncate" title="${safeEmail}">${safeEmail}</td>
                    <td class="p-sm">${status}</td>
                    <td class="p-sm"><span class="text-gray font-sm">-</span></td>
                `;
                tbody.appendChild(tr);
            });
        }

        // Update Pagination Controls
        const totalPages = Math.ceil(total / ADMIN_PAGE_LIMIT);
        pageDisplay.textContent = `Page ${currentAdminPage} of ${totalPages || 1}`;
        
        if (btnPrev) btnPrev.disabled = currentAdminPage === 1;
        if (btnNext) btnNext.disabled = currentAdminPage >= totalPages;
        
    } catch (err) {
        console.error(err);
        tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center error-text">Failed to load users</td></tr>';
    }
}

// Make global for onclick handler
window.revokeInvite = async (email) => {
    showNotification('Revoke Invitation', `Are you sure you want to revoke the invitation for ${email}?`, [
        {
            label: 'Revoke',
            type: 'danger',
            hideClose: true,
            callback: async () => {
                try {
                    await API.revokeInvitation(email);
                    loadAdminUsers();
                } catch (err) {
                    console.error(err);
                    showNotification('Error', 'Failed to revoke invitation');
                }
            }
        },
        {
            label: 'Cancel',
            type: 'secondary'
        }
    ]);
};

function initOnboarding() {
  const hasSeenFirstTrip = localStorage.getItem('seenFirstTrip');
  const modalFirstTrip = document.getElementById('modal-first-trip');
  const btnCloseFirstTrip = document.getElementById('btn-close-first-trip');
  const modalOverlay = document.getElementById('modal-overlay');

  if (!hasSeenFirstTrip && modalFirstTrip) {
      setTimeout(() => {
          modalFirstTrip.classList.remove('hidden');
          modalOverlay.classList.remove('hidden');
      }, 1000);
      
      if (btnCloseFirstTrip) {
          btnCloseFirstTrip.addEventListener('click', () => {
              modalFirstTrip.classList.add('hidden');
              modalOverlay.classList.add('hidden');
              localStorage.setItem('seenFirstTrip', 'true');
          });
      }
  }
}
