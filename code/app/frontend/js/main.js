import DOMPurify from 'dompurify';
import { API, API_BASE, set503Callback } from './api.js';
import { checkSession, currentUser } from './auth.js';
import { importLibrary, setOptions } from '@googlemaps/js-api-loader';
import { loadTheme, toggleTheme, currentTheme } from './theme.js';
import { MapPin } from './ui/MapPin.js';
import { DataTile } from './ui/DataTile.js';
import { Stepper } from './ui/Stepper.js';
import { ScoreRing } from './ui/ScoreRing.js';
import { LookoutBox } from './ui/LookoutBox.js';

import { announce, displayLocationName, ensureRecommendationsArray, esc, renderReferences } from './utils.js';
import { MARKER_ACCENTS, MARKER_ICONS, markerAccent, tokenColor, markerColor, accentDot, getIconForWeather, directionToDegrees, getWindScale, getWindArrowSVG } from './tokens.js';
import { hashString, smoothPolygon, chaikin, getCirclePolygon } from './geometry.js';
import { getRadarSweepClass } from './animations/RadarSweep.js';
import { getSearchRingClass } from './animations/SearchRing.js';
import { showNotification } from './notifications.js';
import { initAdminUI } from './admin.js';

const GOOGLE_MAPS_API_KEY = __GOOGLE_MAPS_API_KEY__;
setOptions({ key: GOOGLE_MAPS_API_KEY, version: 'weekly' });

// Pre-warm large library bundles so they're ready when first needed.
importLibrary('maps');

let ChartLib = null;
async function loadChart() {
    if (ChartLib) return ChartLib;
    ChartLib = (await import('chart.js/auto')).default;
    return ChartLib;
}
loadChart();

// ─── Shared State ────────────────────────────────────────────────────────────
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

// ─── Sweep Manager ────────────────────────────────────────────────────────────
const STOP_SWEEP_RADIUS_M = 9260; // 5 nautical miles
const stopSweepInstances = new Map(); // stopId → RadarSweep instance
let stopSweepQueue = [];
let _routeLineAnimInterval = null;

async function startStopSweepSequence(stops) {
    clearStopSweeps();
    stopSweepQueue = [...stops];
    markers.forEach(m => m.map = null);

    // Launch one continuous sweep per stop simultaneously
    const Cls = await getRadarSweepClass();
    for (const stop of stopSweepQueue) {
        const inst = new Cls(map, { lat: stop.latitude, lng: stop.longitude }, STOP_SWEEP_RADIUS_M);
        stopSweepInstances.set(stop.id, inst);
    }

    // Animated outer ring around the full voyage search area
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

    // Marching-ants route line
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
    // Stop and remove just this stop's sweep synchronously — no async race
    const inst = stopSweepInstances.get(stopId);
    if (inst) { inst.stop(); stopSweepInstances.delete(stopId); }
    stopSweepQueue = stopSweepQueue.filter(s => s.id !== stopId);
    if (stopSweepQueue.length === 0) clearStopSweeps();
}

function clearStopSweeps() {
    stopSweepInstances.forEach(inst => inst.stop());
    stopSweepInstances.clear();
    stopSweepQueue = [];
    if (searchRing) { searchRing.stop(); searchRing = null; }
    if (_routeLineAnimInterval) {
        clearInterval(_routeLineAnimInterval);
        _routeLineAnimInterval = null;
        if (routePolyline) {
            routePolyline.set('icons', [{
                icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
                offset: '0',
                repeat: '20px'
            }]);
        }
    }
    markers.forEach(m => m.map = map);
}

let isResearchAllRunning = false;
let activeInfoWindow = null;
let lastKnownItineraryFull = false;
let lastKnownResearchDone = false;
// Briefing cache keyed by stop_id — populated on demand, cleared on stop delete
const briefingCache = new Map();
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

// ─── Pagination State ─────────────────────────────────────────────────────────
let currentVoyagePage = 1;
const VOYAGE_PAGE_LIMIT = 20;
let currentStopPage = 1;
const STOP_PAGE_LIMIT = 50;


// ─── App Init & Routing ───────────────────────────────────────────────────────

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

  const path = window.location.pathname;

  // Shared/Public View Handler
  if (path.startsWith('/shared/')) {
      const token = path.replace('/shared/', '');
      if (token) {
          initSharedMode(token);
          return;
      }
  }

  // Print View Handler
  const printMatch = path.match(/^\/voyages\/(\d+)\/print$/);
  if (printMatch) {
      initPrintMode(parseInt(printMatch[1], 10));
      return;
  }

  checkSession();
  initMap();
  initUI();
  initAdminUI();
  initOnboarding();

  // Back button handling
  window.addEventListener('popstate', async (e) => {
      handleRoute(window.location.pathname, false);
  });

  // Load data and handle initial route
  loadVoyages().then(async () => {
      handleRoute(path, false);
  });

  startHealthCheck();
}

async function handleRoute(path, doPushState = true) {
    if (path === '/' || path === '/index.html' || path === '') {
        showVoyageList(doPushState);
        return;
    }

    const parts = path.split('/').filter(p => p !== '');
    // Expect: ['voyages', '{id}', ...]
    if (parts[0] === 'voyages' && parts[1]) {
        const id = parseInt(parts[1]);
        if (isNaN(id)) return;

        // Ensure voyage is selected
        if (!currentVoyage || currentVoyage.id !== id) {
            const v = voyages.find(v => v.id === id) || { id };
            await selectVoyage(v, doPushState);
        }

        // Handle sub-views
        if (parts[2] === 'guide') {
            const btn = document.getElementById('btn-view-guide');
            handleGuideClick(currentVoyage, btn, doPushState);
        } else if (parts[2] === 'report') {
            handleShowReport(doPushState);
        } else if (parts[2] === 'stops' && parts[3]) {
            const stopId = parseInt(parts[3]);
            if (!isNaN(stopId)) {
                // Fetch briefing and show
                try {
                    const b = await API.getBriefing(stopId);
                    showBriefing(b, doPushState);
                } catch (e) {
                    console.error("Failed to load briefing for deep link", e);
                }
            }
        } else {
            // Just the voyage view, close any modals
            closeAllModals();
        }
    }
}

function closeAllModals() {
    document.querySelectorAll('.modal').forEach(m => m.classList.add('hidden'));
    document.getElementById('modal-overlay').classList.add('hidden');
}

function startHealthCheck() {    const warning = document.getElementById('health-warning');
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
    loadTheme();
    initThemeToggle();
    initPilotAndFilterListeners();
    initDiscoveryListeners();
    initMobileMenuListeners();
    initVoyageModalListeners();
    initNavigationListeners();
    initModalCloseListeners();
    initAdminUI();
}

function initThemeToggle() {
    // btn-theme-toggle is injected by auth.js after session check,
    // so listen on the container with event delegation.
    const container = document.getElementById('auth-container');
    if (!container) return;
    container.addEventListener('click', (e) => {
        if (e.target.closest('#btn-theme-toggle')) {
            const next = toggleTheme();
            // Update button state without reload for fast feedback
            const btn = document.getElementById('btn-theme-toggle');
            if (btn) {
                btn.innerHTML = `<span class="material-symbols-outlined">${next === 'dark' ? 'light_mode' : 'dark_mode'}</span>`;
                btn.setAttribute('aria-pressed', next === 'dark' ? 'true' : 'false');
            }
            // colorScheme is immutable after Map construction — reload so
            // initMap picks up the new theme with the correct colorScheme.
            window.location.reload();
        }
    });
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

// Track selected discovery month (1-based)
let currentDiscoveryMonth = new Date().getMonth() + 1;

function initDiscoveryListeners() {
    const btnDiscover = document.getElementById('btn-discover');
    const btnCloseDiscovery = document.getElementById('btn-close-discovery');

    if (btnDiscover) btnDiscover.addEventListener('click', () => toggleDiscoveryMode(true));
    if (btnCloseDiscovery) btnCloseDiscovery.addEventListener('click', () => toggleDiscoveryMode(false));

    const btnCloseDiscoveryIntro = document.getElementById('btn-close-discovery-intro');
    if (btnCloseDiscoveryIntro) {
        btnCloseDiscoveryIntro.addEventListener('click', () => {
            document.getElementById('modal-discovery-intro').classList.add('hidden');
            document.getElementById('modal-overlay').classList.add('hidden');
        });
    }

    // Build 12-square month picker
    const squaresContainer = document.getElementById('month-squares');
    if (squaresContainer) {
        const MONTH_ABBR = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
        MONTH_ABBR.forEach((abbr, i) => {
            const num = i + 1;
            const sq = document.createElement('button');
            sq.type = 'button';
            sq.className = 'np-month-sq';
            sq.dataset.month = num;
            sq.setAttribute('aria-label', abbr);
            sq.setAttribute('aria-pressed', String(num === currentDiscoveryMonth));
            sq.textContent = abbr;
            if (num === currentDiscoveryMonth) sq.classList.add('active');
            sq.addEventListener('click', () => {
                currentDiscoveryMonth = num;
                squaresContainer.querySelectorAll('.np-month-sq').forEach(b => {
                    const active = parseInt(b.dataset.month) === num;
                    b.classList.toggle('active', active);
                    b.setAttribute('aria-pressed', String(active));
                });
                loadDiscoveryRegions(num);
            });
            squaresContainer.appendChild(sq);
        });
    }
}

function initMobileMenuListeners() {
    const appContainer = document.getElementById('app');
    const btnMobileMenu = document.getElementById('btn-mobile-menu');
    const btnCloseSidebar = document.getElementById('btn-close-sidebar');

    if (btnMobileMenu) btnMobileMenu.addEventListener('click', () => appContainer.classList.add('menu-open'));
    if (btnCloseSidebar) btnCloseSidebar.addEventListener('click', () => appContainer.classList.remove('menu-open'));

    // Bottom tab bar
    const tabBar = document.getElementById('mobile-tab-bar');
    if (!tabBar) return;

    function setActiveTab(name) {
        tabBar.querySelectorAll('.np-tab').forEach(t => {
            const isActive = t.dataset.tab === name;
            t.setAttribute('aria-pressed', String(isActive));
            t.classList.toggle('active', isActive);
        });
    }

    document.getElementById('tab-map')?.addEventListener('click', () => {
        appContainer.classList.remove('menu-open');
        setActiveTab('map');
    });

    document.getElementById('tab-plan')?.addEventListener('click', () => {
        appContainer.classList.add('menu-open');
        setActiveTab('plan');
    });

    document.getElementById('tab-guide')?.addEventListener('click', () => {
        appContainer.classList.remove('menu-open');
        setActiveTab('guide');
        if (currentVoyage) {
            const btn = document.getElementById('btn-view-guide');
            handleGuideClick(currentVoyage, btn || document.getElementById('tab-guide'));
        }
    });

    document.getElementById('tab-profile')?.addEventListener('click', () => {
        appContainer.classList.remove('menu-open');
        setActiveTab('profile');
        // Show auth container info — toggle a small popover
        const authContainer = document.getElementById('auth-container');
        if (authContainer) authContainer.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    });

    // Sync active tab when sidebar opens/closes via other means
    const observer = new MutationObserver(() => {
        const isOpen = appContainer.classList.contains('menu-open');
        if (isOpen) setActiveTab('plan');
        else {
            // Only reset if no modal is open
            const anyModal = document.querySelector('.modal:not(.hidden)');
            if (!anyModal) setActiveTab('map');
        }
    });
    observer.observe(appContainer, { attributes: true, attributeFilter: ['class'] });
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
    const inputRadius = document.getElementById('voyage-radius');
    const radiusDisplay = document.getElementById('voyage-radius-display');
    const modalTitle = modalNewVoyage.querySelector('h2');
    const submitBtn = formNewVoyage.querySelector('button[type="submit"]');

    inputRadius.addEventListener('input', () => { radiusDisplay.textContent = inputRadius.value; });

    // Open Modal (Create Mode)
    btnNewVoyage.addEventListener('click', () => {
        editingVoyageId = null;
        modalTitle.textContent = 'Plan a New Voyage';
        submitBtn.textContent = 'Create Voyage';
        document.getElementById('modal-voyage-pill').textContent = 'NEW VOYAGE';
        modalOverlay.classList.remove('hidden');
        modalNewVoyage.classList.remove('hidden');
        // Hide date fields for initial creation (Discovery First)
        document.getElementById('voyage-date-fields').classList.add('hidden');
        document.getElementById('voyage-start').value = '';
        document.getElementById('voyage-end').value = '';
        document.getElementById('voyage-title').value = '';
        document.getElementById('voyage-location-name').value = '';
        document.getElementById('voyage-radius').value = 60;
        document.getElementById('voyage-radius-display').textContent = 60;
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
            radius = Math.max(1, Math.min(150, radius));
            const inputRadius = document.getElementById('voyage-radius');
            if (inputRadius) {
                inputRadius.value = radius;
                document.getElementById('voyage-radius-display').textContent = radius;
            }

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
    const btnPrintVoyage = document.getElementById('btn-print-voyage');
    if (btnCloseReport) btnCloseReport.onclick = closeReport;
    if (btnCopyReport) btnCopyReport.onclick = handleCopyReport;
    if (btnPrintVoyage) btnPrintVoyage.addEventListener('click', () => {
        if (currentVoyage) window.open(`/voyages/${currentVoyage.id}/print`, '_blank');
    });

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

    // 2d. Convert grid/flex layouts to tables for Google Docs compatibility
    const gridLayouts = [];
    const convertGridToTable = (selector, colOverride = null) => {
        content.querySelectorAll(selector).forEach(container => {
            const children = Array.from(container.children);
            if (!children.length) return;
            const cols = colOverride ?? (() => {
                const tpl = window.getComputedStyle(container).gridTemplateColumns;
                return tpl ? tpl.trim().split(/\s+/).length : children.length;
            })();
            const table = document.createElement('table');
            table.style.cssText = 'width:100%;border-collapse:separate;border-spacing:6px;margin:8px 0';
            let row = null;
            children.forEach((child, i) => {
                if (i % cols === 0) { row = document.createElement('tr'); table.appendChild(row); }
                const td = document.createElement('td');
                const cs = window.getComputedStyle(child);
                const bg = cs.getPropertyValue('background-color');
                if (bg && bg !== 'rgba(0, 0, 0, 0)') td.style.backgroundColor = bg;
                td.style.borderRadius = cs.getPropertyValue('border-top-left-radius');
                td.style.padding = [
                    cs.getPropertyValue('padding-top'), cs.getPropertyValue('padding-right'),
                    cs.getPropertyValue('padding-bottom'), cs.getPropertyValue('padding-left'),
                ].join(' ');
                td.style.verticalAlign = 'top';
                td.style.width = `${Math.floor(100 / cols)}%`;
                td.appendChild(child);
                row.appendChild(td);
            });
            // Pad last row with borderless empty cells
            if (row) {
                while (row.children.length < cols) {
                    const empty = document.createElement('td');
                    empty.style.border = 'none';
                    row.appendChild(empty);
                }
            }
            container.parentNode.insertBefore(table, container);
            container.style.display = 'none';
            gridLayouts.push({ container, table, children });
        });
    };
    convertGridToTable('.np-metric-grid');
    convertGridToTable('.np-day-grid');
    convertGridToTable('.np-weather-grid');
    convertGridToTable('.np-wind-forecast', 4);
    convertGridToTable('.np-report-hero', 2);
    convertGridToTable('.np-guide-grid', 2);

    // 2e. Replace circular stop number badges — they render as full-width colored bars in Docs
    const stopNumberData = [];
    content.querySelectorAll('.np-report-stop').forEach(stopEl => {
        const numEl = stopEl.querySelector('.np-report-stop__number');
        const titleEl = stopEl.querySelector('.np-report-stop__title');
        if (numEl && titleEl) {
            stopNumberData.push({ numEl, titleEl, origTitle: titleEl.textContent });
            numEl.style.display = 'none';
            titleEl.textContent = `Stop ${numEl.textContent.trim()}: ${titleEl.textContent}`;
        }
    });

    // 3. Inline computed styles so Google Docs preserves colors, weights, and backgrounds
    const INLINE_PROPS = [
        'color', 'background-color',
        'font-family', 'font-size', 'font-weight', 'font-style',
        'text-transform', 'letter-spacing', 'line-height', 'text-align',
        'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
        'border-top-width', 'border-top-style', 'border-top-color',
        'border-right-width', 'border-right-style', 'border-right-color',
        'border-bottom-width', 'border-bottom-style', 'border-bottom-color',
        'border-left-width', 'border-left-style', 'border-left-color',
        'border-top-left-radius', 'border-top-right-radius',
        'border-bottom-left-radius', 'border-bottom-right-radius',
        'vertical-align', 'white-space',
    ];
    const allElements = content.querySelectorAll('*');
    const originalAttributes = [];
    allElements.forEach(el => {
        if (tempImages.some(t => t.img === el)) return;
        if (modifiedLists.some(m => m.ul === el)) return;
        if (overviewReplacements.some(r => r.grid === el)) return;
        originalAttributes.push({ el, style: el.getAttribute('style'), class: el.getAttribute('class') });

        const cs = window.getComputedStyle(el);
        // Skip background on circle elements — they lose their shape in Docs and become colored bars
        const isCircle = el.offsetWidth > 0 &&
            parseFloat(cs.getPropertyValue('border-top-left-radius')) >= el.offsetWidth / 2;
        const parts = [];
        for (const prop of INLINE_PROPS) {
            const val = cs.getPropertyValue(prop);
            if (!val) continue;
            if (prop === 'background-color' && (val === 'rgba(0, 0, 0, 0)' || val === 'transparent')) continue;
            if (prop === 'background-color' && isCircle) continue;
            parts.push(`${prop}:${val}`);
        }
        el.setAttribute('style', parts.join(';'));
        el.removeAttribute('class');
    });

    // 3a. Override with clipboard-friendly table/image styles
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
    content.querySelectorAll('td').forEach(td => {
        if (!td.textContent.trim() && !td.children.length) { td.style.border = 'none'; return; }
        td.style.padding = '4px 8px'; td.style.border = '1px solid #cccccc'; td.style.verticalAlign = 'top';
    });
    content.querySelectorAll('table').forEach(table => { table.style.borderCollapse = 'collapse'; table.style.width = '100%'; table.style.marginTop = '1rem'; table.style.marginBottom = '1rem'; });
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
        gridLayouts.forEach(({ container, table, children }) => {
            children.forEach(child => container.appendChild(child));
            table.remove();
            container.style.display = '';
        });
        stopNumberData.forEach(({ numEl, titleEl, origTitle }) => {
            numEl.style.display = '';
            titleEl.textContent = origTitle;
        });
        processedImages.forEach(({ el, src }) => { el.src = src; });
        btnCopyReport.disabled = false;
    }
}

// ─── Voyage Management ────────────────────────────────────────────────────────

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

const VOYAGE_ACCENTS = ['coral', 'teal', 'violet', 'amber', 'sky'];

function renderVoyageList() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '';

  if ((!voyages || voyages.length === 0) && currentVoyagePage === 1) {
    listContainer.innerHTML = '<p class="np-empty-state">No voyages yet. Plan your first trip!</p>';
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

  let voyageCardIndex = 0;

  sorted.forEach(voyage => {
    if (voyage === 'DATED_HEADER') {
      const hdr = document.createElement('p');
      hdr.className = 'np-voyage-label np-voyage-label--top';
      hdr.textContent = 'Upcoming & Recent';
      listContainer.appendChild(hdr);
      return;
    }
    if (voyage === 'SEPARATOR') {
      const sep = document.createElement('p');
      sep.className = 'np-voyage-label np-voyage-label--sep';
      sep.textContent = 'Undated';
      listContainer.appendChild(sep);
      return;
    }

    const accent = VOYAGE_ACCENTS[voyageCardIndex % VOYAGE_ACCENTS.length];
    voyageCardIndex++;

    const el = document.createElement('div');
    el.className = 'voyage-item';
    el.style.setProperty('--accent', `var(--${accent})`);

    const startDate = voyage.start_date ? new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : null;
    const endDate = voyage.end_date ? new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'}) : null;

    // Build info column
    const info = document.createElement('div');
    info.className = 'voyage-info';

    const titleEl = document.createElement('h2');
    titleEl.className = 'voyage-title';
    titleEl.textContent = voyage.title;
    info.appendChild(titleEl);

    // Chip row
    const chipRow = document.createElement('div');
    chipRow.className = 'np-chip-row';

    if (startDate) {
      const dateChip = document.createElement('span');
      dateChip.className = 'np-chip np-chip--date';
      dateChip.textContent = endDate ? `${startDate} – ${endDate}` : startDate;
      chipRow.appendChild(dateChip);
    } else {
      const dateChip = document.createElement('span');
      dateChip.className = 'np-chip np-chip--nodates';
      dateChip.textContent = 'No dates set';
      chipRow.appendChild(dateChip);
    }

    if (voyage.location_name) {
      const locChip = document.createElement('span');
      locChip.className = 'np-chip np-chip--loc';
      locChip.textContent = `📍 ${displayLocationName(voyage.location_name)}`;
      chipRow.appendChild(locChip);
    }

    info.appendChild(chipRow);

    // Action buttons
    const actions = document.createElement('div');
    actions.className = 'voyage-actions';
    actions.innerHTML = `
      <button class="btn-icon edit" title="Edit">
        <span class="material-symbols-outlined">edit</span>
      </button>
      <button class="btn-icon delete" title="Delete">
        <span class="material-symbols-outlined">delete</span>
      </button>`;

    el.appendChild(info);
    el.appendChild(actions);
    
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
    document.getElementById('modal-voyage-pill').textContent = 'EDIT VOYAGE';
    
    // Show date fields in edit mode
    document.getElementById('voyage-date-fields').classList.remove('hidden');

    document.getElementById('voyage-title').value = voyage.title;
    document.getElementById('voyage-start').value = voyage.start_date ? voyage.start_date.split('T')[0] : '';
    document.getElementById('voyage-end').value = voyage.end_date ? voyage.end_date.split('T')[0] : '';
    document.getElementById('voyage-location-name').value = voyage.location_name || '';
    document.getElementById('voyage-radius').value = voyage.search_radius || 60;
    document.getElementById('voyage-radius-display').textContent = voyage.search_radius || 60;
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

function showVoyageList(doPushState = true) {
    if (doPushState && window.location.pathname !== '/') {
        window.history.pushState({}, '', '/');
    }
    document.getElementById('voyage-list').classList.remove('hidden');
    document.getElementById('itinerary-view').classList.add('hidden');
    document.querySelector('.sidebar-actions').classList.remove('hidden');
    // On mobile keep sidebar open on voyage list view
    document.querySelectorAll('#mobile-tab-bar .np-tab').forEach(t => t.classList.toggle('active', t.dataset.tab === 'plan'));

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

async function selectVoyage(voyage, doPushState = true) {
    if (doPushState) {
        window.history.pushState({ voyageId: voyage.id }, '', `/voyages/${voyage.id}`);
    }
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
    
    // For mobile — show sidebar (itinerary), activate Plan tab
    document.getElementById('app').classList.add('menu-open');
    document.getElementById('tab-plan')?.setAttribute('aria-pressed', 'true');
    document.querySelectorAll('#mobile-tab-bar .np-tab').forEach(t => t.classList.toggle('active', t.dataset.tab === 'plan'));

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

// ─── Stop & Itinerary Management ──────────────────────────────────────────────

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
                map.fitBounds(bounds, { padding: 50, maxZoom: 14 });
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

        // Start radar sweep sequence cycling through all stops
        if (map && currentStops.length > 0) {
            startStopSweepSequence(currentStops);
        }

        // Stream progress updates
        if (fullResRes && fullResRes.session_id) {
            _fullResProgressES = API.streamProgress(fullResRes.session_id, (evt) => {
                if (evt.stage === 'error_503') {
                    set503Callback((count) => {})(1); // Force a 503 check/notification
                    showNotification('Model Busy', 'Our AI models are currently experiencing high demand. Please try again in a few minutes.');
                    return;
                }
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
                    await checkItineraryFullness();
                    renderItinerary();

                    renderMapStops();
                    showNotification('Research Complete', 'All research tasks have been completed successfully.');
                } else if (consecutiveErrors > 15) {
                    clearInterval(_fullResPoll); _fullResPoll = null;
                    if (_fullResProgressES) { _fullResProgressES.close(); _fullResProgressES = null; }
                    clearStopSweeps();
                    isResearchAllRunning = false;
                    renderItinerary();
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
        // Revert spinners
        const researchBtns = document.querySelectorAll('.day-actions .research');
        researchBtns.forEach(btn => {
           btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
           btn.disabled = false;
        });
        if (err.conflict) {
            showNotification('Already Running', 'Full voyage research is already in progress. Please wait for it to finish.');
        } else {
            showNotification('Error', 'Failed to trigger research.');
        }
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
        
        // Hide pilot suggestions — not applicable for undated voyages
        const btnPilot = document.getElementById('btn-pilot-suggestions');
        if (btnPilot) btnPilot.classList.add('hidden');
        
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

    // Pilot Suggestions button is not shown in the dated view — control lives in the sidebar list
    const btnPilot = document.getElementById('btn-pilot-suggestions');
    if (btnPilot) btnPilot.classList.add('hidden');

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
        // Guide research conflict is non-fatal — recommendations still proceed.
        const [recRes] = await Promise.all([
            API.generateRecommendations(currentVoyage.id),
            API.triggerVoyageGuideResearch(currentVoyage.id).catch(e => {
                if (e.conflict) showNotification('Already Running', 'Guide research is already in progress for this voyage.');
                else throw e;
            }),
        ]);

        // Shared completion handler — called from either the done progress event or the poll.
        let pilotDone = false;
        const finishPilotResearch = async () => {
            if (pilotDone) return;
            pilotDone = true;
            clearInterval(_pilotPoll); _pilotPoll = null;
            if (_pilotEventSource) { _pilotEventSource.close(); _pilotEventSource = null; }
            if (_pilotProgressES) { _pilotProgressES.close(); _pilotProgressES = null; }

            isPilotResearching = false;

            // Do a final fetch to ensure we have the full list
            try {
                const res = await API.getRecommendations(currentVoyage.id);
                const recs = ensureRecommendationsArray(res);
                if (recs && recs.length > 0) {
                    voyageRecommendations = recs;
                    renderRecommendations();
                }
            } catch (e) { /* best effort */ }

            renderItinerary();

            if (radarSweep) { radarSweep.stop(); radarSweep = null; }
            if (pilotCenterMarker) pilotCenterMarker.map = map;
            if (pilotRadiusMarker) pilotRadiusMarker.map = map;
            icon.classList.remove('spin');
            btn.disabled = false;

            if (voyageRecommendations.length > 0) {
                showNotification("Research Complete", `We've identified ${voyageRecommendations.length} resource hubs, anchorages, and moorings in your voyage area.`);
                if (map && voyageRecommendations.length > 0) {
                    const { LatLngBounds } = await importLibrary("core");
                    const bounds = new LatLngBounds();
                    voyageRecommendations.forEach(r => bounds.extend({ lat: r.latitude, lng: r.longitude }));
                    if (pilotCircle) bounds.union(pilotCircle.getBounds());
                    map.fitBounds(bounds, { padding: 100, maxZoom: 12 });
                }
            } else {
                showNotification('Incomplete', "The AI research is taking longer than expected. Please try again or check back in a few minutes.");
            }
        };

        // Stream progress events; trigger completion on 'done' or immediate stop on 'error'.
        if (recRes.progress_session_id) {
            _pilotProgressES = API.streamProgress(recRes.progress_session_id, (evt) => {
                if (evt.stage === 'done') {
                    console.log('[progress] done received — finishing pilot research');
                    finishPilotResearch();
                } else if (evt.stage === 'error') {
                    clearInterval(_pilotPoll); _pilotPoll = null;
                    if (_pilotEventSource) { _pilotEventSource.close(); _pilotEventSource = null; }
                    if (_pilotProgressES) { _pilotProgressES.close(); _pilotProgressES = null; }
                    if (radarSweep) { radarSweep.stop(); radarSweep = null; }
                    isPilotResearching = false;
                    icon.classList.remove('spin');
                    btn.disabled = false;
                    renderItinerary();
                    showNotification('Research Failed', evt.message || 'The Local Pilot agent was unable to complete. Please try again.');
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

        const recLabels = {
            hub: 'Resource Hub', anchorage: 'Anchorage', mooring: 'Mooring',
        };

        voyageRecommendations.forEach((rec) => {
            const type = (rec.type || '').toLowerCase();
            const filterKey = type.includes('anchor') ? 'anchorage' : type.includes('moor') ? 'mooring' : 'hub';
            const accent = markerAccent(rec.type);
            const dot = accentDot(accent, 28, filterKey);

            const marker = new AdvancedMarkerElement({
                map,
                position: { lat: rec.latitude, lng: rec.longitude },
                content: dot,
                title: rec.name,
                zIndex: 20,
            });

            if (!activeFilters.has(filterKey)) marker.map = null;

            marker.addListener('gmp-click', () => {
                showRecommendationInfoWindow(rec, marker, { accent, label: recLabels[filterKey] || 'Spot' });
            });

            recommendationMarkers.push({ marker, type: filterKey });
        });

        if (recommendationMarkers.length > 0) {
            document.getElementById('pilot-controls').classList.remove('hidden');
        }

    } catch (err) {
        console.error("Error in renderRecommendations:", err);
    }
}

async function showRecommendationInfoWindow(rec, anchor, style) {
    if (activeInfoWindow) activeInfoWindow.close();

    const { InfoWindow } = await importLibrary('maps');
    const references = rec.reference_links ? (typeof rec.reference_links === 'string' ? JSON.parse(rec.reference_links) : rec.reference_links) : [];
    const accentHex = tokenColor(style.accent);
    const inkHex = tokenColor('ink');
    const mutedHex = tokenColor('muted');
    const surfaceHex = tokenColor('surface');

    let resourcesHtml = '';
    if (rec.url || references.length > 0) {
        resourcesHtml = `
            <div style="margin-bottom:12px">
                <p style="margin:0 0 5px;font-size:10px;font-weight:800;text-transform:uppercase;letter-spacing:.08em;color:${mutedHex}">Resources</p>
                <div style="display:flex;flex-wrap:wrap;gap:6px">
                    ${rec.url ? `<a href="${rec.url}" target="_blank" style="font-size:13px;color:${accentHex};font-weight:700;text-decoration:none">Website ↗</a>` : ''}
                    ${references.map((url, i) => `<a href="${url}" target="_blank" style="font-size:13px;color:${accentHex};text-decoration:none">Link ${i+1} ↗</a>`).join('')}
                </div>
            </div>`;
    }

    const hasDates = currentVoyage && currentVoyage.start_date && currentVoyage.end_date;
    const addButton = hasDates ? `
        <button class="btn primary w-full p-sm" onclick="addRecommendationToItinerary('${rec.id}')" style="width:100%;margin-top:8px">
            Add to Itinerary
        </button>` : '';

    const content = `
        <div style="color:${inkHex};background:${surfaceHex};max-width:280px;font-family:system-ui,sans-serif;padding:4px">
            <b style="font-size:15px;font-weight:700;display:block;margin-bottom:4px">${rec.name}</b>
            <span style="font-size:11px;font-weight:800;text-transform:uppercase;letter-spacing:.08em;color:${accentHex}">${style.label}</span>
            <p style="margin:8px 0;font-size:13px;line-height:1.5;color:${mutedHex}">${rec.description || ''}</p>
            <div style="background:${accentHex}18;border-left:3px solid ${accentHex};padding:8px;border-radius:0 8px 8px 0;margin-bottom:12px;font-size:13px;color:${inkHex};font-style:italic">"${rec.reasoning || ''}"</div>
            ${resourcesHtml}
            ${addButton}
        </div>`;
    
    activeInfoWindow = new InfoWindow({
        content: content,
        position: anchor.position
    });
    activeInfoWindow.open(map, anchor instanceof google.maps.marker.AdvancedMarkerElement ? anchor : null);
}

async function showFacilityInfoWindow(f, anchor, accent) {
    if (activeInfoWindow) activeInfoWindow.close();

    const { InfoWindow } = await importLibrary('maps');
    const accentHex = tokenColor(accent);
    const inkHex = tokenColor('ink');
    const mutedHex = tokenColor('muted');
    const surfaceHex = tokenColor('surface');
    const dangerHex = tokenColor('danger');
    const amberHex = tokenColor('amber');

    const addressHtml = f.address
        ? `<p style="margin:4px 0 8px;font-size:12px;color:${mutedHex}">${f.address}</p>` : '';

    let ratingHtml = '';
    if (f.rating) {
        const stars = Math.round(f.rating);
        const filled = '★'.repeat(stars);
        const empty = '☆'.repeat(5 - stars);
        const count = f.user_rating_count ? ` (${f.user_rating_count.toLocaleString()})` : '';
        ratingHtml = `<p style="margin:4px 0;font-size:13px;color:${mutedHex}">
            <span style="color:${amberHex}">${filled}${empty}</span>
            <span style="margin-left:4px">${f.rating.toFixed(1)}${count}</span>
        </p>`;
    }

    let statusHtml = '';
    if (f.business_status && f.business_status !== 'OPERATIONAL') {
        statusHtml = `<p style="margin:4px 0;font-size:11px;font-weight:700;color:${dangerHex}">${f.business_status.replace(/_/g, ' ')}</p>`;
    }

    const descriptionHtml = f.details?.description
        ? `<p style="margin:8px 0;font-size:13px;line-height:1.5;color:${mutedHex}">${f.details.description}</p>` : '';

    const detailRows = f.details
        ? Object.entries(f.details)
            .filter(([k, v]) => k !== 'description' && v && String(v).toLowerCase() !== 'n/a')
            .map(([k, v]) => `<p style="margin:3px 0;font-size:12px;color:${mutedHex}"><strong style="color:${inkHex}">${k}:</strong> ${v}</p>`)
            .join('') : '';

    const websiteHtml = f.website
        ? `<a href="${f.website}" target="_blank" style="font-size:13px;color:${accentHex};font-weight:700;text-decoration:none;display:block;margin-top:8px">Visit Website ↗</a>` : '';

    const content = `
        <div style="color:${inkHex};background:${surfaceHex};max-width:280px;font-family:system-ui,sans-serif;padding:4px">
            <b style="font-size:15px;font-weight:700;display:block;margin-bottom:2px">${f.name}</b>
            <span style="font-size:11px;font-weight:800;text-transform:uppercase;letter-spacing:.08em;color:${accentHex}">${f.type || 'Facility'}</span>
            ${addressHtml}${ratingHtml}${statusHtml}${descriptionHtml}
            ${detailRows ? `<div style="background:${accentHex}18;padding:8px;border-radius:0 8px 8px 0;border-left:3px solid ${accentHex};margin:8px 0">${detailRows}</div>` : ''}
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

    // Add Pilot Range Circle — sky-tinted ring
    const skyColor = tokenColor('sky') || '#1A5BCE';
    pilotCircle = new Circle({
        strokeColor: skyColor,
        strokeOpacity: 0.6,
        strokeWeight: 2.5,
        fillColor: skyColor,
        fillOpacity: 0.06,
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

    const centerDot = document.createElement('div');
    centerDot.style.cssText = 'width:14px;height:14px;border-radius:50%;background:var(--sky);border:2.5px solid var(--surface);box-shadow:0 2px 6px rgba(0,0,0,.3)';
    centerContainer.appendChild(centerDot);

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
    const radiusHandleDot = document.createElement('div');
    radiusHandleDot.style.cssText = 'width:12px;height:12px;border-radius:50%;background:var(--surface);border:2.5px solid var(--sky);box-shadow:0 2px 6px rgba(0,0,0,.3);cursor:ew-resize';

    const edgePos = spherical.computeOffset(center, radiusMeters, 90);
    pilotRadiusMarker = new AdvancedMarkerElement({
        map: map,
        position: edgePos,
        content: radiusHandleDot,
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
            const card = document.createElement('div');
            card.className = 'np-research-card';

            const top = document.createElement('div');
            top.className = 'np-research-card__top';
            const spinner = document.createElement('span');
            spinner.className = 'material-symbols-outlined spin np-research-card__icon';
            spinner.textContent = 'explore';
            const lbl = document.createElement('span');
            lbl.className = 'np-research-card__label';
            lbl.textContent = 'Researching area…';
            top.appendChild(spinner);
            top.appendChild(lbl);
            card.appendChild(top);

            card.appendChild(Stepper({ steps: [
                { label: 'Scanning anchorages', state: 'done' },
                { label: 'Checking pilot charts', state: 'active' },
                { label: 'Sourcing recommendations', state: 'queued' },
            ]}));
            list.appendChild(card);
            return;
        }

        if (voyageRecommendations && voyageRecommendations.length > 0) {
            // Show Discovery Results in Sidebar
            const header = document.createElement('div');
            header.className = 'p-sm border-b text-gray font-xs uppercase tracking-wider';
            header.textContent = 'Recommended Hubs & Spots';
            list.appendChild(header);

            voyageRecommendations.forEach(rec => {
                const accent = markerAccent(rec.type);
                const tl = (rec.type || '').toLowerCase();
                let recIcon = 'location_on';
                if (tl.includes('anchor')) recIcon = 'anchor';
                else if (tl.includes('moor')) recIcon = 'crisis_alert';
                else if (tl.includes('hub') || tl.includes('marina')) recIcon = 'hub';

                const el = document.createElement('div');
                el.className = 'day-item day-item--stacked';
                el.style.setProperty('--accent', `var(--${accent})`);
                el.innerHTML = DOMPurify.sanitize(`
                    <div class="rec-header">
                        <span class="material-symbols-outlined rec-icon">${recIcon}</span>
                        <span class="rec-name">${displayLocationName(rec.name)}</span>
                        <span class="rec-badge">${rec.type || 'Spot'}</span>
                    </div>
                    ${rec.description ? `<p class="rec-description">${rec.description}</p>` : ''}
                    ${rec.reasoning ? `
                        <div class="pilot-reasoning">
                            <p><strong>Pilot's Reasoning:</strong> "${rec.reasoning}"</p>
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

            const redoContainer = document.createElement('div');
            redoContainer.className = 'p-md text-center';
            redoContainer.innerHTML = '<p class="text-gray mb-md">Results looking stale? <b>Re-run Local Pilot Research</b> to refresh hubs and spots for this area.</p>';
            const redoBtn = document.createElement('button');
            redoBtn.className = 'btn secondary w-full';
            redoBtn.innerHTML = '<span class="material-symbols-outlined icon-align">refresh</span> Re-run Local Pilot Research';
            redoBtn.onclick = () => handlePilotSuggestionsClick();
            redoContainer.appendChild(redoBtn);
            list.appendChild(redoContainer);
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

    // §5.6 — Research-all in progress card
    if (isResearchAllRunning) {
        const card = document.createElement('div');
        card.className = 'np-research-card';

        const top = document.createElement('div');
        top.className = 'np-research-card__top';
        const spinner = document.createElement('span');
        spinner.className = 'material-symbols-outlined spin np-research-card__icon';
        spinner.textContent = 'travel_explore';
        const lbl = document.createElement('span');
        lbl.className = 'np-research-card__label';
        lbl.textContent = 'Researching all stops…';
        top.appendChild(spinner);
        top.appendChild(lbl);
        card.appendChild(top);

        card.appendChild(Stepper({ steps: [
            { label: 'Fetching weather forecasts', state: 'active' },
            { label: 'Reading tide tables', state: 'queued' },
            { label: 'Locating nearby facilities', state: 'queued' },
            { label: 'Building voyage guide', state: 'queued' },
        ]}));
        list.appendChild(card);
        return;
    }

    // §5.7 — Research complete success banner
    if (lastKnownResearchDone) {
        const banner = document.createElement('div');
        banner.className = 'np-research-banner';

        const bannerTop = document.createElement('div');
        bannerTop.className = 'np-research-banner__top';
        const checkIcon = document.createElement('span');
        checkIcon.className = 'material-symbols-outlined np-research-banner__icon';
        checkIcon.textContent = 'check_circle';
        const bannerText = document.createElement('div');
        bannerText.className = 'np-research-banner__text';
        const bannerTitle = document.createElement('span');
        bannerTitle.className = 'np-research-banner__title';
        bannerTitle.textContent = 'Research complete';
        bannerText.appendChild(bannerTitle);
        bannerTop.appendChild(checkIcon);
        bannerTop.appendChild(bannerText);
        banner.appendChild(bannerTop);

        list.appendChild(banner);
    }

    let currentDate = new Date(currentVoyage.start_date);
    const endDate = new Date(currentVoyage.end_date);
    let dayIndex = 0;

    while (currentDate <= endDate) {
        const dateStr = currentDate.toISOString().split('T')[0];
        const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
        const isSelected = selectedDate === dateStr;
        const accent = VOYAGE_ACCENTS[dayIndex % VOYAGE_ACCENTS.length];
        const dayNum = dayIndex + 1;
        const dateLabel = currentDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });

        const el = document.createElement('div');
        el.className = ['np-stop-card', isSelected && 'np-stop-card--selected'].filter(Boolean).join(' ');
        el.style.setProperty('--accent', `var(--${accent})`);

        // Numbered accent circle
        const num = document.createElement('div');
        num.className = 'np-stop-card__num';
        num.setAttribute('aria-hidden', 'true');
        num.textContent = dayNum;
        el.appendChild(num);

        // Middle: date chip + location
        const mid = document.createElement('div');
        mid.className = 'np-stop-card__info';

        const dateChip = document.createElement('span');
        dateChip.className = 'np-stop-card__date';
        dateChip.textContent = dateLabel;
        mid.appendChild(dateChip);

        const loc = document.createElement('span');
        loc.className = ['np-stop-card__loc', !stop && 'np-stop-card__loc--empty'].filter(Boolean).join(' ');
        loc.textContent = stop ? displayLocationName(stop.location_name).split(',')[0].trim() : 'No destination';
        mid.appendChild(loc);
        el.appendChild(mid);

        // Action buttons (only for stops)
        if (stop) {
            const actions = document.createElement('div');
            actions.className = 'np-stop-card__actions';

            const btnResearch = document.createElement('button');
            btnResearch.className = 'btn-icon research';
            btnResearch.title = 'Research';
            btnResearch.dataset.stopId = stop.id;
            btnResearch.innerHTML = '<span class="material-symbols-outlined">science</span>';
            btnResearch.addEventListener('click', (e) => {
                e.stopPropagation();
                handleResearchClick(stop, btnResearch);
            });

            const btnDelete = document.createElement('button');
            btnDelete.className = 'btn-icon delete-stop';
            btnDelete.title = 'Delete Stop';
            btnDelete.innerHTML = '<span class="material-symbols-outlined">delete</span>';
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
                                briefingCache.delete(stop.id);
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

            actions.appendChild(btnResearch);
            actions.appendChild(btnDelete);
            el.appendChild(actions);

            // Weather + tide chips if briefing is cached
            const cached = briefingCache.get(stop.id);
            if (cached) {
                const w = cached.weather_summary || {};
                const chips = document.createElement('div');
                chips.className = 'np-stop-card__chips';
                if (w.condition && w.condition !== 'N/A') {
                    const wChip = document.createElement('span');
                    wChip.className = 'np-stop-card__chip';
                    wChip.style.setProperty('--accent', 'var(--sky)');
                    wChip.textContent = w.condition;
                    chips.appendChild(wChip);
                }
                if (w.wind_speed_kt) {
                    const windChip = document.createElement('span');
                    windChip.className = 'np-stop-card__chip';
                    windChip.style.setProperty('--accent', 'var(--teal)');
                    const deg = directionToDegrees(w.wind_direction);
                    const scale = getWindScale(w.wind_speed_kt) * 0.7;
                    if (w.wind_direction) {
                        const svgWrapper = document.createElement('span');
                        svgWrapper.style.cssText = 'width:14px;height:14px;display:inline-block;vertical-align:middle;margin-right:2px';
                        svgWrapper.innerHTML = getWindArrowSVG(deg, scale);
                        windChip.appendChild(svgWrapper);
                        windChip.appendChild(document.createTextNode(`${w.wind_speed_kt}kt`));
                    } else {
                        windChip.textContent = `💨 ${w.wind_speed_kt}kt`;
                    }
                    chips.appendChild(windChip);
                }
                if (chips.children.length > 0) {
                    mid.appendChild(chips);
                }
            }
        }

        el.addEventListener('click', () => selectDate(dateStr));
        list.appendChild(el);
        dayIndex++;
        
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
    }

    // Research all stops control — always visible at the bottom of the dated itinerary
    const researchAllContainer = document.createElement('div');
    researchAllContainer.className = 'p-md text-center border-t mt-md';
    const researchAllBtn = document.createElement('button');
    researchAllBtn.className = 'btn secondary w-full';
    researchAllBtn.onclick = () => handleResearchAll(true);
    if (lastKnownResearchDone) {
        researchAllContainer.innerHTML = '<p class="text-gray mb-md">Want fresher stop data? <b>Re-run Stop Research</b> to refresh weather, tides, and local charts for every stop.</p>';
        researchAllBtn.innerHTML = '<span class="material-symbols-outlined icon-align">travel_explore</span> Re-run Stop Research';
    } else if (lastKnownItineraryFull) {
        researchAllContainer.innerHTML = '<p class="text-gray mb-md"><b>Itinerary complete!</b> Run stop research to get weather, tides, and pilot info for every stop.</p>';
        researchAllBtn.innerHTML = '<span class="material-symbols-outlined icon-align">travel_explore</span> Run Stop Research';
    } else {
        researchAllContainer.innerHTML = '<p class="text-gray mb-md">Add stops for each day, then run stop research to fetch weather, tides, and local charts.</p>';
        researchAllBtn.innerHTML = '<span class="material-symbols-outlined icon-align">travel_explore</span> Run Stop Research';
        researchAllBtn.disabled = true;
    }
    researchAllContainer.appendChild(researchAllBtn);
    list.appendChild(researchAllContainer);

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

        content.innerHTML = '';
        const loadCard = document.createElement('div');
        loadCard.className = 'np-research-card np-research-card--modal';

        const loadTop = document.createElement('div');
        loadTop.className = 'np-research-card__top';
        const loadSpinner = document.createElement('span');
        loadSpinner.className = 'material-symbols-outlined spin np-research-card__icon';
        loadSpinner.textContent = 'explore';
        const loadLbl = document.createElement('div');
        loadLbl.className = 'np-research-card__meta';
        const loadTitle = document.createElement('span');
        loadTitle.className = 'np-research-card__label';
        loadTitle.textContent = 'Researching stop…';
        const loadSub = document.createElement('span');
        loadSub.className = 'np-research-card__sub';
        loadSub.textContent = 'Checking weather, tides, and local charts';
        loadLbl.appendChild(loadTitle);
        loadLbl.appendChild(loadSub);
        loadTop.appendChild(loadSpinner);
        loadTop.appendChild(loadLbl);
        loadCard.appendChild(loadTop);

        loadCard.appendChild(Stepper({ steps: [
            { label: 'Fetching weather forecast', state: 'active' },
            { label: 'Reading tide tables', state: 'queued' },
            { label: 'Locating nearby facilities', state: 'queued' },
            { label: 'Computing sun phase', state: 'queued' },
        ]}));
        content.appendChild(loadCard);

        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        const resRes = await API.triggerResearch(stop.id);

        // Start radar sweep on this stop's position
        if (map) startStopSweepSequence([stop]);

        // Stream progress updates
        if (resRes.session_id) {
            _stopProgressES = API.streamProgress(resRes.session_id, (evt) => {
                if (evt.stage === 'error_503') {
                    set503Callback((count) => {})(1); // Force a 503 check/notification
                    showNotification('Model Busy', 'Our AI models are currently experiencing high demand. Please try again in a few minutes.');
                    return;
                }
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
        if (err.conflict) {
            showNotification('Already Running', 'Research is already in progress for this stop. Please wait for it to finish.');
            button.innerHTML = originalContent;
        } else {
            button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
            setTimeout(() => button.innerHTML = originalContent, 2000);
        }
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

    const cs = getComputedStyle(document.documentElement);
    const appFont = cs.getPropertyValue('--font').trim() || 'Inter, system-ui, sans-serif';
    Chart.defaults.font.family = appFont;
    const tideLine    = cs.getPropertyValue('--chart-tide-line').trim()    || '#0077be';
    const tideFill    = cs.getPropertyValue('--chart-tide-fill').trim()    || 'rgba(0, 119, 190, 0.2)';
    const gridZero    = cs.getPropertyValue('--chart-grid-zero').trim()    || '#333333';
    const gridDefault = cs.getPropertyValue('--chart-grid-default').trim() || 'rgba(0, 0, 0, 0.1)';
    const axisText    = cs.getPropertyValue('--chart-axis-text').trim()    || '#777777';

    chartInstances.set(canvasId, new Chart(ctx, {
        type: 'line',
        data: {
            datasets: [{
                label: 'Tide Height (ft)',
                data: points,
                borderColor: tideLine,
                backgroundColor: tideFill,
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
                border: { display: false },
                tooltip: {
                    callbacks: {
                        title: (context) => {
                            const val = context[0].parsed.x;
                            const hour = Math.floor(val);
                            const m = Math.round((val - hour) * 60);
                            // Handle day overflow for label
                            let label = `${hour.toString().padStart(2,'0')}:${m.toString().padStart(2,'0')}`;
                            if (hour < 0) label += " (Prev Day)";
                            if (hour >= 24) label += " (Next Day)";

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
                    title: { display: true, text: 'Hour (Local Time)', color: axisText },
                    ticks: {
                        color: axisText,
                        stepSize: 3,
                        callback: (v) => {
                            if (v < 0 || v > 24) return '';
                            return `${v}:00`;
                        }
                    }
                },
                y: {
                    title: { display: true, text: 'Feet', color: axisText },
                    ticks: { color: axisText },
                    grid: {
                        color: (context) => {
                            if (context.tick.value === 0) {
                                return gridZero;
                            }
                            return gridDefault;
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

function generateHourlyTimelineHTML(weather, sun = {}) {
    const hw = weather.hourly_wind || [];
    const hwd = weather.hourly_wind_dir || [];
    const hc = weather.hourly_conditions || [];
    const ht = weather.hourly_temp || [];
    const hg = weather.hourly_gusts || [];
    const hp = weather.hourly_precip || [];
    const hwh = weather.hourly_wave_height || [];
    const hwp = weather.hourly_wave_period || [];
    const hwd_deg = weather.hourly_wave_dir || [];

    if (hw.length < 24) return '';

    // Helper: Parse HH:MM AM/PM into hour (0-23) and minute (0-59)
    const parseSunTime = (s) => {
        if (!s || typeof s !== 'string') return null;
        const parts = s.match(/(\d+):(\d+)\s*(AM|PM)/i);
        if (!parts) return null;
        let hour = parseInt(parts[1]), m = parseInt(parts[2]), ap = parts[3].toUpperCase();
        if (ap === 'PM' && hour < 12) hour += 12;
        if (ap === 'AM' && hour === 12) hour = 0;
        return { hour, min: m, total: hour + m/60, display: s };
    };

    const sunrise = parseSunTime(sun.sunrise);
    const sunset  = parseSunTime(sun.sunset);
    const celestialEvents = [
        sunrise ? { ...sunrise, icon: 'wb_twilight', label: 'Sunrise', type: 'sunrise' } : null,
        sunset ? { ...sunset, icon: 'wb_twilight', label: 'Sunset', type: 'sunset' } : null
    ].filter(Boolean).sort((a,b) => a.total - b.total);

    let nextEventIdx = 0;
    let html = '<div class="np-hourly-track">';

    for (let i = 0; i < 24; i++) {
        const hourLabel = i === 0 ? '12 AM' : i < 12 ? `${i} AM` : i === 12 ? '12 PM' : `${i-12} PM`;
        
        // Determine if it's night
        const isNight = (sunrise && sunset) ? (i < sunrise.total || i > sunset.total) : (i < 6 || i > 18);
        const colClass = 'np-hour-col' + (isNight ? ' np-hour-col--night' : '');

        const kt = Math.round(hw[i]);
        const deg = directionToDegrees(hwd[i]);
        const scale = getWindScale(kt) * 0.7;
        const strengthClass = kt < 11 ? 'light' : kt < 22 ? 'moderate' : 'strong';
        
        let waveHTML = '';
        if (hwh[i] > 0) {
            const waveDeg = hwd_deg[i] || 0;
            waveHTML = `
                <div class="np-hour-wave-visual">
                    <div class="np-wind-forecast__arrow-bg">
                        ${getWindArrowSVG(waveDeg, 0.65, 'np-wind-forecast__arrow-svg')}
                    </div>
                    <div class="np-wind-forecast__circle">
                        <div class="np-wind-forecast__value">${hwh[i].toFixed(1)}</div>
                        <div class="np-wind-forecast__dir">ft</div>
                    </div>
                </div>
                <div class="np-hour-wave-period">${hwp[i] ? hwp[i].toFixed(0) : '-'}s</div>
            `;
        }

        html += `
            <div class="${colClass}">
                <div class="np-hour-time">${hourLabel}</div>
                <span class="material-symbols-outlined np-hour-icon">${getIconForWeather(hc[i], isNight)}</span>
                <div class="np-hour-temp">${Math.round(ht[i])}°</div>
                <div class="np-hour-wind-visual np-hour-wind-visual--${strengthClass}">
                    <div class="np-wind-forecast__arrow-bg">
                        ${getWindArrowSVG(deg, scale, 'np-wind-forecast__arrow-svg')}
                    </div>
                    <div class="np-wind-forecast__circle">
                        <div class="np-wind-forecast__value">${kt}</div>
                        ${hg[i] ? `<div class="np-hour-wind-gust-inner">${Math.round(hg[i])}</div>` : ''}
                    </div>
                </div>
                <div class="np-hour-wind-dir-label">${hwd[i] || '--'}</div>
                ${hp[i] > 0 ? `<div class="np-hour-precip"><span class="material-symbols-outlined">water_drop</span>${hp[i].toFixed(1).replace(/^0/, '')}"</div>` : '<div style="height:16px"></div>'}
                <div style="margin-top:auto">${waveHTML}</div>
            </div>
        `;

        // Insert celestial events that happen in this hour AFTER the hour column
        while (nextEventIdx < celestialEvents.length && celestialEvents[nextEventIdx].hour === i) {
            const ev = celestialEvents[nextEventIdx];
            html += `
                <div class="np-hour-col np-hour-col--celestial np-hour-col--${ev.type}">
                    <div class="np-hour-time">${ev.display}</div>
                    <span class="material-symbols-outlined np-hour-icon">${ev.icon}</span>
                    <div class="np-hour-temp" style="font-size:10px">${ev.label}</div>
                </div>
            `;
            nextEventIdx++;
        }
    }

    html += '</div>';
    return html;
}

function renderHourlyTimeline(weather, sun = {}) {
    const html = generateHourlyTimelineHTML(weather, sun);
    if (!html) return null;
    const timeline = document.createElement('div');
    timeline.className = 'np-hourly-timeline';
    timeline.innerHTML = html;
    return timeline;
}

async function showBriefing(briefing, doPushState = true) {
    // Cache so itinerary cards can show weather chips
    if (briefing && briefing.stop_id) briefingCache.set(briefing.stop_id, briefing);

    if (doPushState && currentVoyage) {
        window.history.pushState({}, '', `/voyages/${currentVoyage.id}/stops/${briefing.stop_id}`);
    }
    const modal = document.getElementById('modal-briefing');
    const content = document.getElementById('briefing-content');
    const btnClose = document.getElementById('btn-close-briefing');
    const btnRedo = document.getElementById('btn-redo-briefing');
    const modalOverlay = document.getElementById('modal-overlay');

    const closeBriefing = () => {
        modal.classList.add('hidden');
        modalOverlay.classList.add('hidden');
        if (currentVoyage) {
            window.history.pushState({}, '', `/voyages/${currentVoyage.id}`);
        }
    };
    btnClose.onclick = closeBriefing;
    modalOverlay.onclick = closeBriefing;

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

    const formatTime = (t) => {
        if (!t) return 'N/A';
        try {
            const d = new Date(t);
            if (isNaN(d.getTime())) return t;
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        } catch (e) { return t; }
    };

    // §5.6 Signal briefing sections — built as DOM nodes
    const sections = document.createDocumentFragment();

    const weather = briefing.weather_summary || {};
    const sun = briefing.sun_phase || {};

    // ── Weather ──────────────────────────────────────────────────────────────
    const weatherSec = document.createElement('div');
    weatherSec.className = 'briefing-section';

    const wxHeader = document.createElement('h3');
    wxHeader.className = 'briefing-header-icon';
    wxHeader.innerHTML = `<span class="material-symbols-outlined">${getIconForWeather(weather.condition)}</span> Weather`;
    weatherSec.appendChild(wxHeader);

    const wxTiles = document.createElement('div');
    wxTiles.style.cssText = 'display:grid;grid-template-columns:repeat(auto-fill,minmax(88px,1fr));gap:8px;margin-top:8px';

    if (!isInvalid(weather.condition)) {
        wxTiles.appendChild(DataTile({ label: 'Conditions', value: weather.condition, icon: getIconForWeather(weather.condition), accent: 'sky' }));
    }
    if (!isInvalid(weather.wind_direction) || weather.wind_speed_kt) {
        wxTiles.appendChild(DataTile({
            label: 'Wind',
            value: `${weather.wind_speed_kt || 0} kt`,
            sub: isInvalid(weather.wind_direction) ? '' : weather.wind_direction,
            icon: 'air',
            accent: 'sky'
        }));
    }
    if (weather.temp_max_f || weather.temp_min_f) {
        wxTiles.appendChild(DataTile({ label: 'Temp', value: `${Math.round(weather.temp_max_f)}°F`, sub: `Low ${Math.round(weather.temp_min_f)}°F`, icon: 'thermometer', accent: 'amber' }));
    }
    if (weather.wave_height_ft > 0) {
        wxTiles.appendChild(DataTile({ label: 'Waves', value: `${weather.wave_height_ft} ft`, icon: 'waves', accent: 'teal' }));
    }
    weatherSec.appendChild(wxTiles);

    if (!isInvalid(weather.summary)) {
        const wxSum = document.createElement('p');
        wxSum.style.cssText = 'margin-top:8px;font-size:13px;color:var(--muted);line-height:1.5';
        wxSum.textContent = weather.summary;
        weatherSec.appendChild(wxSum);
    }

    // Replace old wind tiles with the 24h timeline
    const timeline = renderHourlyTimeline(weather, sun);
    if (timeline) {
        weatherSec.appendChild(timeline);
    }

    if (briefing.weather_last_updated) {
        const wxAge = document.createElement('p');
        wxAge.style.cssText = 'margin-top:6px;font-size:11px;color:var(--muted);font-style:italic';
        const updatedAt = new Date(briefing.weather_last_updated);
        wxAge.textContent = `Forecast as of ${updatedAt.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        weatherSec.appendChild(wxAge);
    }

    sections.appendChild(weatherSec);

    // ── Sun Phase ─────────────────────────────────────────────────────────────
    if (sun.sunrise || sun.sunset) {
        const sunSec = document.createElement('div');
        sunSec.className = 'briefing-section';

        const sunHeader = document.createElement('h3');
        sunHeader.className = 'briefing-header-icon';
        sunHeader.innerHTML = '<span class="material-symbols-outlined">wb_twilight</span> Sun Phase';
        sunSec.appendChild(sunHeader);

        const sunCard = document.createElement('div');
        sunCard.style.cssText = [
            'display:flex',
            'gap:8px',
            'margin-top:8px',
            'padding:14px',
            'border-radius:var(--radius-tile)',
            'background:linear-gradient(120deg,color-mix(in oklab,var(--amber) 10%,var(--surface)),color-mix(in oklab,var(--violet) 8%,var(--surface)))',
        ].join(';');

        if (sun.sunrise) sunCard.appendChild(DataTile({ label: 'Sunrise', value: formatTime(sun.sunrise), accent: 'amber' }));
        if (sun.sunset)  sunCard.appendChild(DataTile({ label: 'Sunset',  value: formatTime(sun.sunset),  accent: 'violet' }));
        sunSec.appendChild(sunCard);
        sections.appendChild(sunSec);
    }

    // ── Tides ─────────────────────────────────────────────────────────────────
    const tides = briefing.tides || {};
    const allEvents = tides.events || [];
    const displayEvents = allEvents.filter(e => e.time.startsWith(targetDateYMD));
    const [y, m, d] = targetDateYMD.split('-');
    const displayDateHeader = `${m}/${d}/${y}`;

    const tidesSec = document.createElement('div');
    tidesSec.className = 'briefing-section';

    const tidesHeader = document.createElement('h3');
    tidesHeader.className = 'briefing-header-icon';
    tidesHeader.innerHTML = `<span class="material-symbols-outlined">waves</span> Tides — ${tides.station_name || 'Unknown Station'} <span class="np-tides-meta">${displayDateHeader}</span>`;
    tidesSec.appendChild(tidesHeader);

    // Chart.js tide chart — same renderer used in the Voyage Report
    const tideCanvasId = `briefingTideChart_${stop.id}`;
    const tideWrap = document.createElement('div');
    tideWrap.style.cssText = 'height:180px;border-radius:var(--radius-tile);overflow:hidden;margin-top:8px';
    const tideCanvas = document.createElement('canvas');
    tideCanvas.id = tideCanvasId;
    tideWrap.appendChild(tideCanvas);
    tidesSec.appendChild(tideWrap);
    // Render after element is in DOM
    requestAnimationFrame(() => renderTideChart(tideCanvasId, tides, targetDateYMD));

    // Tide events table
    if (displayEvents.length > 0) {
        const tbl = document.createElement('table');
        tbl.className = 'briefing-table';
        tbl.style.marginTop = '10px';
        tbl.innerHTML = `<thead><tr>
            <th class="briefing-th">Time</th>
            <th class="briefing-th">Type</th>
            <th class="briefing-th">Height</th>
        </tr></thead>`;
        const tbody = document.createElement('tbody');
        displayEvents.forEach(e => {
            let timeStr = e.time;
            try {
                const dt = new Date(e.time.replace(' ', 'T'));
                if (!isNaN(dt.getTime())) {
                    let hour = dt.getHours(), min = String(dt.getMinutes()).padStart(2,'0');
                    const ap = hour >= 12 ? 'pm' : 'am';
                    hour = hour % 12 || 12;
                    timeStr = `${hour}:${min} ${ap}`;
                }
            } catch (_) {}
            const tr = document.createElement('tr');
            tr.innerHTML = `<td class="briefing-td">${timeStr}</td><td class="briefing-td">${e.type}</td><td class="briefing-td">${e.height_ft} ft</td>`;
            tbody.appendChild(tr);
        });
        tbl.appendChild(tbody);
        tidesSec.appendChild(tbl);
    } else {
        const noData = document.createElement('p');
        noData.className = 'briefing-no-data';
        noData.textContent = 'No tide events for this date';
        tidesSec.appendChild(noData);
    }
    sections.appendChild(tidesSec);

    // ── Facilities ────────────────────────────────────────────────────────────
    const facilities = briefing.facilities || [];
    if (facilities.length > 0) {
        const facilSec = document.createElement('div');
        facilSec.className = 'briefing-section';

        const facilHeader = document.createElement('h3');
        facilHeader.className = 'briefing-header-icon';
        facilHeader.innerHTML = '<span class="material-symbols-outlined">warehouse</span> Facilities';
        facilSec.appendChild(facilHeader);

        const facilList = document.createElement('ul');
        facilList.className = 'facility-list';

        facilities.forEach(f => {
            const accent = markerAccent(f.type);
            const tl = (f.type || '').toLowerCase();
            let icon = 'place';
            if (tl.includes('anchor'))      icon = 'anchor';
            else if (tl.includes('marina')) icon = 'storefront';
            else if (tl.includes('moor'))   icon = 'link';
            else if (tl.includes('bar'))    icon = 'local_bar';
            else if (tl.includes('restaurant')) icon = 'restaurant';

            const li = document.createElement('li');
            li.className = 'facility-item np-facility-briefing-item';
            li.style.setProperty('--accent', `var(--${accent})`);

            // ── Row 1: icon + name + type badge ──────────────────────────────
            const header = document.createElement('div');
            header.className = 'np-facility-briefing-item__header';

            const iconEl = document.createElement('span');
            iconEl.className = 'material-symbols-outlined np-facility-briefing-item__icon';
            iconEl.setAttribute('aria-hidden', 'true');
            iconEl.textContent = icon;

            const nameSpan = document.createElement('span');
            nameSpan.className = 'np-facility-briefing-item__name';
            nameSpan.textContent = f.name;

            const badge = document.createElement('span');
            badge.className = 'np-facility-briefing-item__badge';
            badge.textContent = f.type || 'Facility';

            header.appendChild(iconEl);
            header.appendChild(nameSpan);
            header.appendChild(badge);
            li.appendChild(header);

            // ── Row 2: address ────────────────────────────────────────────────
            const address = f.address || (f.details && typeof f.details === 'object' && f.details.address);
            if (address) {
                const addr = document.createElement('p');
                addr.className = 'np-facility-briefing-item__address';
                addr.textContent = address;
                li.appendChild(addr);
            }

            // ── Row 3: description / string details ───────────────────────────
            if (typeof f.details === 'string') {
                const desc = document.createElement('p');
                desc.className = 'np-facility-briefing-item__desc';
                desc.textContent = f.details;
                li.appendChild(desc);
            }

            // ── Row 4: rating + website + other tiles ─────────────────────────
            if (f.details && typeof f.details === 'object') {
                const starHtml = (r) => '★'.repeat(Math.round(r)) + '☆'.repeat(5 - Math.round(r));
                const tiles = document.createElement('div');
                tiles.className = 'np-facility-briefing-item__tiles';

                if (f.rating) {
                    tiles.appendChild(DataTile({ label: 'Rating', value: `${f.rating.toFixed(1)} ${starHtml(f.rating)}`, sub: f.user_rating_count ? `${f.user_rating_count.toLocaleString()} reviews` : '', accent, detail: true, compact: true }));
                }
                if (f.business_status && f.business_status !== 'OPERATIONAL') {
                    const stat = document.createElement('span');
                    stat.className = 'np-facility-briefing-item__status';
                    stat.textContent = f.business_status.replace(/_/g, ' ');
                    tiles.appendChild(stat);
                }
                // Remaining structured detail tiles (skip address — already shown above)
                Object.entries(f.details).forEach(([k, v]) => {
                    if (k === 'address' || !v) return;
                    const sv = String(v).toLowerCase().trim();
                    if (['n/a','','unknown','not specified'].includes(sv)) return;
                    tiles.appendChild(DataTile({ label: k.replace(/_/g,' '), value: String(v), accent, detail: true }));
                });
                if (tiles.children.length > 0) li.appendChild(tiles);
            }

            // ── Website link ──────────────────────────────────────────────────
            const website = f.website || (f.details && typeof f.details === 'object' && f.details.website);
            if (website) {
                const link = document.createElement('a');
                link.href = website;
                link.target = '_blank';
                link.rel = 'noopener';
                link.className = 'np-facility-briefing-item__website';
                link.textContent = website;
                li.appendChild(link);
            }

            // ── References ────────────────────────────────────────────────────
            if (f.references && f.references.length > 0) {
                const refs = document.createElement('div');
                refs.className = 'ref-link';
                refs.innerHTML = `<strong>Refs:</strong> ${f.references.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}`;
                li.appendChild(refs);
            }

            facilList.appendChild(li);
        });

        facilSec.appendChild(facilList);
        sections.appendChild(facilSec);
    }

    // ── Render into modal ─────────────────────────────────────────────────────
    content.innerHTML = '';

    // Safety alerts at the top if present
    const safetyAlerts = briefing.safety_alerts;
    if (Array.isArray(safetyAlerts) && safetyAlerts.length > 0) {
        const box = LookoutBox(safetyAlerts);
        if (box) content.appendChild(box);
    }

    content.appendChild(sections);

    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoBriefing(briefing, btnRedo);
    }

    // Safety Audit Handler
    const btnLookout = document.getElementById('btn-lookout');
    if (btnLookout) {
        btnLookout.onclick = () => runLookoutAudit(briefing, btnLookout);
    }

    // Show Modal
    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');

    const hide = () => {
        modal.classList.add('hidden');
        modalOverlay.classList.add('hidden');
    };

    btnClose.onclick = hide;
    modalOverlay.onclick = hide;
}

async function redoBriefing(oldBriefing, btn) {
    const content = document.getElementById('briefing-content');
    content.innerHTML = '';
    const redoCard = document.createElement('div');
    redoCard.className = 'np-research-card np-research-card--modal';
    const redoTop = document.createElement('div');
    redoTop.className = 'np-research-card__top';
    const redoSpinner = document.createElement('span');
    redoSpinner.className = 'material-symbols-outlined spin np-research-card__icon';
    redoSpinner.textContent = 'explore';
    const redoLbl = document.createElement('span');
    redoLbl.className = 'np-research-card__label';
    redoLbl.textContent = 'Re-researching stop…';
    redoTop.appendChild(redoSpinner);
    redoTop.appendChild(redoLbl);
    redoCard.appendChild(redoTop);
    redoCard.appendChild(Stepper({ steps: [
        { label: 'Fetching weather forecast', state: 'active' },
        { label: 'Reading tide tables', state: 'queued' },
        { label: 'Locating nearby facilities', state: 'queued' },
        { label: 'Computing sun phase', state: 'queued' },
    ]}));
    content.appendChild(redoCard);
    btn.disabled = true;

    try {
        const redoRes = await API.triggerResearch(oldBriefing.stop_id);

        let redoProgressES = null;
        if (redoRes && redoRes.session_id) {
            _stopProgressES = API.streamProgress(redoRes.session_id, (evt) => {
                if (evt.stage === 'error_503') {
                    set503Callback((count) => {})(1); // Force a 503 check/notification
                    showNotification('Model Busy', 'Our AI models are currently experiencing high demand. Please try again in a few minutes.');
                    return;
                }
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
                        btn.disabled = false;
                        showBriefing(b); // Re-render with new data
                        renderMapStops();
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        btn.disabled = false;
        if (err.conflict) {
            content.innerHTML = '<div class="np-conflict-notice"><span class="material-symbols-outlined">sync</span><p>Research is already in progress for this stop. The briefing will update automatically when it completes.</p></div>';
        } else {
            content.innerHTML = '<div class="error-state error-text"><p>Failed to redo research.</p></div>';
        }
    }
}


async function runVoyageLookout(voyage, sortedStops, reportContent, btn) {
    const originalHTML = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="material-symbols-outlined spin" aria-hidden="true">cloud_download</span>';

    // Update weather for all stops first so the safety audit uses fresh data.
    try {
        await API.updateVoyageWeather(voyage.id);
    } catch (e) {
        // Non-fatal — proceed with the audit even if weather update fails.
        console.warn('Weather update failed before safety audit:', e);
    }

    btn.innerHTML = '<span class="material-symbols-outlined spin" aria-hidden="true">visibility</span>';

    let progressES = null;

    const finish = async () => {
        if (progressES) { progressES.close(); progressES = null; }
        btn.disabled = false;
        btn.innerHTML = originalHTML;
        // Fetch fresh briefings and update the page
        const briefings = await Promise.all(sortedStops.map(s => API.getBriefing(s.id).catch(() => null)));
        refreshSafetyOverview(reportContent, sortedStops, briefings);
    };

    // Safety-net timeout (5 min) in case SSE drops
    const timeout = setTimeout(finish, 300_000);

    try {
        const res = await API.triggerVoyageLookout(voyage.id);

        if (res && res.session_id) {
            progressES = API.streamProgress(res.session_id, async (evt) => {
                if (evt.stage === 'done') {
                    clearTimeout(timeout);
                    await finish();
                } else if (evt.stage === 'error') {
                    clearTimeout(timeout);
                    if (progressES) { progressES.close(); progressES = null; }
                    btn.disabled = false;
                    btn.innerHTML = originalHTML;
                    showNotification('Safety Audit', 'Safety audit encountered an error.');
                }
            });
        } else {
            // No session_id — fall back to a single delayed fetch
            clearTimeout(timeout);
            setTimeout(finish, 10_000);
        }
    } catch (err) {
        clearTimeout(timeout);
        if (progressES) { progressES.close(); progressES = null; }
        btn.disabled = false;
        btn.innerHTML = originalHTML;
        if (err.conflict) {
            showNotification('Safety Audit', 'A safety audit is already in progress for this voyage.');
        } else {
            showNotification('Safety Audit', 'Failed to start safety audit. Please try again.');
        }
    }
}

function refreshSafetyOverview(reportContent, sortedStops, briefings) {
    // Remove any existing safety overview
    const existing = reportContent.querySelector('#np-safety-overview');
    if (existing) existing.remove();

    const stopsWithAlerts = sortedStops
        .map((stop, idx) => ({ stop, briefing: briefings.find(br => br && br.stop_id === stop.id), idx }))
        .filter(({ briefing }) => briefing && Array.isArray(briefing.safety_alerts) && briefing.safety_alerts.length > 0);

    if (stopsWithAlerts.length === 0) {
        showNotification('Safety Audit', 'Safety audit complete. No alerts found.');
        return;
    }

    const box = document.createElement('div');
    box.className = 'lookout-box np-safety-overview';
    box.id = 'np-safety-overview';

    const header = document.createElement('div');
    header.className = 'lookout-header';
    header.innerHTML = '<span class="material-symbols-outlined" aria-hidden="true">visibility</span><h3>Safety Overview</h3>';
    box.appendChild(header);

    const list = document.createElement('div');
    list.className = 'lookout-alert-list';

    stopsWithAlerts.forEach(({ stop, briefing, idx }) => {
        const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, { timeZone: 'UTC', weekday: 'short', month: 'short', day: 'numeric' });
        const stopName = displayLocationName(stop.location_name).split(',')[0].trim();

        const stopHeader = document.createElement('div');
        stopHeader.className = 'np-safety-overview__stop-header';
        stopHeader.innerHTML = `<span class="np-safety-overview__day">Day ${idx + 1}</span>
            <span class="np-safety-overview__name">${DOMPurify.sanitize(stopName)}</span>
            <span class="np-safety-overview__date">${dateStr}</span>`;
        list.appendChild(stopHeader);

        briefing.safety_alerts.forEach(a => {
            const alertEl = document.createElement('div');
            alertEl.className = `lookout-alert lookout-alert--${a.severity || 'info'}`;
            alertEl.innerHTML = `<span class="material-symbols-outlined lookout-alert__icon" aria-hidden="true">${DOMPurify.sanitize(a.icon || 'warning')}</span>
                <div class="lookout-alert__text">
                    <span class="lookout-alert__msg">${DOMPurify.sanitize(a.message || '')}</span>
                    ${a.action ? `<span class="lookout-alert__action">${DOMPurify.sanitize(a.action)}</span>` : ''}
                </div>`;
            list.appendChild(alertEl);
        });
    });

    box.appendChild(list);

    const oldestAudit = stopsWithAlerts
        .map(({ briefing }) => briefing.safety_alerts_updated_at)
        .filter(Boolean)
        .map(t => new Date(t))
        .sort((a, b) => a - b)[0];
    if (oldestAudit) {
        const updated = document.createElement('p');
        updated.className = 'np-safety-overview__updated';
        updated.innerHTML = `<span class="material-symbols-outlined" aria-hidden="true">update</span> Audited ${oldestAudit.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        box.appendChild(updated);
    }

    // Insert after the day-grid section (find it by the section label before it)
    const dayGridEl = reportContent.querySelector('.np-day-grid, .np-day-grid--fill');
    if (dayGridEl && dayGridEl.parentNode) {
        dayGridEl.parentNode.insertBefore(box, dayGridEl.nextSibling);
    } else {
        // Fallback: prepend to report
        reportContent.insertBefore(box, reportContent.firstChild);
    }

    // Also update per-stop alerts in the report
    stopsWithAlerts.forEach(({ stop, briefing, idx }) => {
        const stopSection = reportContent.querySelectorAll('.np-report-stop')[idx];
        if (!stopSection) return;
        const existingBox = stopSection.querySelector('.lookout-box');
        if (existingBox) existingBox.remove();

        const perStopBox = LookoutBox(briefing.safety_alerts);
        if (perStopBox) {
            const weatherGrid = stopSection.querySelector('.np-weather-grid');
            if (weatherGrid) {
                stopSection.insertBefore(perStopBox, weatherGrid);
            }
        }
    });
}

async function runLookoutAudit(oldBriefing, btn) {
    btn.disabled = true;
    const originalLabel = btn.innerHTML;
    btn.innerHTML = '<span class="material-symbols-outlined icon-lg icon-align spin" aria-hidden="true">visibility</span> Auditing…';

    try {
        const res = await API.triggerLookoutAudit(oldBriefing.stop_id);
        const startTime = Date.now();
        const TIMEOUT_MS = 120000;

        const poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                btn.disabled = false;
                btn.innerHTML = originalLabel;
                showNotification('Safety Audit', 'The audit is taking longer than expected. Try again in a moment.');
                return;
            }
            try {
                const b = await API.getBriefing(oldBriefing.stop_id);
                if (b && b.safety_alerts !== null && b.safety_alerts !== undefined) {
                    clearInterval(poll);
                    btn.disabled = false;
                    btn.innerHTML = originalLabel;
                    briefingCache.set(b.stop_id, b);
                    showBriefing(b, false);
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        btn.disabled = false;
        btn.innerHTML = originalLabel;
        if (err.noBriefing) {
            showNotification('Safety Audit', 'Run stop research first before running a safety audit.');
        } else if (err.conflict) {
            showNotification('Safety Audit', 'A safety audit is already in progress for this stop.');
        } else {
            showNotification('Safety Audit', 'Failed to start safety audit. Please try again.');
        }
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

// ─── Map ──────────────────────────────────────────────────────────────────────

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

  const { Map } = await importLibrary('maps');
  const { Geocoder } = await importLibrary("geocoding");

  const theme = currentTheme();
  const isMidnightMariner = theme === 'midnight-mariner' || theme === 'dark';

  console.log('NavalPlan: Map init — theme:', theme, '| colorScheme:', isMidnightMariner ? 'DARK' : 'LIGHT');

  map = new Map(document.getElementById("map-container"), {
    center: { lat: 20, lng: 0 },
    zoom: 3,
    maxZoom: 18,
    mapId: isMidnightMariner ? __GOOGLE_MAPS_MAP_ID_MM__ : __GOOGLE_MAPS_MAP_ID__,
    colorScheme: isMidnightMariner ? 'DARK' : 'LIGHT',
    disableDefaultUI: false,
    mapTypeControl: false,
    streetViewControl: false,
    fullscreenControl: false,
    clickableIcons: false
  });

  // Spacer pushes Google's top-right controls (map type, fullscreen) below the auth bar
  const mapCtrlSpacer = document.createElement('div');
  mapCtrlSpacer.style.cssText = 'height:72px;width:1px;pointer-events:none';
  map.controls[google.maps.ControlPosition.TOP_RIGHT].push(mapCtrlSpacer);

  // Global Data Layer Styling
  map.data.setStyle((feature) => {
      const type = feature.getProperty('type');

      // If it's a point in the data layer (default), hide it because we use AdvancedMarkers
      if (feature.getGeometry().getType() === 'Point') {
          return { visible: false };
      }
      // 2. Discovery Regions — colors match tier chip accent tokens
      const tier = feature.getProperty('tier');
      if (tier) {
          let token = 'teal';
          if (tier === 'Hidden Gem' || tier === 'Deep Cut') token = 'violet';
          else if (tier === 'Regional Favorite')            token = 'amber';
          else if (tier === 'Challenging')                  token = 'coral';

          const color = tokenColor(token);
          return {
              fillColor: color,
              fillOpacity: 0.5,
              strokeColor: color,
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
          showRegionBriefing(props, currentDiscoveryMonth);
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
  
      const doSave = async () => {
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
      };

      if (stop && stop.location_name) {
          const existing = displayLocationName(stop.location_name);
          showNotification(
              'Change Location?',
              `This date is already set to "${existing}". Replace it with the new location?`,
              [
                  { label: 'Replace', type: 'primary', hideClose: true, callback: doSave },
                  { label: 'Cancel', type: 'secondary' },
              ]
          );
      } else {
          doSave();
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

    // Add Markers — Signal numbered teardrop pins with rotating accents
    sortedStops.forEach((stop, index) => {
        const accent = VOYAGE_ACCENTS[index % VOYAGE_ACCENTS.length];
        const pinEl = MapPin({ accent, n: index + 1, label: `Stop ${index + 1}: ${displayLocationName(stop.location_name)}` });

        const marker = new AdvancedMarkerElement({
            map: map,
            position: { lat: stop.latitude, lng: stop.longitude },
            content: pinEl,
            title: `${displayLocationName(stop.location_name)} (Day ${index + 1})`,
            zIndex: 100
        });

        marker.addListener('gmp-click', () => {
            if (activeInfoWindow) activeInfoWindow.close();
            const ink = tokenColor('ink');
            const surface = tokenColor('surface');
            activeInfoWindow = new InfoWindow({
                content: `<div style="color:${ink};background:${surface};padding:6px 10px;border-radius:10px;font-family:system-ui,sans-serif;font-size:14px"><b>${displayLocationName(stop.location_name)}</b><br><span style="color:${tokenColor('muted')}">Stop ${index + 1}</span></div>`
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
                             const filterKey = type.includes('anchor') ? 'anchorage'
                                 : type.includes('marina') ? 'marina'
                                 : type.includes('moor') ? 'mooring'
                                 : type.includes('restaurant') ? 'restaurant'
                                 : type.includes('bar') ? 'bar'
                                 : 'other';

                             const accent = markerAccent(f.type);
                             const dot = accentDot(accent, 24, filterKey);

                             const fMarker = new AdvancedMarkerElement({
                                 map: activeFacilityFilters.has(filterKey) ? map : null,
                                 position: { lat: f.latitude, lng: f.longitude },
                                 content: dot,
                                 title: f.name,
                                 zIndex: 1
                             });

                             fMarker.addListener('gmp-click', () => {
                                 showFacilityInfoWindow(f, fMarker, accent);
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

    // Draw route — dashed marching-ants polyline in --ink color
    const coords = sortedStops.map(s => ({ lat: s.latitude, lng: s.longitude }));
    const routeLineColor = tokenColor('ink') || '#0B1220';

    routePolyline = new Polyline({
      path: coords,
      geodesic: true,
      strokeColor: routeLineColor,
      strokeOpacity: 0,
      icons: [{
        icon: { path: 'M 0,-1 0,1', strokeOpacity: 0.7, scale: 3 },
        offset: '0',
        repeat: '14px'
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

    const cs = getComputedStyle(document.documentElement);
    const appFont = cs.getPropertyValue('--font').trim() || 'Inter, system-ui, sans-serif';
    Chart.defaults.font.family = appFont;
    const tideLine      = cs.getPropertyValue('--chart-tide-line').trim()       || '#0077be';
    const tideFillMini  = cs.getPropertyValue('--chart-tide-fill-mini').trim()  || 'rgba(0, 119, 190, 0.1)';
    const highLabel     = cs.getPropertyValue('--chart-tide-high-label').trim() || '#d9534f';
    const lowLabel      = cs.getPropertyValue('--chart-tide-low-label').trim()  || '#314c3b';

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

              if (maxPt) drawLabel(maxPt, highLabel, 'bottom');

              // Draw Low

              if (minPt) drawLabel(minPt, lowLabel, 'top');
  
          }
  
      };
  
  
  
      if (chartInstances.has(canvasId)) { chartInstances.get(canvasId).destroy(); chartInstances.delete(canvasId); }
      const ctx = canvas.getContext('2d');

      chartInstances.set(canvasId, new Chart(ctx, {
  
          type: 'line',
  
          data: {
  
              datasets: [{

                  data: points,

                  borderColor: tideLine,

                  backgroundColor: tideFillMini,
  
                  borderWidth: 2,
  
                  tension: 0.4,
  
                  pointRadius: 0,
  
                  fill: 'start'
  
              }]
  
          },
  
          options: {
  
              responsive: true,
  
              maintainAspectRatio: false,
  
              plugins: { legend: { display: false }, border: { display: false }, tooltip: { enabled: false } },
  
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
        // No stops yet — show the voyage hub fitted to the search radius
        markersParam += `&markers=color:blue%7C${currentVoyage.latitude},${currentVoyage.longitude}`;
    }

    // For voyages with stops, omit center/zoom so Google auto-fits the route.
    // For undated voyages (hub only), compute zoom from search_radius so the
    // map matches the search circle the user sees on the map.
    let centerZoomParam = '';
    if (sortedStops.length === 0 && currentVoyage.latitude && currentVoyage.longitude) {
        const radiusNm = currentVoyage.search_radius || 60;
        const radiusM  = radiusNm * 1852;
        const lat      = currentVoyage.latitude;
        // Google Static Maps: at zoom z the map width in metres is
        // 600px * 156543.03 * cos(lat) / 2^z. We want the diameter to fit.
        const z = Math.log2(600 * 156543.03 * Math.cos(lat * Math.PI / 180) / (radiusM * 2));
        const zoom = Math.max(1, Math.min(14, Math.round(z) - 1)); // -1 for padding
        centerZoomParam = `&center=${lat},${currentVoyage.longitude}&zoom=${zoom}`;
    }

    const url = `${baseUrl}?size=${size}&scale=${scale}&maptype=${mapType}${pathParam}${markersParam}${centerZoomParam}&key=${key}`;

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

    async function handleShowReport(doPushState = true) {
        if (!currentVoyage) return;
        if (doPushState) {
            window.history.pushState({}, '', `/voyages/${currentVoyage.id}/report`);
        }

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

            const closeReport = () => {
                modal.classList.add('hidden');
                modalOverlay.classList.add('hidden');
                if (currentVoyage) {
                    window.history.pushState({}, '', `/voyages/${currentVoyage.id}`);
                }
            };
            document.getElementById('btn-close-report').onclick = closeReport;
            modalOverlay.onclick = closeReport;

            content.innerHTML = DOMPurify.sanitize(html, { ADD_ATTR: ["target"] });
            modal.classList.remove("hidden");
            modalOverlay.classList.remove("hidden");

            // Inject Share button
            const headerControls = modal.querySelector('.modal-header-row .flex.gap-sm');
            let btnShare = document.getElementById('btn-share-report');
            if (!btnShare) {
                btnShare = document.createElement('button');
                btnShare.id = 'btn-share-report';
                btnShare.className = 'btn-icon icon-xl';
                btnShare.title = 'Share Report';
                btnShare.innerHTML = '<span class="material-symbols-outlined">share</span>';
                headerControls.insertBefore(btnShare, headerControls.firstChild);
            }
            btnShare.onclick = () => handleShareClick(guide);

            // Wire Sailing Mode toggle
            const btnSailingMode = document.getElementById('btn-toggle-sailing-mode');
            if (btnSailingMode) {
                // Ensure initial state is inactive
                btnSailingMode.classList.remove('active');
                modal.classList.remove('np-report--sailing-only');
                
                btnSailingMode.onclick = () => {
                    const active = btnSailingMode.classList.toggle('active');
                    if (active) {
                        modal.classList.add('np-report--sailing-only');
                    } else {
                        modal.classList.remove('np-report--sailing-only');
                    }
                };
            }

            // Wire Safety Audit button
            const btnVoyageLookout = document.getElementById('btn-voyage-lookout');
            if (btnVoyageLookout) {
                btnVoyageLookout.onclick = () => runVoyageLookout(currentVoyage, sortedStops, content, btnVoyageLookout);
            }

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


// ─── Voyage Guide ─────────────────────────────────────────────────────────────

async function handleGuideClick(voyage, button, doPushState = true) {
    if (doPushState) {
        window.history.pushState({}, '', `/voyages/${voyage.id}/guide`);
    }
    const originalContent = button.innerHTML;
    
    try {
        const resp = await API.getVoyageGuide(voyage.id);
        if (resp && resp.guide && resp.guide.summary) {
            showVoyageGuide(resp, doPushState);
            return;
        }

        // Show Modal Immediately with loading state
        const modal = document.getElementById('modal-guide');
        const content = document.getElementById('guide-content');
        const modalOverlay = document.getElementById('modal-overlay');

        const closeGuide = () => {
            modal.classList.add('hidden');
            modalOverlay.classList.add('hidden');
            if (currentVoyage) {
                window.history.pushState({}, '', `/voyages/${currentVoyage.id}`);
            }
        };
        document.getElementById('btn-close-guide').onclick = closeGuide;
        modalOverlay.onclick = closeGuide;
        
        content.innerHTML = '';
        const guideLoadCard = document.createElement('div');
        guideLoadCard.className = 'np-research-card np-research-card--modal np-research-card--guide';
        const guideLoadTop = document.createElement('div');
        guideLoadTop.className = 'np-research-card__top';
        const guideSpinner = document.createElement('span');
        guideSpinner.className = 'material-symbols-outlined spin np-research-card__icon';
        guideSpinner.textContent = 'travel_explore';
        const guideLbl = document.createElement('div');
        guideLbl.className = 'np-research-card__meta';
        const guideTitleEl = document.createElement('span');
        guideTitleEl.className = 'np-research-card__label';
        guideTitleEl.textContent = 'Building destination guide…';
        const guideSubEl = document.createElement('span');
        guideSubEl.className = 'np-research-card__sub';
        guideSubEl.textContent = 'Gathering local knowledge, seasonal data, and regional hazards';
        guideLbl.appendChild(guideTitleEl);
        guideLbl.appendChild(guideSubEl);
        guideLoadTop.appendChild(guideSpinner);
        guideLoadTop.appendChild(guideLbl);
        guideLoadCard.appendChild(guideLoadTop);
        guideLoadCard.appendChild(Stepper({ steps: [
            { label: 'Researching seasonal conditions', state: 'active' },
            { label: 'Mapping regional hazards', state: 'queued' },
            { label: 'Finding hubs & charter info', state: 'queued' },
            { label: 'Compiling country & culture', state: 'queued' },
        ]}));
        content.appendChild(guideLoadCard);
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        const guideRes = await API.triggerVoyageGuideResearch(voyage.id);

        // Stream progress updates
        if (guideRes && guideRes.session_id) {
            _guideProgressES = API.streamProgress(guideRes.session_id, (evt) => {
                if (evt.stage === 'error_503') {
                    set503Callback((count) => {})(1); // Force a 503 check/notification
                    showNotification('Model Busy', 'Our AI models are currently experiencing high demand. Please try again in a few minutes.');
                    return;
                }
            });
        }

        // Poll
        _guidePoll = setInterval(async () => {
            try {
                const resp = await API.getVoyageGuide(voyage.id);
                if (resp && resp.guide && resp.guide.summary && resp.guide.summary.length > 0) {
                    clearInterval(_guidePoll); _guidePoll = null;
                    if (_guideProgressES) { _guideProgressES.close(); _guideProgressES = null; }
                    button.innerHTML = originalContent;
                    showVoyageGuide(resp);
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);

    } catch (err) {
        console.error(err);
        clearInterval(_guidePoll); _guidePoll = null;
        if (_guideProgressES) { _guideProgressES.close(); _guideProgressES = null; }
        if (err.conflict) {
            showNotification('Already Running', 'Guide research is already in progress for this voyage. Please wait for it to finish.');
            button.innerHTML = originalContent;
        } else {
            button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
            setTimeout(() => button.innerHTML = originalContent, 2000);
        }
    }
}

function showVoyageGuide(resp, doPushState = true) {
    const guide = resp.guide;
    const mapURL = resp.map_url;

    const modal = document.getElementById('modal-guide');
    const content = document.getElementById('guide-content');
    const btnRedo = document.getElementById('btn-redo-guide');
    const modalOverlay = document.getElementById('modal-overlay');

    const closeGuide = () => {
        modal.classList.add('hidden');
        modalOverlay.classList.add('hidden');
        if (currentVoyage) {
            window.history.pushState({}, '', `/voyages/${currentVoyage.id}`);
        }
    };
    document.getElementById('btn-close-guide').onclick = closeGuide;
    modalOverlay.onclick = closeGuide;

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
        html += `<img src="${url}" alt="Voyage Map" class="np-report-map" />`;
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
    content.innerHTML = '';
    const redoGuideCard = document.createElement('div');
    redoGuideCard.className = 'np-research-card np-research-card--modal np-research-card--guide';
    const rt = document.createElement('div');
    rt.className = 'np-research-card__top';
    const rs = document.createElement('span');
    rs.className = 'material-symbols-outlined spin np-research-card__icon';
    rs.textContent = 'travel_explore';
    const rl = document.createElement('span');
    rl.className = 'np-research-card__label';
    rl.textContent = 'Re-researching guide…';
    rt.appendChild(rs); rt.appendChild(rl);
    redoGuideCard.appendChild(rt);
    redoGuideCard.appendChild(Stepper({ steps: [
        { label: 'Researching seasonal conditions', state: 'active' },
        { label: 'Mapping regional hazards', state: 'queued' },
        { label: 'Finding hubs & charter info', state: 'queued' },
        { label: 'Compiling country & culture', state: 'queued' },
    ]}));
    content.appendChild(redoGuideCard);
    btn.disabled = true;

    try {
        const redoGuideRes = await API.triggerVoyageGuideResearch(oldGuide.voyage_id);

        const oldTime = new Date(oldGuide.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 5 * 60 * 1000; // 5 minutes — guide research can take ~60s+

        let poll;
        let redoGuideProgressES = null;
        if (redoGuideRes && redoGuideRes.session_id) {
            redoGuideProgressES = API.streamProgress(redoGuideRes.session_id, (evt) => {
                if (evt.stage === 'error') {
                    clearInterval(poll);
                    redoGuideProgressES.close();
                    btn.disabled = false;
                    content.innerHTML = `<div class="error-state"><p><strong>Research failed.</strong> ${evt.message || 'The agent was unable to generate a guide.'} The previous guide has been preserved.</p></div>`;
                }
            });
        }

        poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                if (redoGuideProgressES) redoGuideProgressES.close();
                content.innerHTML = '<div class="error-state"><p><strong>Research timed out.</strong></p></div>';
                btn.disabled = false;
                return;
            }
            try {
                const g = await API.getVoyageGuide(oldGuide.voyage_id);
                if (g && g.guide) {
                    const newTime = new Date(g.guide.created_at).getTime();
                    if (newTime > oldTime) {
                        clearInterval(poll);
                        if (redoGuideProgressES) redoGuideProgressES.close();
                        btn.disabled = false;
                        showVoyageGuide(g);
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        btn.disabled = false;
        if (err.conflict) {
            content.innerHTML = '<div class="np-conflict-notice"><span class="material-symbols-outlined">sync</span><p>Guide research is already in progress for this voyage. The guide will update automatically when it completes.</p></div>';
        }
    }
}


// ─── Discovery Mode ───────────────────────────────────────────────────────────

async function toggleDiscoveryMode(active) {
    currentMode = active ? 'discovery' : 'planner';
    const discoveryControls = document.getElementById('discovery-controls');
    const sidebar = document.getElementById('sidebar');
    
    if (active) {
        discoveryControls.classList.remove('hidden');
        sidebar.classList.add('hidden');

        // Sync month squares to current month
        currentDiscoveryMonth = new Date().getMonth() + 1;
        document.querySelectorAll('#month-squares .np-month-sq').forEach(b => {
            const active = parseInt(b.dataset.month) === currentDiscoveryMonth;
            b.classList.toggle('active', active);
            b.setAttribute('aria-pressed', String(active));
        });

        clearRecommendations();
        clearPilotCircle();
        clearMap(); // Clear existing markers/routes
        loadDiscoveryRegions(currentDiscoveryMonth);
        
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
            selectVoyage(currentVoyage, false); // Restore voyage view
        } else {
            showVoyageList(false);
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
    const titleEl = document.getElementById('region-title');
    const content = document.getElementById('region-briefing-content');
    const overlay = document.getElementById('modal-overlay');

    const region = discoveryRegions.find(r => r.id === props.id);

    // Tier → accent + label
    const tierAccent = (tier) => {
        if (tier === 'Hidden Gem' || tier === 'Deep Cut') return 'violet';
        if (tier === 'Regional Favorite')                 return 'amber';
        if (tier === 'Challenging')                       return 'coral';
        return 'teal';
    };
    const accent = tierAccent(props.tier);
    const tierLabel = props.tier || (props.is_hidden_gem ? 'Hidden Gem' : 'Standard');

    titleEl.textContent = props.name;
    content.innerHTML = '';

    // ── Two-column header: summary left, chip + score right ───────────────
    const header = document.createElement('div');
    header.className = 'np-region-header';

    const headerLeft = document.createElement('div');
    headerLeft.className = 'np-region-header__left';

    if (props.summary) {
        const summary = document.createElement('p');
        summary.className = 'np-region-summary';
        summary.textContent = props.summary;
        headerLeft.appendChild(summary);
    }

    const headerRight = document.createElement('div');
    headerRight.className = 'np-region-header__right';

    const chip = document.createElement('span');
    chip.className = 'np-region-chip';
    chip.style.setProperty('--accent', `var(--${accent})`);
    chip.textContent = tierLabel;
    headerRight.appendChild(chip);

    const scoreRing = ScoreRing({ score: props.suitability_score || 0, accent, size: 56 });
    const scoreWrap = document.createElement('div');
    scoreWrap.className = 'np-region-score';
    const scoreLabel = document.createElement('span');
    scoreLabel.className = 'np-region-score__label';
    scoreLabel.textContent = 'Suitability';
    scoreWrap.appendChild(scoreRing);
    scoreWrap.appendChild(scoreLabel);
    headerRight.appendChild(scoreWrap);

    header.appendChild(headerLeft);
    header.appendChild(headerRight);
    content.appendChild(header);

    // ── DataTile row: Wind / Temp / Tide ──────────────────────────────────
    const tiles = document.createElement('div');
    tiles.className = 'np-region-tiles';

    const windKt = region?.avg_wind_speed_knots;
    const tempC  = region?.avg_temp_c;
    const tempF  = tempC != null ? Math.round(tempC * 9/5 + 32) : null;

    tiles.appendChild(DataTile({
        label: 'Wind',
        value: windKt != null ? `${windKt} kt` : '—',
        icon: 'air',
        accent: 'sky',
    }));
    tiles.appendChild(DataTile({
        label: 'Avg Temp',
        value: tempF != null ? `${tempF}°F` : '—',
        sub: tempC != null ? `${tempC}°C` : '',
        icon: 'thermometer',
        accent: 'amber',
    }));
    tiles.appendChild(DataTile({
        label: 'Tides',
        value: 'Varies',
        icon: 'water',
        accent: 'teal',
    }));
    content.appendChild(tiles);

    // ── Deep cut reasoning ────────────────────────────────────────────────
    if (region?.deep_cut_reasoning) {
        const dcCard = document.createElement('div');
        dcCard.className = 'np-region-deep-cut';
        dcCard.style.setProperty('--accent', `var(--${accent})`);
        const dcLabel = document.createElement('div');
        dcLabel.className = 'np-region-deep-cut__label';
        dcLabel.textContent = 'The Deep Cut Factor';
        const dcText = document.createElement('p');
        dcText.className = 'np-region-deep-cut__text';
        dcText.textContent = region.deep_cut_reasoning;
        dcCard.appendChild(dcLabel);
        dcCard.appendChild(dcText);
        content.appendChild(dcCard);
    }

    // ── Action buttons ────────────────────────────────────────────────────
    const actions = document.createElement('div');
    actions.className = 'np-region-actions';

    const btnStart = document.createElement('button');
    btnStart.className = 'btn primary';
    btnStart.textContent = 'Start here →';
    btnStart.addEventListener('click', () => {
        hide();

        // Compute polygon centroid from the region's outer ring.
        let centerLat = null, centerLng = null;
        if (region && region.geometry && region.geometry.coordinates && region.geometry.coordinates[0]) {
            const ring = region.geometry.coordinates[0];
            const sumLat = ring.reduce((s, c) => s + c[1], 0);
            const sumLng = ring.reduce((s, c) => s + c[0], 0);
            centerLat = sumLat / ring.length;
            centerLng = sumLng / ring.length;
        }

        // Pan the map to the region center before leaving discovery mode.
        if (map && centerLat !== null) {
            map.panTo({ lat: centerLat, lng: centerLng });
            map.setZoom(7);
        }

        toggleDiscoveryMode(false);

        // Open new voyage modal pre-filled with this region.
        const btnNew = document.getElementById('btn-new-voyage');
        if (btnNew) btnNew.click();

        setTimeout(() => {
            const locInput = document.getElementById('voyage-location-name');
            if (locInput) {
                locInput.value = props.name;
                locInput.dispatchEvent(new Event('input'));
            }
            // Pre-fill coordinates from the centroid.
            if (centerLat !== null) {
                const inputLat = document.getElementById('voyage-lat');
                const inputLng = document.getElementById('voyage-lng');
                const displayCoords = document.getElementById('voyage-coords-display');
                if (inputLat) inputLat.value = centerLat.toFixed(6);
                if (inputLng) inputLng.value = centerLng.toFixed(6);
                if (displayCoords) displayCoords.textContent = `${centerLat.toFixed(4)}, ${centerLng.toFixed(4)}`;
            }
        }, 100);
    });

    actions.appendChild(btnStart);
    content.appendChild(actions);

    // ── Admin: delete seasonality ─────────────────────────────────────────
    if (currentUser?.is_admin) {
        const adminRow = document.createElement('div');
        adminRow.className = 'np-region-admin';
        const btnDel = document.createElement('button');
        btnDel.className = 'btn secondary';
        btnDel.innerHTML = `<span class="material-symbols-outlined" aria-hidden="true">delete</span> Remove for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}`;
        btnDel.addEventListener('click', () => {
            showNotification('Remove Seasonality',
                `Remove ${props.name} from discovery for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}?`,
                [
                    {
                        label: 'Remove', type: 'danger', hideClose: true,
                        callback: async () => {
                            try {
                                await API.deleteDiscoverySeasonality(props.id, month);
                                hide();
                                loadDiscoveryRegions(month);
                            } catch (err) {
                                console.error(err);
                                showNotification('Error', 'Failed to remove region.');
                            }
                        }
                    },
                    { label: 'Cancel', type: 'secondary' }
                ]
            );
        });
        adminRow.appendChild(btnDel);
        content.appendChild(adminRow);
    }

    modal.classList.remove('hidden');
    overlay.classList.remove('hidden');

    const hide = () => {
        modal.classList.add('hidden');
        overlay.classList.add('hidden');
    };

    document.getElementById('btn-close-region-briefing').onclick = hide;
    overlay.onclick = hide;
}

async function initSharedMode(token) {
    document.body.classList.add('shared-view');
    document.documentElement.classList.add('shared-view');
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
                <div class="np-qr-wrap">
                    <canvas id="share-qr-canvas"></canvas>
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

    const renderQR = async (url) => {
        const canvas = shareModal.querySelector('#share-qr-canvas');
        if (!canvas) return;
        const QRCode = (await import('qrcode')).default;
        const cs = getComputedStyle(document.documentElement);
        await QRCode.toCanvas(canvas, url, {
            width: 180,
            margin: 2,
            color: {
                dark: cs.getPropertyValue('--ink').trim() || '#000000',
                light: cs.getPropertyValue('--surface').trim() || '#ffffff',
            },
        });
    };

    // Init State
    chk.checked = currentVoyage.is_public;
    if (currentVoyage.is_public) {
        const url = `${window.location.origin}/shared/${currentVoyage.share_token}`;
        linkInput.value = url;
        linkContainer.classList.remove('hidden');
        renderQR(url);
    }
    
    document.getElementById('modal-overlay').classList.remove('hidden');
    shareModal.classList.remove('hidden');
    
    chk.onchange = async () => {
        try {
            if (chk.checked) {
                const res = await API.enableSharing(currentVoyage.id);
                currentVoyage.is_public = true;
                currentVoyage.share_token = res.share_token;

                const url = `${window.location.origin}/shared/${res.share_token}`;
                linkInput.value = url;
                linkContainer.classList.remove('hidden');
                renderQR(url);
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

// ─── Report & Guide HTML Generators ──────────────────────────────────────────

function generateGuideHTML(guide) {
    const MONTHS = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];

    // Helper: Signal section label chip
    const sectionChip = (label, accent = 'coral') =>
        `<div class="np-eyebrow-accent" style="--accent:var(--${accent})">${label}</div>`;

    // Helper: tinted hazard card
    const hazardCard = (title, desc, refs, accent = 'amber', icon = 'warning') => `
        <div class="np-hazard-card" style="--accent:var(--${accent})">
            <div class="np-hazard-card__title"><span class="material-symbols-outlined np-hazard-card__icon">${icon}</span>${title}</div>
            <p class="np-hazard-card__desc">${desc}</p>
            ${refs && refs.length ? renderReferences(refs) : ''}
        </div>`;

    // Overview + Hazards (left col)
    let leftCol = `<div class="np-report-item--non-sailing">`;
    leftCol += sectionChip('Overview', 'coral');
    leftCol += `<p class="np-overview-summary">${guide.summary || 'No summary available.'}</p>`;
    leftCol += `</div>`;

    if (guide.hazards && guide.hazards.length > 0) {
        leftCol += sectionChip('Regional Hazards', 'amber');
        guide.hazards.forEach(h => {
            const t = (h.title || '').toLowerCase();
            const icon = t.includes('reef') ? 'water' : t.includes('current') ? 'cyclone' : t.includes('storm') ? 'thunderstorm' : t.includes('shoal') ? 'anchor' : 'warning';
            const accent = t.includes('current') || t.includes('wind') ? 'sky' : 'amber';
            leftCol += hazardCard(h.title + (h.url ? ` <a href="${h.url}" target="_blank" style="color:var(--sky)">(Info)</a>` : ''), h.description, h.references, accent, icon);
        });
    }

    if (guide.security_safety && (guide.security_safety.summary || guide.security_safety.crime_report)) {
        leftCol += `<div class="np-report-item--non-sailing">`;
        const s = guide.security_safety;
        const riskAccent = (s.risk_level || '').toLowerCase() === 'high' ? 'coral' : (s.risk_level || '').toLowerCase() === 'medium' ? 'amber' : 'teal';
        leftCol += sectionChip('Security & Safety', riskAccent);
        leftCol += hazardCard(
            `Risk: ${s.risk_level || 'Low'}`,
            s.summary || '',
            s.references,
            riskAccent,
            'lock'
        );
        if (s.safety_tips && s.safety_tips.length > 0) {
            leftCol += `<ul class="np-safety-tips">` +
                s.safety_tips.map(t => `<li>${t}</li>`).join('') + `</ul>`;
        }
        leftCol += `</div>`;
    }

    // Major Hubs moved to full-width section below the 2-col grid
    let hubsSection = '';
    if (guide.hubs && guide.hubs.length > 0) {
        hubsSection += `<div class="np-report-item--non-sailing">`;
        hubsSection += sectionChip('Major Hubs', 'amber');
        hubsSection += `<div class="np-hub-grid">`;
        guide.hubs.forEach(h => {
            hubsSection += `<div class="np-hub-card">
                <div class="np-hub-card__name">${h.name}${h.url ? ` <a href="${h.url}" target="_blank" style="color:var(--sky);font-weight:400">(Website)</a>` : ''}</div>
                <p class="np-hub-card__desc">${h.description}</p>
                ${h.references && h.references.length ? renderReferences(h.references) : ''}
            </div>`;
        });
        hubsSection += `</div></div>`;
    }

    if (guide.points_of_interest && guide.points_of_interest.length > 0) {
        leftCol += `<div class="np-report-item--non-sailing">`;
        leftCol += sectionChip('Points of Interest', 'violet');
        guide.points_of_interest.forEach(poi => {
            leftCol += `<div class="np-poi-item">
                <div class="np-poi-item__name">${poi.name}${poi.url ? ` <a href="${poi.url}" target="_blank" style="color:var(--sky);font-weight:400">(Website)</a>` : ''}</div>
                <p class="np-poi-item__desc">${poi.description}</p>
                ${poi.references && poi.references.length ? renderReferences(poi.references) : ''}
            </div>`;
        });
        leftCol += `</div>`;
    }

    // Sailing Season + At a Glance (right col)
    let rightCol = '';

    if (guide.sailing_season) {
        const s = guide.sailing_season;
        const bestMonths = s.primary_season_months || [];
        const stormMonths = s.storm_season_months || [];
        rightCol += `<div class="np-report-item--non-sailing">`;
        rightCol += sectionChip('Sailing Season', 'teal');
        rightCol += `<div class="np-season-card">`;
        rightCol += `<div class="np-season-month-grid">`;
        MONTHS.forEach((mo, i) => {
            const num = i + 1;
            const isBest = bestMonths.includes(num) || bestMonths.includes(mo) || bestMonths.some(m => String(m).startsWith(mo));
            const isStorm = stormMonths.includes(num) || stormMonths.includes(mo) || stormMonths.some(m => String(m).startsWith(mo));
            const mod = isBest ? 'best' : isStorm ? 'storm' : 'off';
            rightCol += `<div class="np-season-month np-season-month--${mod}">${mo}</div>`;
        });
        rightCol += `</div>`;
        rightCol += `<div class="np-season-legend">
            <span><span class="np-season-legend__dot np-season-legend__dot--best"></span>Best</span>
            <span><span class="np-season-legend__dot np-season-legend__dot--storm"></span>Storm</span>
        </div>`;
        if (s.notes) {
            rightCol += `<p class="np-caption" style="margin-top:8px">${s.notes}</p>`;
        }
        rightCol += `${s.references && s.references.length ? renderReferences(s.references) : ''}</div></div>`;
    }

    // At a glance data tiles
    const glanceTiles = [];
    if (guide.country_info) {
        const c = guide.country_info;
        if (c.name)     glanceTiles.push({ label: 'Country', value: c.name, accent: 'sky' });
        if (c.timezone) glanceTiles.push({ label: 'Timezone', value: c.timezone, accent: 'violet' });
        if (c.languages && c.languages.length) glanceTiles.push({ label: 'Language', value: c.languages[0], accent: 'teal' });
    }
    if (guide.currencies && guide.currencies.length > 0) {
        const cur = guide.currencies[0];
        glanceTiles.push({ label: 'Currency', value: `${cur.symbol || ''} ${cur.code}`.trim(), accent: 'amber' });
    }
    if (glanceTiles.length > 0) {
        rightCol += `<div class="np-report-item--non-sailing">`;
        rightCol += sectionChip('At a Glance', 'sky');
        rightCol += `<div class="np-glance-grid">`;
        glanceTiles.forEach(({ label, value, accent }) => {
            rightCol += `<div class="np-glance-tile" style="--accent:var(--${accent})">
                <div class="np-glance-tile__label">${label}</div>
                <div class="np-glance-tile__value">${value}</div>
            </div>`;
        });
        rightCol += `</div></div>`;
    }

    let charterAirportsHTML = '';

    if (guide.charter_info) {
        const c = guide.charter_info;
        charterAirportsHTML += `<div class="np-charter-airports-col np-report-item--non-sailing">`;
        charterAirportsHTML += sectionChip('Charter Info', 'amber');
        charterAirportsHTML += `<div class="np-charter-card">
            <div>Available: <strong style="color:var(--ink)">${c.is_charter_destination ? 'Yes ✓' : 'No'}</strong></div>`;
        if (c.companies && c.companies.length > 0) {
            charterAirportsHTML += `<ul class="np-charter-list">` +
                c.companies.map(comp => {
                    if (typeof comp === 'string') return `<li>${comp}</li>`;
                    return `<li>${comp.url ? `<a href="${comp.url}" target="_blank" style="color:var(--sky)">${comp.name}</a>` : comp.name}${comp.references && comp.references.length ? renderReferences(comp.references) : ''}</li>`;
                }).join('') + `</ul>`;
        }
        charterAirportsHTML += `</div></div>`;
    }

    if (guide.airports && guide.airports.length > 0) {
        charterAirportsHTML += `<div class="np-charter-airports-col np-report-item--non-sailing">`;
        charterAirportsHTML += sectionChip('Nearest Airports', 'sky');
        guide.airports.forEach(a => {
            const type = a.type ? a.type.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(' ') : '';
            charterAirportsHTML += `<div class="np-airport-row">
                <span class="np-airport-row__icon material-symbols-outlined" aria-hidden="true">local_airport</span>
                <div>
                    <div><span class="np-airport-row__name">${a.name}</span> <span class="np-airport-row__code">(${a.iata_code || 'N/A'})</span></div>
                    <div class="np-airport-row__meta">${type}${a.distance_km ? ` · ${a.distance_km} km` : ''}</div>
                </div>
            </div>`;
        });
        charterAirportsHTML += `</div>`;
    }

    if (charterAirportsHTML) {
        rightCol += `<div class="np-charter-airports-row">${charterAirportsHTML}</div>`;
    }

    return `
        <div class="np-guide-grid">
            <div>${leftCol}</div>
            <div>${rightCol}</div>
        </div>
        ${hubsSection}
    `;
}

function alertToHTML(a) {
    const hasTbl = Array.isArray(a.travel_table) && a.travel_table.length > 0;
    const hasDep = hasTbl && a.travel_table.some(r => r.depart_by);
    const tblHTML = hasTbl ? `
        <table class="lookout-travel-table">
            <thead><tr>
                <th>Speed</th><th>Travel time</th>${hasDep ? '<th>Depart by</th>' : ''}
            </tr></thead>
            <tbody>${a.travel_table.map(r => `<tr>
                <td>${esc(String(r.speed_kt))} kt</td>
                <td>${esc(r.travel_time || '')}</td>
                ${hasDep ? `<td>${esc(r.depart_by || '—')}</td>` : ''}
            </tr>`).join('')}</tbody>
        </table>` : '';
    const travelClass = hasTbl ? 'lookout-alert--travel' : `lookout-alert--${esc(a.severity || 'info')}`;
    return `<div class="lookout-alert ${travelClass}">
        <span class="material-symbols-outlined lookout-alert__icon" aria-hidden="true">${esc(a.icon || 'warning')}</span>
        <div class="lookout-alert__text">
            <span class="lookout-alert__msg">${esc(a.message || '')}</span>
            ${tblHTML}
            ${a.action ? `<span class="lookout-alert__action">${esc(a.action)}</span>` : ''}
        </div>
    </div>`;
}

function generateReportHTML(voyage, stops, briefings, guide, recommendations, hasBriefings, mapURL) {
    const sortedStops = [...stops].sort((a, b) =>
        new Date(a.target_date) - new Date(b.target_date)
    );
    const isInvalidVal = (v) => {
        if (!v) return true;
        return ['n/a','unknown','not specified'].includes(String(v).toLowerCase().trim());
    };
    const fmtSunTime = (t) => {
        if (!t) return null;
        try {
            const d = new Date(t);
            if (isNaN(d.getTime())) return t;
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        } catch (e) { return t; }
    };

    let dateDisplay = 'Dates Pending';
    if (voyage.start_date && voyage.end_date) {
        const s = new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone:'UTC',month:'short',day:'numeric'});
        const e = new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone:'UTC',month:'short',day:'numeric',year:'numeric'});
        dateDisplay = `${s} – ${e}`;
    }

    // ── Stat tiles ────────────────────────────────────────────────────────────
    const isDated = !!(voyage.start_date && voyage.end_date);
    const researchedCount = briefings.filter(b => b !== null).length;
    const dayCount = isDated
        ? Math.round((new Date(voyage.end_date) - new Date(voyage.start_date)) / 86400000) + 1 : '--';
    const avgWind = (() => {
        const speeds = briefings.filter(Boolean).map(b => parseFloat((b.weather_summary || {}).wind_speed_kt || 0)).filter(n => n > 0);
        return speeds.length ? Math.round(speeds.reduce((a,b) => a+b, 0) / speeds.length) + ' kt' : '--';
    })();

    // For undated voyages, count facility types from recommendations instead
    const recList = Array.isArray(recommendations) ? recommendations : [];
    const countType = (keyword) => recList.filter(r => (r.type || '').toLowerCase().includes(keyword)).length;
    const marinaCount    = countType('marina') + countType('yacht') + countType('hub') + countType('chandl') + countType('provision') + countType('repair') + countType('supply');
    const mooringCount   = countType('moor');
    const anchorageCount = countType('anchor');
    const poiCount       = (guide && Array.isArray(guide.points_of_interest)) ? guide.points_of_interest.length : 0;

    const overviewCol = (guide && guide.summary) ? `
        <div class="np-destination-overview np-report-item--non-sailing">
            <span class="np-destination-overview__eyebrow">Destination Overview</span>
            <blockquote class="np-destination-overview__quote">${DOMPurify.sanitize(guide.summary)}</blockquote>
            <span class="np-destination-overview__cta">↓ Full guide below</span>
        </div>` : '';

    // ── Hero: header + stats (left) / overview (right) ───────────────────────
    let html = `
        <div class="np-report-hero">
            <div class="np-report-hero__left">
                <div class="np-report-header">
                    <span class="np-report-header__tag">Voyage Report</span>
                    <h1 class="np-report-header__title">${DOMPurify.sanitize(voyage.title)}</h1>
                    <p class="np-report-header__meta">${dateDisplay} · ${DOMPurify.sanitize(displayLocationName(voyage.location_name))}</p>
                </div>
                <div class="np-metric-grid np-report-item--non-sailing">
                    ${isDated ? `
                    <div class="np-metric-tile" style="--accent:var(--sky)">
                        <div class="np-metric-tile__label">Stops</div>
                        <div class="np-metric-tile__value">${sortedStops.length}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--teal)">
                        <div class="np-metric-tile__label">Days</div>
                        <div class="np-metric-tile__value">${dayCount}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--amber)">
                        <div class="np-metric-tile__label">Avg Wind</div>
                        <div class="np-metric-tile__value">${avgWind}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--violet)">
                        <div class="np-metric-tile__label">Researched</div>
                        <div class="np-metric-tile__value">${researchedCount}/${sortedStops.length}</div>
                    </div>` : `
                    <div class="np-metric-tile" style="--accent:var(--amber)">
                        <div class="np-metric-tile__label">Marinas & Hubs</div>
                        <div class="np-metric-tile__value">${marinaCount || '—'}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--violet)">
                        <div class="np-metric-tile__label">Moorings</div>
                        <div class="np-metric-tile__value">${mooringCount || '—'}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--teal)">
                        <div class="np-metric-tile__label">Anchorages</div>
                        <div class="np-metric-tile__value">${anchorageCount || '—'}</div>
                    </div>
                    <div class="np-metric-tile" style="--accent:var(--green)">
                        <div class="np-metric-tile__label">Points of Interest</div>
                        <div class="np-metric-tile__value">${poiCount || '—'}</div>
                    </div>`}
                </div>
            </div>
            ${overviewCol}
        </div>
    `;

    // ── Map Snapshot ──────────────────────────────────────────────────────────
    if (mapURL) {
        const sep = mapURL.includes('?') ? '&' : '?';
        const url = `${mapURL}${sep}t=${Date.now()}`;
        html += `<img src="${url}" alt="Voyage Map" class="np-report-map np-report-item--non-sailing" />`;
    }

    // ── Day by day grid ───────────────────────────────────────────────────────
    if (hasBriefings) {
        const nmBetween = (a, b) => {
            if (!a.latitude || !a.longitude || !b.latitude || !b.longitude) return null;
            const toRad = d => d * Math.PI / 180;
            const R = 3440.065;
            const dLat = toRad(b.latitude - a.latitude);
            const dLon = toRad(b.longitude - a.longitude);
            const x = Math.sin(dLat/2)**2 + Math.cos(toRad(a.latitude)) * Math.cos(toRad(b.latitude)) * Math.sin(dLon/2)**2;
            return Math.round(2 * R * Math.asin(Math.sqrt(x)));
        };

        const dayGridClass = sortedStops.length < 5 ? 'np-day-grid np-day-grid--fill' : 'np-day-grid';
        html += `<span class="np-section-label">Day by Day</span>`;
        html += `<div class="${dayGridClass}" style="--day-count:${sortedStops.length}">`;
        sortedStops.forEach((stop, idx) => {
            const briefing = briefings.find(br => br && br.stop_id === stop.id) || {};
            const accent = VOYAGE_ACCENTS[idx % VOYAGE_ACCENTS.length];
            const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });
            const w = briefing.weather_summary || {};
            const sun = briefing.sun_phase || {};
            const temp = (w.temp_max_f && w.temp_min_f) ? `${Math.round(w.temp_max_f)}° / ${Math.round(w.temp_min_f)}°` : '';
            const sunriseStr = fmtSunTime(sun.sunrise);
            const sunsetStr  = fmtSunTime(sun.sunset);
            const canvasId = `reportMiniTideChart_${idx}`;
            const nextStop = sortedStops[idx + 1];
            const distNm = nextStop ? nmBetween(stop, nextStop) : null;
            html += `
                <div class="np-day-tile" style="--accent:var(--${accent})">
                    <div class="np-day-tile__label">Day ${idx+1} · ${dateStr}</div>
                    <div class="np-day-tile__name">${DOMPurify.sanitize(displayLocationName(stop.location_name).split(',')[0].trim())}</div>
                    ${w.condition ? `<div class="np-day-tile__meta"><span class="material-symbols-outlined np-day-tile__icon">${getIconForWeather(w.condition)}</span>${w.condition}</div>` : ''}
                    ${temp ? `<div class="np-day-tile__meta"><span class="material-symbols-outlined np-day-tile__icon">thermometer</span>${temp}</div>` : ''}
                    ${w.wind_speed_kt ? `<div class="np-day-tile__meta"><span style="width:14px;height:14px;display:inline-block;vertical-align:middle;margin-right:4px">${getWindArrowSVG(directionToDegrees(w.wind_direction), getWindScale(w.wind_speed_kt) * 0.7)}</span>${w.wind_speed_kt} kt ${w.wind_direction || ''}</div>` : ''}
                    ${sunriseStr ? `<div class="np-day-tile__meta"><span class="material-symbols-outlined np-day-tile__icon">wb_twilight</span>↑ ${sunriseStr}</div>` : ''}
                    ${sunsetStr  ? `<div class="np-day-tile__meta"><span class="material-symbols-outlined np-day-tile__icon">wb_twilight</span>↓ ${sunsetStr}</div>` : ''}
                    ${distNm !== null ? `<div class="np-day-tile__meta"><span class="material-symbols-outlined np-day-tile__icon">sailing</span>${distNm} nm to next stop</div>` : '<div class="np-day-tile__meta np-day-tile__meta--placeholder">&nbsp;</div>'}
                    ${briefing.weather_last_updated ? `<div class="np-day-tile__meta" style="font-style:italic;opacity:0.7"><span class="material-symbols-outlined np-day-tile__icon">update</span>${new Date(briefing.weather_last_updated).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}</div>` : ''}
                    <div class="np-day-tile__chart overview-chart">
                        <canvas id="${canvasId}" data-tide-json='${JSON.stringify(briefing.tides || {}).replace(/'/g, "&apos;")}' data-date="${stop.target_date}"></canvas>
                    </div>
                </div>`;
        });
        html += `</div>`;

        // ── Safety Overview ───────────────────────────────────────────────────
        const stopsWithAlerts = sortedStops
            .map((stop, idx) => ({ stop, briefing: briefings.find(br => br && br.stop_id === stop.id), idx }))
            .filter(({ briefing }) => briefing && Array.isArray(briefing.safety_alerts) && briefing.safety_alerts.length > 0);

        if (stopsWithAlerts.length > 0) {
            const severityOrder = { danger: 0, warning: 1, info: 2 };
            const topSeverity = stopsWithAlerts.reduce((top, { briefing }) => {
                const s = briefing.safety_alerts.reduce((t, a) =>
                    (severityOrder[a.severity] ?? 2) < (severityOrder[t] ?? 2) ? a.severity : t, 'info');
                return (severityOrder[s] ?? 2) < (severityOrder[top] ?? 2) ? s : top;
            }, 'info');

            html += `<div class="lookout-box np-safety-overview np-report-item--non-sailing" id="np-safety-overview">
                <div class="lookout-header">
                    <span class="material-symbols-outlined" aria-hidden="true">visibility</span>
                    <h3>Safety Overview</h3>
                </div>
                <div class="lookout-alert-list">`;

            stopsWithAlerts.forEach(({ stop, briefing, idx }) => {
                const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, { timeZone: 'UTC', weekday: 'short', month: 'short', day: 'numeric' });
                const stopName = displayLocationName(stop.location_name).split(',')[0].trim();
                html += `<div class="np-safety-overview__stop-header">
                    <span class="np-safety-overview__day">Day ${idx + 1}</span>
                    <span class="np-safety-overview__name">${esc(stopName)}</span>
                    <span class="np-safety-overview__date">${dateStr}</span>
                </div>`;
                briefing.safety_alerts.forEach(a => { html += alertToHTML(a); });
            });

            const oldestAudit = stopsWithAlerts
                .map(({ briefing }) => briefing.safety_alerts_updated_at)
                .filter(Boolean)
                .map(t => new Date(t))
                .sort((a, b) => a - b)[0];
            if (oldestAudit) {
                html += `<p class="np-safety-overview__updated">
                    <span class="material-symbols-outlined" aria-hidden="true">update</span>
                    Audited ${oldestAudit.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}
                </p>`;
            }

            html += `</div></div>`;
        }
    }

    // ── Full Destination Guide ────────────────────────────────────────────────
    if (guide) {
        html += `
            <div class="np-guide-section">
                <span class="np-guide-section__eyebrow">Destination Guide</span>
                ${generateGuideHTML(guide)}
            </div>
        `;
    }

    // ── Recommendations (discovery mode) ─────────────────────────────────────
    if (!hasBriefings && recommendations && recommendations.length > 0) {
        const groups = {
            marina:    { title: 'Resource Hubs & Marinas', accent: 'amber', items: [] },
            anchorage: { title: 'Recommended Anchorages', accent: 'teal',  items: [] },
            mooring:   { title: 'Mooring Fields',          accent: 'violet',items: [] },
            other:     { title: 'Other Recommendations',   accent: 'sky',   items: [] },
        };
        recommendations.forEach(rec => {
            const t = (rec.type || '').toLowerCase();
            if (t.includes('marina') || t.includes('hub')) groups.marina.items.push(rec);
            else if (t.includes('anchor')) groups.anchorage.items.push(rec);
            else if (t.includes('mooring')) groups.mooring.items.push(rec);
            else groups.other.items.push(rec);
        });
        ['marina','anchorage','mooring','other'].forEach(key => {
            const { title, accent, items } = groups[key];
            if (!items.length) return;
            html += `<div class="np-rec-section">
                <span class="np-eyebrow-accent" style="--accent:var(--${accent})">${title}</span>`;
            items.forEach(rec => {
                const ra = markerAccent(rec.type);
                html += `
                    <div class="np-rec-item" style="--accent:var(--${ra})">
                        <div class="np-rec-item__header">
                            ${DOMPurify.sanitize(rec.name)}
                            <span class="np-rec-item__type">${DOMPurify.sanitize(rec.type || 'Spot')}</span>
                        </div>
                        <p class="np-rec-item__desc">${DOMPurify.sanitize(rec.description)}</p>
                        ${rec.reasoning ? `<p class="np-rec-item__reasoning">"${DOMPurify.sanitize(rec.reasoning)}"</p>` : ''}
                    </div>`;
            });
            html += `</div>`;
        });
    }

    // ── Daily Itinerary sections ──────────────────────────────────────────────
    if (hasBriefings) {
        sortedStops.forEach((stop, idx) => {
            const b = briefings.find(br => br && br.stop_id === stop.id);
            if (!b) return;
            const accent = VOYAGE_ACCENTS[idx % VOYAGE_ACCENTS.length];
            const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, {timeZone:'UTC', weekday:'long', month:'long', day:'numeric'});
            const w = b.weather_summary || {};
            const sun = b.sun_phase || {};

            html += `
                <div class="np-report-stop" style="--accent:var(--${accent})">
                    <div class="np-report-stop__header">
                        <div class="np-report-stop__number">${idx+1}</div>
                        <div>
                            <div class="np-report-stop__title">${DOMPurify.sanitize(displayLocationName(stop.location_name))}</div>
                            <div class="np-report-stop__date">${dateStr}</div>
                        </div>
                    </div>
            `;

            // Weather tiles
            if (!isInvalidVal(w.condition)) {
                html += `<div class="np-weather-grid">`;
                if (!isInvalidVal(w.condition)) html += `<div class="np-weather-tile" style="--accent:var(--sky)"><div class="np-weather-tile__label">Conditions</div><div class="np-weather-tile__value">${w.condition}</div></div>`;
                if (w.wind_speed_kt) html += `<div class="np-weather-tile" style="--accent:var(--teal)"><div class="np-weather-tile__label">Wind</div><div class="np-weather-tile__value">${w.wind_speed_kt} kt</div></div>`;
                if (w.temp_max_f) html += `<div class="np-weather-tile" style="--accent:var(--amber)"><div class="np-weather-tile__label">Temp</div><div class="np-weather-tile__value">${Math.round(w.temp_max_f)}°F</div></div>`;
                const sunriseStr = fmtSunTime(sun.sunrise);
                const sunsetStr  = fmtSunTime(sun.sunset);
                if (sunriseStr) html += `<div class="np-weather-tile" style="--accent:var(--amber)"><div class="np-weather-tile__label">Sunrise</div><div class="np-weather-tile__value">${sunriseStr}</div></div>`;
                if (sunsetStr)  html += `<div class="np-weather-tile" style="--accent:var(--violet)"><div class="np-weather-tile__label">Sunset</div><div class="np-weather-tile__value">${sunsetStr}</div></div>`;
                html += `</div>`;

                // Add 24h Hourly Timeline
                const timelineHTML = generateHourlyTimelineHTML(w, sun);
                if (timelineHTML) {
                    html += `<div class="np-hourly-timeline">${timelineHTML}</div>`;
                }

                if (b.weather_last_updated) {
                    const updatedAt = new Date(b.weather_last_updated);
                    const label = updatedAt.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
                    html += `<p style="margin:4px 0 8px;font-size:11px;color:var(--muted);font-style:italic">Forecast as of ${label}</p>`;
                }
            }

            // Safety alerts
            if (Array.isArray(b.safety_alerts) && b.safety_alerts.length > 0) {
                const alertsHtml = b.safety_alerts.map(alertToHTML).join('');
                html += `<div class="lookout-box" style="margin-bottom:16px">
                    <div class="lookout-header">
                        <span class="material-symbols-outlined" aria-hidden="true">visibility</span>
                        <h3>Safety Lookout</h3>
                    </div>
                    <div class="lookout-alert-list">${alertsHtml}</div>
                </div>`;
            }

            // Tide chart canvas (preserved for Chart.js rendering)
            if (b.tides && b.tides.events) {
                html += `<div class="np-tide-chart-wrap"><canvas id="reportTideChart_${idx}"></canvas></div>`;
            }

            // Facilities
            const firstStop = sortedStops[0];
            const isLastStop = idx === sortedStops.length - 1 && sortedStops.length > 1;
            const isReturnStop = isLastStop && (() => {
                const toRad = d => d * Math.PI / 180;
                const R = 3440.065;
                const dLat = toRad(stop.latitude - firstStop.latitude);
                const dLon = toRad(stop.longitude - firstStop.longitude);
                const a = Math.sin(dLat/2)**2 + Math.cos(toRad(firstStop.latitude)) * Math.cos(toRad(stop.latitude)) * Math.sin(dLon/2)**2;
                return 2 * R * Math.asin(Math.sqrt(a)) < 1;
            })();

            if (isReturnStop) {
                html += `<p class="np-caption" style="font-style:italic">Facilities omitted — return to starting area.</p>`;
            } else if (b.facilities && b.facilities.length > 0) {
                html += `<div class="np-report-item--non-sailing">`;
                html += `<span class="np-facilities-header">Facilities</span>`;
                b.facilities.forEach(f => {
                    const fa = markerAccent(f.type);
                    const desc = typeof f.details === 'string' ? f.details : (f.details?.description || '');
                    const address = f.address || (typeof f.details === 'object' && f.details?.address) || '';
                    html += `<div class="np-facility-report-card" style="--accent:var(--${fa})">
                        <div class="np-facility-report-card__header">
                            <span class="np-facility-report-card__name">${esc(f.name)}</span>
                            <span class="np-facility-report-card__badge">${esc(f.type || 'Facility')}</span>
                        </div>
                        ${address ? `<p class="np-facility-report-card__address">${esc(address)}</p>` : ''}
                        ${desc ? `<p class="np-facility-report-card__desc">${esc(desc)}</p>` : ''}
                        ${f.rating ? `<p class="np-facility-report-card__rating">${'★'.repeat(Math.round(f.rating))}${'☆'.repeat(5-Math.round(f.rating))} ${f.rating.toFixed(1)}</p>` : ''}
                        ${f.website ? `<a href="${f.website}" target="_blank" class="np-facility-report-card__website">${esc(f.website)}</a>` : ''}
                        ${renderReferences(f.references)}
                    </div>`;
                });
                html += `</div>`;
            }

            html += `</div>`;
        });
    }

    html += `<footer class="np-report-footer"><p>Generated by NavalPlan</p></footer>`;
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
// ─── Print View ───────────────────────────────────────────────────────────────

async function initPrintMode(voyageId) {
    document.body.classList.add('print-view');
    document.documentElement.classList.add('print-view');
    const app = document.getElementById('app');
    app.innerHTML = '<div class="loading-state"><span class="material-symbols-outlined spin loading-icon">sync</span><p>Preparing print view…</p></div>';

    let person;
    try { person = await API.getPerson(); } catch (e) { /* treat as logged out */ }
    if (!person) {
        app.innerHTML = '<div class="error-state text-center p-xl"><h2 class="text-dark">Sign in required</h2><p>Please <a href="/">sign in</a> to view this report.</p></div>';
        return;
    }

    try {
        const [pilotReport, stops] = await Promise.all([
            API.getPilotReport(voyageId),
            API.getStops(voyageId),
        ]);
        const sortedStops = [...stops].sort((a, b) => new Date(a.target_date) - new Date(b.target_date));
        const briefings = await Promise.all(sortedStops.map(s => API.getBriefing(s.id).catch(() => null)));

        renderPrintReport({
            voyage: pilotReport.voyage,
            guide: pilotReport.guide,
            stops,
            briefings,
            recommendations: pilotReport.recommendations,
            map_url: pilotReport.map_url,
        }, app);
    } catch (err) {
        console.error(err);
        app.innerHTML = '<div class="error-state text-center p-xl"><h2 class="text-dark">Report Not Found</h2><p>Unable to load this voyage.</p></div>';
    }
}

function renderPrintReport(data, container) {
    const guide = data.guide || {};
    const voyage = data.voyage || {};
    const stops = data.stops || [];
    const briefings = data.briefings || [];
    const recommendations = data.recommendations || [];
    const hasBriefings = briefings.some(b => b !== null);
    const mapURL = data.map_url;

    const printBar = `
        <div class="np-print-bar">
            <a href="/voyages/${voyage.id || ''}" class="btn secondary">← Back</a>
            <button class="btn primary" onclick="window.print()">
                <span class="material-symbols-outlined" style="vertical-align:middle;font-size:18px">print</span>
                Print / Save as PDF
            </button>
        </div>`;

    const html = generateReportHTML(voyage, stops, briefings, guide, recommendations, hasBriefings, mapURL);

    container.innerHTML = DOMPurify.sanitize(
        `${printBar}<div class="print-report-content">${html}</div>`,
        { ADD_ATTR: ['target'] }
    );

    // Re-attach print bar button (DOMPurify strips onclick; use event delegation instead)
    const printBtn = container.querySelector('.np-print-bar .btn.primary');
    if (printBtn) printBtn.addEventListener('click', () => window.print());

    // Move metric grid to sit beside the map in a flex row
    const metricGrid = container.querySelector('.np-metric-grid');
    const map = container.querySelector('.np-report-map');
    if (metricGrid && map) {
        const row = document.createElement('div');
        row.className = 'np-print-map-row';
        map.parentNode.insertBefore(row, map);
        row.appendChild(map);
        row.appendChild(metricGrid);
    }

    setTimeout(() => {
        const sortedStops = [...stops].sort((a, b) => new Date(a.target_date) - new Date(b.target_date));
        sortedStops.forEach((stop, idx) => {
            const b = briefings.find(br => br && br.stop_id === stop.id) || {};
            if (b.tides && b.tides.events) {
                renderTideChart(`reportTideChart_${idx}`, b.tides, stop.target_date);
                renderMiniTideChart(`reportMiniTideChart_${idx}`, b.tides, stop.target_date);
            }
        });
    }, 100);
}

// ─── Onboarding ───────────────────────────────────────────────────────────────

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
