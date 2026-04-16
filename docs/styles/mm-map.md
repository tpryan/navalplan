# Refined Google Maps Styling Guide: High-Contrast Midnight Mariner

This JSON configuration is meticulously tuned to match the "Variant 1" concept, featuring deep navy-to-black water and high-contrast, silvery-white land masses.

## Styling Philosophy
*   **Water:** Deep midnight navy for maximum depth.
*   **Land:** High-visibility "Silvery Moon" gray to make coastlines and islands pop sharply.
*   **UI Harmony:** Designed to sit behind glassmorphic overlays without creating visual mud.

## JSON Styling Object

```json
[
  {
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#0d1117"
      }
    ]
  },
  {
    "elementType": "labels.icon",
    "stylers": [
      {
        "visibility": "off"
      }
    ]
  },
  {
    "elementType": "labels.text.fill",
    "stylers": [
      {
        "color": "#747d88"
      }
    ]
  },
  {
    "elementType": "labels.text.stroke",
    "stylers": [
      {
        "color": "#0d1117"
      }
    ]
  },
  {
    "featureType": "administrative",
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#e6edf3"
      }
    ]
  },
  {
    "featureType": "landscape.man_made",
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#e6edf3"
      }
    ]
  },
  {
    "featureType": "landscape.natural",
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#d0d7de"
      }
    ]
  },
  {
    "featureType": "poi",
    "stylers": [
      {
        "visibility": "off"
      }
    ]
  },
  {
    "featureType": "road",
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#1b2129"
      }
    ]
  },
  {
    "featureType": "road",
    "elementType": "labels.text.fill",
    "stylers": [
      {
        "color": "#8b949e"
      }
    ]
  },
  {
    "featureType": "water",
    "elementType": "geometry",
    "stylers": [
      {
        "color": "#010409"
      }
    ]
  }
]
```

## Implementation Note
Use this JSON in your Google Cloud Console or directly in your `google.maps.MapOptions`. This version specifically increases the land luminosity to match the "silvery" look of the design concept.