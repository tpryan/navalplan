import mapboxgl from 'mapbox-gl';
import Chart from 'chart.js/auto';
import { API } from './api.js';
import { exportToGoogleDocs } from './google_export.js';

// Configuration
const MAPBOX_TOKEN = __MAPBOX_TOKEN__; 

// State
let voyages = [];
let currentVoyage = null;
let currentStops = [];
let selectedDate = null;
let map = null;
let markers = [];
let editingVoyageId = null;

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
  const btnExport = document.getElementById('btn-export-voyage');

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
    // Set default dates
    const today = new Date().toISOString().split('T')[0];
    document.getElementById('voyage-start').value = today;
    document.getElementById('voyage-end').value = '';
    document.getElementById('voyage-title').value = '';
    document.getElementById('voyage-location-name').value = '';
    displayCoords.textContent = '';
    inputLat.value = '';
    inputLng.value = '';
  });

  // Close Modal Helper
  const closeModal = () => {
    modalOverlay.classList.add('hidden');
    modalNewVoyage.classList.add('hidden');
    formNewVoyage.reset();
    displayCoords.textContent = '';
    inputLat.value = '';
    inputLng.value = '';
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
    btnUseMapCenter.addEventListener('click', () => {
      if (!map) return;
      const center = map.getCenter();
      inputLat.value = center.lat;
      inputLng.value = center.lng;
      displayCoords.textContent = `Lat: ${center.lat.toFixed(4)}, Lng: ${center.lng.toFixed(4)}`;
    });
  }

  // Handle Form Submit
  formNewVoyage.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(formNewVoyage);
    const voyageData = {
      title: formData.get('title'),
      start_date: formData.get('start_date') + 'T00:00:00Z',
      end_date: formData.get('end_date') + 'T00:00:00Z',
      location_name: formData.get('location_name'),
      latitude: formData.get('latitude') ? parseFloat(formData.get('latitude')) : null,
      longitude: formData.get('longitude') ? parseFloat(formData.get('longitude')) : null
    };

    try {
      if (editingVoyageId) {
        await API.updateVoyage(editingVoyageId, voyageData);
      } else {
        await API.createVoyage(voyageData);
      }
      closeModal();
      loadVoyages(); // Refresh list
    } catch (err) {
      console.error(err);
      alert('Failed to save voyage. Check console.');
    }
  });

  // Back Button
  if (btnBack) {
    btnBack.addEventListener('click', showVoyageList);
  }

  // Export Button
  if (btnExport) {
    btnExport.addEventListener('click', handleShowReport);
  }

  // Report Modal Close Handler
  const modalReport = document.getElementById('modal-report');
  const btnCloseReport = document.getElementById('btn-close-report');
  const btnCopyReport = document.getElementById('btn-copy-report');
  
  const closeReport = () => {
      modalReport.classList.add('hidden');
      if (document.getElementById('modal-new-voyage').classList.contains('hidden')) {
          modalOverlay.classList.add('hidden');
      }
  };
  
  if (btnCloseReport) {
      btnCloseReport.onclick = closeReport;
  }
  
  if (btnCopyReport) {
    btnCopyReport.onclick = () => {
        const content = document.getElementById('report-content');
        const range = document.createRange();
        range.selectNode(content);
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
        document.execCommand('copy');
        window.getSelection().removeAllRanges();
        
        const originalText = btnCopyReport.textContent;
        btnCopyReport.textContent = 'Copied!';
        setTimeout(() => btnCopyReport.textContent = originalText, 2000);
    };
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
      <div class="voyage-info">
        <h3>${voyage.title}</h3>
        <p>${new Date(voyage.start_date).toLocaleDateString()} - ${new Date(voyage.end_date).toLocaleDateString()}</p>
        ${voyage.location_name ? '<p style="font-size:0.8rem; color:#888">📍 ' + voyage.location_name + '</p>' : ''}
      </div>
      <div class="voyage-actions">
        <button class="btn-icon edit" title="Edit">
          <span class="material-symbols-outlined">edit</span>
        </button>
        <button class="btn-icon delete" title="Delete">
          <span class="material-symbols-outlined">delete</span>
        </button>
      </div>
    `;
    
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
      if (confirm(`Are you sure you want to delete "${voyage.title}"?`)) {
        try {
          await API.deleteVoyage(voyage.id);
          loadVoyages();
          if (currentVoyage && currentVoyage.id === voyage.id) {
             showVoyageList(); // Reset view if we deleted the current voyage
          }
        } catch (err) {
          console.error(err);
          alert('Failed to delete voyage');
        }
      }
    });

    listContainer.appendChild(el);
  });
}

function openEditModal(voyage) {
    editingVoyageId = voyage.id;
    const modalOverlay = document.getElementById('modal-overlay');
    const modalNewVoyage = document.getElementById('modal-new-voyage');
    const modalTitle = modalNewVoyage.querySelector('h2');
    const submitBtn = document.querySelector('#form-new-voyage button[type="submit"]');

    modalTitle.textContent = 'Edit Voyage';
    submitBtn.textContent = 'Update Voyage';
    
    document.getElementById('voyage-title').value = voyage.title;
    document.getElementById('voyage-start').value = voyage.start_date.split('T')[0];
    document.getElementById('voyage-end').value = voyage.end_date.split('T')[0];
    document.getElementById('voyage-location-name').value = voyage.location_name || '';
    
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

        if (map) {
            if (currentStops.length > 0) {
                const bounds = new mapboxgl.LngLatBounds();
                currentStops.forEach(stop => bounds.extend([stop.longitude, stop.latitude]));
                if (voyage.latitude != null && voyage.longitude != null) {
                    bounds.extend([voyage.longitude, voyage.latitude]);
                }
                map.fitBounds(bounds, { padding: 50, maxZoom: 12 });
            } else if (voyage.latitude != null && voyage.longitude != null) {
                map.flyTo({ center: [voyage.longitude, voyage.latitude], zoom: 9 });
            }
        }
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
        
        // Day Info
        let html = `
            <div style="flex:1" class="day-info">
                <span class="day-date">${currentDate.toLocaleDateString(undefined, {month:'short', day:'numeric'})}</span>
                <span class="day-location ${stop ? 'set' : ''}">${stop ? stop.location_name : 'No destination'}</span>
            </div>
        `;
        
        // Research Action
        if (stop) {
            html += `
                <div class="day-actions">
                    <button class="btn-icon research" title="Research">
                        <span class="material-symbols-outlined">science</span>
                    </button>
                </div>
            `;
        }
        
        el.innerHTML = html;
        
        // Handlers
        el.querySelector('.day-info').addEventListener('click', () => selectDate(dateStr));
        
        if (stop) {
            const btnResearch = el.querySelector('.research');
            btnResearch.addEventListener('click', (e) => {
                e.stopPropagation();
                handleResearchClick(stop, btnResearch);
            });
        }

        list.appendChild(el);
        
        currentDate.setUTCDate(currentDate.getUTCDate() + 1);
    }
}

async function handleResearchClick(stop, button) {
    // Check if briefing exists first
    button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
    
    try {
        const existing = await API.getBriefing(stop.id);
        if (existing) {
            showBriefing(existing);
            button.innerHTML = '<span class="material-symbols-outlined">description</span>';
            return;
        }

        // Trigger
        await API.triggerResearch(stop.id);
        
        // Poll
        const poll = setInterval(async () => {
            try {
                const b = await API.getBriefing(stop.id);
                if (b) {
                    clearInterval(poll);
                    button.innerHTML = '<span class="material-symbols-outlined">description</span>';
                    showBriefing(b);
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);
        
    } catch (err) {
        console.error(err);
        button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
    }
}

function renderTideChart(canvasId, tideData, targetDateStr) {
    if (!tideData || !tideData.events) return;

    // Target Date Midnight
    const targetDate = new Date(targetDateStr);
    const targetStart = new Date(targetDate).setUTCHours(0,0,0,0);
    const targetEnd = new Date(targetDate).setUTCHours(24,0,0,0);

    // Parse Events
    // Data: { time: "YYYY-MM-DD HH:MM", height_ft: 1.2 }
    const points = [];
    tideData.events.forEach(e => {
        // Try parsing ISO or loose format
        let d = new Date(e.time);
        if (isNaN(d.getTime())) {
            // Fallback for simple "HH:MM" (assume target date)
            const parts = e.time.split(':');
            if (parts.length >= 2) {
                d = new Date(targetDate);
                d.setHours(parseInt(parts[0]), parseInt(parts[1]), 0);
            }
        }
        
        if (!isNaN(d.getTime())) {
            // Calculate relative hour (-24 to +48 range is fine)
            // But for chart logic, simple float hours relative to midnight is best
            const diffMs = d.getTime() - targetStart;
            const floatHours = diffMs / (1000 * 60 * 60);
            points.push({ x: floatHours, y: e.height_ft });
        }
    });

    points.sort((a, b) => a.x - b.x);

    const ctx = document.getElementById(canvasId).getContext('2d');
    
    new Chart(ctx, {
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
                fill: true
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
                            return label;
                        }
                    }
                }
            },
            scales: {
                x: {
                    type: 'linear',
                    min: 0,
                    max: 24,
                    title: { display: true, text: 'Hour (Midnight to Midnight)' },
                    ticks: {
                        stepSize: 3,
                        callback: (v) => {
                            if (v < 0 || v > 24) return '';
                            return `${v}:00`;
                        }
                    }
                },
                y: {
                    title: { display: true, text: 'Feet' }
                }
            }
        }
    });
}

function showBriefing(briefing) {
    const modal = document.getElementById('modal-briefing');
    const content = document.getElementById('briefing-content');
    const btnClose = document.getElementById('btn-close-briefing');
    const btnRedo = document.getElementById('btn-redo-briefing');
    const modalOverlay = document.getElementById('modal-overlay');

    // Weather
    const weather = briefing.weather_summary || {};
    const weatherHtml = `
        <div class="briefing-section">
            <h3>Weather</h3>
            <div class="weather-box">
                <p><strong>Summary:</strong> ${weather.summary || 'N/A'}</p>
                <div class="briefing-grid">
                    <div><strong>Wind:</strong> ${weather.wind_direction || '-'} ${weather.wind_speed_kt || '-'} kt</div>
                    <div><strong>Waves:</strong> ${weather.wave_height_ft || '-'} ft</div>
                </div>
            </div>
        </div>
    `;

    // Tides
    const tides = briefing.tides || {};
    const tideEvents = (tides.events || []).map(e => `<li><strong>${e.time}</strong> ${e.type}: ${e.height_ft} ft</li>`).join('');
    const tidesHtml = `
        <div class="briefing-section">
            <h3>Tides (${tides.station_name || 'Unknown Station'})</h3>
            <div class="tide-box" style="margin-bottom:1rem;">
                <div style="height:200px; width:100%; position:relative;">
                    <canvas id="tideChartModal"></canvas>
                </div>
                <ul style="list-style:none; padding:0; margin:0; font-size:0.9em; color:#666; margin-top:0.5rem;">${tideEvents || '<li>No tide data</li>'}</ul>
            </div>
        </div>
    `;

    // Facilities
    const facilities = briefing.facilities || [];
    const facilHtml = `
        <div class="briefing-section">
            <h3>Facilities</h3>
            <ul class="facility-list">
                ${facilities.map(f => {
                    let detailsHtml = '';
                    if (typeof f.details === 'string') {
                        detailsHtml = `<p>${f.details}</p>`;
                    } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        const rows = Object.entries(f.details)
                            .filter(([_, v]) => v && v !== 'N/A' && v !== '')
                            .map(([k, v]) => `
                                <tr>
                                    <td style="font-weight:bold; padding-right:1rem; text-transform:capitalize;">${k.replace(/_/g, ' ')}</td>
                                    <td>${v}</td>
                                </tr>
                            `).join('');
                        
                        if (rows) {
                            detailsHtml = `<table style="font-size:0.9em; border-collapse:collapse;">${rows}</table>`;
                        }
                    }

                    return `
                        <li class="facility-item">
                            <h4>${f.name} <span style="font-weight:normal; font-size:0.8em">(${f.type})</span></h4>
                            ${detailsHtml}
                        </li>
                    `;
                }).join('') || '<li>No facilities found</li>'}
            </ul>
        </div>
    `;

    content.innerHTML = weatherHtml + tidesHtml + facilHtml;
    
    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoBriefing(briefing, btnRedo);
    }

    // Show Modal
    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');

    // Render Chart (must happen after modal is visible for size calc)
    if (tides.events && tides.events.length > 0) {
        // Find the stop date to anchor the chart
        const stop = currentStops.find(s => s.id === briefing.stop_id);
        const targetDateStr = stop ? stop.target_date : new Date().toISOString(); // Fallback
        
        renderTideChart('tideChartModal', tides, targetDateStr);
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
        <div style="text-align:center; padding:3rem; color: #666;">
            <span class="material-symbols-outlined spin" style="font-size: 3rem; margin-bottom: 1rem;">sync</span>
            <p><strong>Agent is researching...</strong></p>
            <p style="font-size: 0.9em;">Checking weather, tides, and local charts.</p>
        </div>
    `;
    btn.disabled = true;

    try {
        await API.triggerResearch(oldBriefing.stop_id);
        
        const oldTime = new Date(oldBriefing.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 45000; // 45 seconds
        
        // Poll
        const poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                content.innerHTML = '<div style="text-align:center; padding:2rem; color: #d9534f;"><p><strong>Research timed out.</strong></p><p>The agent is taking too long or encountered an error.</p></div>';
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
                        btn.disabled = false;
                        showBriefing(b); // Re-render with new data
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        content.innerHTML = '<div style="text-align:center; padding:2rem; color: red;"><p>Failed to redo research.</p></div>';
        btn.disabled = false;
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

    const {lng, lat} = e.lngLat;
    const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));

    // Get features at click point
    const features = map.queryRenderedFeatures(e.point);
    console.log('Clicked Features:', features);
    
    // Attempt to find a label
    let locationName = `Location ${lat.toFixed(3)}, ${lng.toFixed(3)}`;
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

    async function handleShowReport() {
    if (!currentVoyage) return;
    
    const btn = document.getElementById('btn-export-voyage');
    const originalContent = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

    try {
        // 1. Fetch all data
        // For simplicity, we re-use the currentStops we have, but we need briefings
        // Sort stops
        const sortedStops = [...currentStops].sort((a, b) => 
            new Date(a.target_date) - new Date(b.target_date)
        );
        
        // Fetch Briefings in parallel
        const briefingPromises = sortedStops.map(s => API.getBriefing(s.id).catch(() => null));
        const briefings = await Promise.all(briefingPromises);
        
        // 2. Build HTML
        let html = `
            <h1 style="text-align:center; border-bottom: 2px solid #333; padding-bottom: 0.5rem;">${currentVoyage.title}</h1>
            <p style="text-align:center; font-style:italic;">
                ${new Date(currentVoyage.start_date).toLocaleDateString()} - ${new Date(currentVoyage.end_date).toLocaleDateString()}
            </p>
            <hr />
        `;
        
        sortedStops.forEach((stop, idx) => {
            const b = briefings[idx];
            html += `
                <div style="margin-bottom: 2rem;">
                    <h2 style="background-color: #eee; padding: 0.5rem;">Day ${idx + 1}: ${stop.location_name}</h2>
                    <p><strong>Date:</strong> ${new Date(stop.target_date).toLocaleDateString()}</p>
            `;
            
            if (b) {
                 // Weather
                if (b.weather_summary) {
                    const w = b.weather_summary;
                    html += `
                        <h3>Weather</h3>
                        <p>${w.summary || 'No summary available.'}</p>
                        <ul>
                            <li>Wind: ${w.wind_direction || '-'} ${w.wind_speed_kt || '-'} kt</li>
                            <li>Waves: ${w.wave_height_ft || '-'} ft</li>
                        </ul>
                    `;
                }
                
                // Tides
                if (b.tides && b.tides.events) {
                    const canvasId = `tideChart_${idx}`;
                    html += `<h3>Tides (${b.tides.station_name || 'Station Unknown'})</h3>
                             <div style="height:200px; width:100%; position:relative; margin-bottom:1rem;">
                                <canvas id="${canvasId}"></canvas>
                             </div>
                             <ul>`;
                    b.tides.events.forEach(e => {
                        html += `<li>${e.time} - ${e.type} (${e.height_ft} ft)</li>`;
                    });
                    html += `</ul>`;
                }
                
                // Facilities
                if (b.facilities && b.facilities.length > 0) {
                     html += `<h3>Facilities</h3>`;
                     b.facilities.forEach(f => {
                         html += `<p><strong>${f.name}</strong> (${f.type})</p>`;
                         if (f.details) {
                             html += `<ul style="font-size: 0.9em; color: #555;">`;
                             for (const [k, v] of Object.entries(f.details)) {
                                 html += `<li>${k}: ${v}</li>`;
                             }
                             html += `</ul>`;
                         }
                     });
                }
            } else {
                html += `<p style="color: #888; font-style: italic;">No briefing data generated yet.</p>`;
            }
            
            html += `</div>`;
        });
        
        // 3. Show Modal
        const modal = document.getElementById('modal-report');
        const content = document.getElementById('report-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = html;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        // Render all charts
        sortedStops.forEach((stop, idx) => {
            const b = briefings[idx];
            if (b && b.tides && b.tides.events) {
                renderTideChart(`tideChart_${idx}`, b.tides, stop.target_date);
            }
        });

    } catch (err) {
        console.error(err);
        alert('Failed to generate report.');
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalContent;
    }
}
