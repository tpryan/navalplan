// Design-token utilities and marker-type configuration.
// All functions are pure (read CSS custom properties from document root).

/** Signal accent token name per facility / recommendation type. */
export const MARKER_ACCENTS = {
    anchorage:    'teal',
    mooring:      'violet',
    marina:       'amber',
    hub:          'amber',
    'yacht club': 'amber',
    restaurant:   'green',
    bar:          'coral',
    default:      'sky',
};

/** Material Symbols icon name per facility type. */
export const MARKER_ICONS = {
    anchorage:  'anchor',
    marina:     'directions_boat',
    mooring:    'link',
    restaurant: 'restaurant',
    bar:        'local_bar',
    hub:        'build',
};

/** Return the Signal accent token for a facility/recommendation type string. */
export function markerAccent(type) {
    if (!type) return MARKER_ACCENTS.default;
    const t = type.toLowerCase();
    if (t.includes('anchor'))     return MARKER_ACCENTS.anchorage;
    if (t.includes('moor'))       return MARKER_ACCENTS.mooring;
    if (t.includes('marina'))     return MARKER_ACCENTS.marina;
    if (t.includes('hub'))        return MARKER_ACCENTS.hub;
    if (t.includes('yacht'))      return MARKER_ACCENTS['yacht club'];
    if (t.includes('restaurant')) return MARKER_ACCENTS.restaurant;
    if (t.includes('bar'))        return MARKER_ACCENTS.bar;
    return MARKER_ACCENTS.default;
}

/** Resolve a Signal token name to its current computed hex value (for APIs needing raw colors). */
export function tokenColor(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim();
}

/** Compat shim for call sites still using markerColor(type). */
export function markerColor(type) {
    return tokenColor(markerAccent(type));
}

/** Map a weather description string to a Material Symbol icon name. */
export function getIconForWeather(description, isNight = false) {
    const d = (description || '').toLowerCase();
    if (d.includes('clear'))              return isNight ? 'nightlight' : 'clear_day';
    if (d.includes('partly cloudy'))      return isNight ? 'partly_cloudy_night' : 'partly_cloudy_day';
    if (d.includes('overcast'))           return 'cloud';
    if (d.includes('thunderstorm'))       return 'thunderstorm';
    if (d.includes('snow'))               return 'weather_snowy';
    if (d.includes('shower'))             return 'rainy';
    if (d.includes('freezing rain'))      return 'weather_mix';
    if (d.includes('rain'))               return 'rainy';
    if (d.includes('drizzle'))            return 'weather_mix';
    if (d.includes('fog'))                return 'foggy';
    return 'cloud';
}

/** Convert cardinal direction to degrees for rotation (pointing in wind direction). */
export function directionToDegrees(dir) {
    const directions = ["N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"];
    const index = directions.indexOf(dir);
    if (index === -1) return 0;
    const deg = index * 22.5;
    // Arrow points where it blows (e.g. N blows South)
    return (deg + 180) % 360;
}

/** Get a scale multiplier for wind arrow based on speed in knots. */
export function getWindScale(kt) {
    if (kt < 11) return 0.8;
    if (kt < 22) return 1.0;
    return 1.2;
}

/** Return an SVG string for a long-stemmed, small-headed wind arrow. */
export function getWindArrowSVG(deg, scale = 1, className = '') {
    // Center of 24x24 is 12,12. 
    // We extend the paths slightly outside the box and use overflow:visible 
    // to ensure the arrow looks long and the head is far from the center.
    return `
    <svg viewBox="0 0 24 24" class="${className}" style="transform: rotate(${deg}deg) scale(${scale}); width:100%; height:100%; overflow:visible;">
        <path d="M12 27V-3M10.5 -1L12 -2.5L13.5 -1" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
    `;
}

/**
 * Build a round accent dot with an optional white Material Symbol icon.
 * Used as content for Google Maps AdvancedMarkerElement.
 */
export function accentDot(accent, size = 28, iconKey = '') {
    const dot = document.createElement('div');
    dot.style.cssText = [
        `width:${size}px`,
        `height:${size}px`,
        'border-radius:50%',
        `background:var(--${accent})`,
        'border:2.5px solid var(--surface)',
        'box-shadow:0 2px 6px rgba(0,0,0,.3)',
        'cursor:pointer',
        'flex-shrink:0',
        'display:flex',
        'align-items:center',
        'justify-content:center',
    ].join(';');
    const iconName = MARKER_ICONS[iconKey];
    if (iconName) {
        const span = document.createElement('span');
        span.className = 'material-symbols-outlined';
        span.style.cssText = `font-size:${Math.round(size * 0.52)}px;color:#fff;line-height:1;pointer-events:none;user-select:none`;
        span.setAttribute('aria-hidden', 'true');
        span.textContent = iconName;
        dot.appendChild(span);
    }
    return dot;
}
