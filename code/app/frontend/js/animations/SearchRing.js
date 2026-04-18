import { importLibrary } from '@googlemaps/js-api-loader';

// Factory — cannot extend OverlayView at parse time; defer until Maps is loaded.
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

let _cls = null;
export async function getSearchRingClass() {
    if (_cls) return _cls;
    const { OverlayView } = await importLibrary('maps');
    _cls = createSearchRingOverlay(OverlayView);
    return _cls;
}
