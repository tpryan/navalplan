import mapboxgl from 'mapbox-gl';
import { API } from './api.js';

// Configuration
const MAPBOX_TOKEN = __MAPBOX_TOKEN__; 

// State
let voyages = [];
let currentVoyage = null;
let currentStops = [];
let selectedDate = null;
let map = null;
let markers = [];

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  initMap();
  initUI();
  loadVoyages();
}

function initUI() {
  const btnNewVoyage = document.getElementById('btn-new-voyage');
  const modalOverlay = document.getElementById('modal-overlay');
  const modalNewVoyage = document.getElementById('modal-new-voyage');
  const btnCancelVoyage = document.getElementById('btn-cancel-voyage');
  const formNewVoyage = document.getElementById('form-new-voyage');
  const btnBack = document.getElementById('btn-back-voyages');

  // Open Modal
  btnNewVoyage.addEventListener('click', () => {
    modalOverlay.classList.remove('hidden');
    modalNewVoyage.classList.remove('hidden');
    // Set default dates (Next Sat to +1 week)
    const today = new Date().toISOString().split('T')[0];
    document.getElementById('voyage-start').value = today;
  });

  // Close Modal Helper
  const closeModal = () => {
    modalOverlay.classList.add('hidden');
    modalNewVoyage.classList.add('hidden');
    formNewVoyage.reset();
  };

  btnCancelVoyage.addEventListener('click', closeModal);
  modalOverlay.addEventListener('click', closeModal);

  // Auto-set End Date
  const inputStart = document.getElementById('voyage-start');
  const inputEnd = document.getElementById('voyage-end');

  inputStart.addEventListener('change', () => {
    if (inputStart.value && !inputEnd.value) {
      // Input date "YYYY-MM-DD" is parsed as UTC midnight
      const d = new Date(inputStart.value);
      d.setUTCDate(d.getUTCDate() + 1);
      inputEnd.value = d.toISOString().split('T')[0];
    }
  });

  // Handle Form Submit
  formNewVoyage.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(formNewVoyage);
    const voyageData = {
      title: formData.get('title'),
      start_date: formData.get('start_date') + 'T00:00:00Z',
      end_date: formData.get('end_date') + 'T00:00:00Z'
    };

    try {
      await API.createVoyage(voyageData);
      closeModal();
      loadVoyages(); // Refresh list
    } catch (err) {
      console.error(err);
      alert('Failed to create voyage. Check console.');
    }
  });

  // Back Button
  if (btnBack) {
    btnBack.addEventListener('click', showVoyageList);
  }
}

async function loadVoyages() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '<p class="loading-text">Loading voyages...</p>';

  try {
    voyages = await API.getVoyages();
    renderVoyageList();
  } catch (err) {
    console.error(err);
    listContainer.innerHTML = '<p class="loading-text error">Failed to load voyages.</p>';
  }
}

function renderVoyageList() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '';

  if (!voyages || voyages.length === 0) {
    listContainer.innerHTML = '<p class="loading-text">No voyages yet. Plan your first trip!</p>';
    return;
  }

  voyages.forEach(voyage => {
    const el = document.createElement('div');
    el.className = 'voyage-item';
    el.innerHTML = `
      <h3>${voyage.title}</h3>
      <p>${new Date(voyage.start_date).toLocaleDateString()} - ${new Date(voyage.end_date).toLocaleDateString()}</p>
    `;
    el.addEventListener('click', () => selectVoyage(voyage));
    listContainer.appendChild(el);
  });
}

function showVoyageList() {
    document.getElementById('voyage-list').classList.remove('hidden');
    document.getElementById('itinerary-view').classList.add('hidden');
    // Hide/Show "New Voyage" button logic (it's inside sidebar-actions which is inside nav, separate from voyage-list)
    // Actually, 'sidebar-actions' contains the button. 'itinerary-view' contains the back button.
    // I need to hide 'sidebar-actions' when in itinerary view?
    // The HTML structure:
    // <nav id="sidebar">
    //   <div class="brand">...</div>
    //   <div class="sidebar-actions"><button id="btn-new-voyage">...</div>
    //   <div id="voyage-list">...</div>
    //   <div id="itinerary-view" class="hidden">...</div>
    // </nav>
    
    // So I should hide .sidebar-actions when showing itinerary.
    document.querySelector('.sidebar-actions').classList.remove('hidden');
    
    currentVoyage = null;
    selectedDate = null;
    clearMap();
}

async function selectVoyage(voyage) {
    currentVoyage = voyage;
    document.getElementById('voyage-list').classList.add('hidden');
    document.getElementById('itinerary-view').classList.remove('hidden');
    document.querySelector('.sidebar-actions').classList.add('hidden');

    document.getElementById('itinerary-title').textContent = voyage.title;
    document.getElementById('itinerary-dates').textContent = `${new Date(voyage.start_date).toLocaleDateString()} - ${new Date(voyage.end_date).toLocaleDateString()}`;

    // Load Stops
    try {
        currentStops = await API.getStops(voyage.id);
        renderItinerary();
        renderMapStops();
    } catch (err) {
        console.error(err);
        alert('Failed to load stops');
    }
}

function renderItinerary() {
    const list = document.getElementById('itinerary-list');
    list.innerHTML = '';
    
    let currentDate = new Date(currentVoyage.start_date);
    const endDate = new Date(currentVoyage.end_date);

    while (currentDate <= endDate) {
        const dateStr = currentDate.toISOString().split('T')[0];
        const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
        
        const el = document.createElement('div');
        el.className = `day-item ${selectedDate === dateStr ? 'selected' : ''}`;
        el.innerHTML = `
            <span class="day-date">${currentDate.toLocaleDateString(undefined, {month:'short', day:'numeric'})}</span>
            <span class="day-location ${stop ? 'set' : ''}">${stop ? stop.location_name : 'No destination'}</span>
        `;
        el.addEventListener('click', () => selectDate(dateStr));
        list.appendChild(el);
        
        currentDate.setUTCDate(currentDate.getUTCDate() + 1);
    }
}

function selectDate(dateStr) {
    selectedDate = dateStr;
    renderItinerary(); // Re-render to show selection highlight
    
    // Zoom to existing stop if present
    const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
    if (stop && map) {
        map.flyTo({ center: [stop.longitude, stop.latitude], zoom: 10 });
    }
}

function initMap() {
  if (!MAPBOX_TOKEN) {
    console.error('Mapbox token is missing. Please set NAVALPLAN_MB_TOKEN environment variable during build.');
    return;
  }

  mapboxgl.accessToken = MAPBOX_TOKEN;

  map = new mapboxgl.Map({
    container: 'map-container',
    style: __MAPBOX_STYLE__, 
    center: [-123.0, 48.5], // Salish Sea
    zoom: 8
  });

  map.on('load', () => {
    console.log('NavalPlan: Map Loaded Successfully');
  });

  map.addControl(new mapboxgl.NavigationControl());

  map.on('click', async (e) => {
    if (!currentVoyage || !selectedDate) return;

    const { lng, lat } = e.lngLat;
    const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));

    // Get features at click point
    const features = map.queryRenderedFeatures(e.point);
    console.log('Clicked Features:', features);
    
    // Attempt to find a label
    let locationName = `Location ${lat.toFixed(3)}, ${lng.toFixed(3)}`;
    // Prioritize specific layers or just look for 'name' property
    const labelFeature = features.find(f => f.properties && (f.properties.name || f.properties.name_en));
    
    if (labelFeature) {
        locationName = labelFeature.properties.name || labelFeature.properties.name_en;
        console.log('Found Label:', locationName);
    }

    // Create or Update
    const stopData = {
        target_date: selectedDate + 'T00:00:00Z',
        location_name: locationName,
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
    } catch (err) {
        console.error(err);
        alert('Failed to save stop');
    }
  });
}

function renderMapStops() {
    clearMap();
    if (!map) return;

    // Sort stops by date
    const sortedStops = [...currentStops].sort((a, b) => 
        new Date(a.target_date) - new Date(b.target_date)
    );

    // Add Markers
    sortedStops.forEach((stop, index) => {
        const el = document.createElement('div');
        el.className = 'marker';
        el.innerHTML = `<span><b>${index + 1}</b></span>`;

        const marker = new mapboxgl.Marker(el)
            .setLngLat([stop.longitude, stop.latitude])
            .setPopup(new mapboxgl.Popup({ offset: 25 }).setText(`${stop.location_name} (Day ${index + 1})`))
            .addTo(map);
        markers.push(marker);
    });

    // Draw Line
    const coords = sortedStops.map(s => [s.longitude, s.latitude]);
    
    if (map.getSource('route')) {
        map.getSource('route').setData({
            type: 'Feature',
            properties: {},
            geometry: {
                type: 'LineString',
                coordinates: coords
            }
        });
    } else {
        map.addSource('route', {
            type: 'geojson',
            data: {
                type: 'Feature',
                properties: {},
                geometry: {
                    type: 'LineString',
                    coordinates: coords
                }
            }
        });

        map.addLayer({
            id: 'route',
            type: 'line',
            source: 'route',
            layout: {
                'line-join': 'round',
                'line-cap': 'round'
            },
            paint: {
                'line-color': '#314c3b', // Brand Green
                'line-width': 4,
                'line-dasharray': [2, 1]
            }
        });
    }
}

function clearMap() {
    markers.forEach(m => m.remove());
    markers = [];
    if (map && map.getSource('route')) {
        map.getSource('route').setData({
            type: 'Feature',
            properties: {},
            geometry: {
                type: 'LineString',
                coordinates: []
            }
        });
    }
}
