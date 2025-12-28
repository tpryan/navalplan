import mapboxgl from 'mapbox-gl';
// import { DateTime } from 'luxon'; // Ready for use later

// Configuration
// TODO: Replace with env variable logic in production
const MAPBOX_TOKEN = 'pk.eyJ1IjoidHByeWFuIiwiYSI6ImNsaWF1ZmpzbzAyMGUzY3MxbXYwbzJoY2sifQ.y_T2pI-8jbcchYR9V9qB7g'; 

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  initMap();
}

function initMap() {
  if (!MAPBOX_TOKEN) {
    console.error('Mapbox token is missing.');
    return;
  }

  mapboxgl.accessToken = MAPBOX_TOKEN;

  const map = new mapboxgl.Map({
    container: 'map-container',
    style: 'mapbox://styles/tpryan/cmc9ofgil01hi01r70dnx3n7x', 
    center: [-123.0, 48.5], // Salish Sea
    zoom: 8
  });

  map.on('load', () => {
    console.log('NavalPlan: Map Loaded Successfully');
    // Future: Load voyages from API here
  });

  // Add navigation controls (zoom/rotate)
  map.addControl(new mapboxgl.NavigationControl());
}
