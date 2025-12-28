import mapboxgl from 'mapbox-gl';
import { API } from './api.js';

// Configuration
const MAPBOX_TOKEN = __MAPBOX_TOKEN__; 

// State
let voyages = [];

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

  // Open Modal
  btnNewVoyage.addEventListener('click', () => {
    modalOverlay.classList.remove('hidden');
    modalNewVoyage.classList.remove('hidden');
    // Set default dates (Next Sat to +1 week)
    // Simple default: Today and Tomorrow
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

  // Handle Form Submit
  formNewVoyage.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(formNewVoyage);
    const voyageData = {
      title: formData.get('title'),
      start_date: formData.get('start_date'),
      end_date: formData.get('end_date')
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

  if (voyages.length === 0) {
    listContainer.innerHTML = '<p class="loading-text">No voyages yet. Plan your first trip!</p>';
    return;
  }

  voyages.forEach(voyage => {
    const el = document.createElement('div');
    el.className = 'voyage-item';
    el.innerHTML = `
      <h3>${voyage.title}</h3>
      <p>${voyage.start_date} - ${voyage.end_date}</p>
    `;
    el.addEventListener('click', () => selectVoyage(voyage));
    listContainer.appendChild(el);
  });
}

function selectVoyage(voyage) {
  console.log('Selected Voyage:', voyage);
  // TODO: Load Itinerary View
  alert(`Selected: ${voyage.title}`);
}


function initMap() {
  if (!MAPBOX_TOKEN) {
    console.error('Mapbox token is missing. Please set NAVALPLAN_MB_TOKEN environment variable during build.');
    // Optionally alert the user or show a UI message
    return;
  }

  mapboxgl.accessToken = MAPBOX_TOKEN;

  const map = new mapboxgl.Map({
    container: 'map-container',
    style: __MAPBOX_STYLE__, 
    center: [-123.0, 48.5], // Salish Sea
    zoom: 8
  });

  map.on('load', () => {
    console.log('NavalPlan: Map Loaded Successfully');
  });

  // Add navigation controls (zoom/rotate)
  map.addControl(new mapboxgl.NavigationControl());
}