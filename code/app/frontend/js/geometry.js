// Pure geometry helpers — no DOM, no state.

/**
 * Simple string hash (djb2-style). Used for deterministic jitter seeds.
 */
export function hashString(str) {
    let hash = 0;
    if (!str) return hash;
    for (let i = 0; i < str.length; i++) {
        hash = ((hash << 5) - hash) + str.charCodeAt(i);
        hash |= 0;
    }
    return hash;
}

/**
 * Apply Chaikin corner-cutting to an array of [lng, lat] coordinates.
 * Preserves the first/last point for open paths; re-closes closed rings.
 */
export function chaikin(coords) {
    if (!coords || coords.length < 2) return coords;
    const newCoords = [];
    const isClosed = (
        coords[0][0] === coords[coords.length - 1][0] &&
        coords[0][1] === coords[coords.length - 1][1]
    );

    for (let i = 0; i < coords.length - 1; i++) {
        const p0 = coords[i];
        const p1 = coords[i + 1];
        newCoords.push([0.75 * p0[0] + 0.25 * p1[0], 0.75 * p0[1] + 0.25 * p1[1]]);
        newCoords.push([0.25 * p0[0] + 0.75 * p1[0], 0.25 * p0[1] + 0.75 * p1[1]]);
    }

    if (isClosed) {
        newCoords.push([newCoords[0][0], newCoords[0][1]]);
    } else {
        newCoords.unshift(coords[0]);
        newCoords.push(coords[coords.length - 1]);
    }
    return newCoords;
}

/**
 * Smooth a polygon by running Chaikin for `iterations` passes.
 */
export function smoothPolygon(coordinates, iterations = 2) {
    if (!coordinates || coordinates.length < 3) return coordinates;
    let result = coordinates;
    for (let i = 0; i < iterations; i++) result = chaikin(result);
    return result;
}

/**
 * Generate a circular GeoJSON-style polygon from a center + radius in miles,
 * with organic jitter so it renders as a natural blob rather than a perfect circle.
 */
export function getCirclePolygon(center, radiusMiles, numPoints = 24, jitter = 0.3, seed = 0) {
    const R = 3958.8; // Earth radius in miles
    const lat1 = (center.lat * Math.PI) / 180;
    const lon1 = (center.lng * Math.PI) / 180;
    const getJitter = (i) => { const v = Math.sin(seed + i) * 10000; return v - Math.floor(v); };

    const coords = [];
    for (let i = 0; i < numPoints; i++) {
        const brng = (2 * Math.PI * i) / numPoints;
        const d = (radiusMiles * ((1 - jitter) + getJitter(i) * jitter * 2)) / R;
        const lat2 = Math.asin(Math.sin(lat1) * Math.cos(d) + Math.cos(lat1) * Math.sin(d) * Math.cos(brng));
        const lon2 = lon1 + Math.atan2(
            Math.sin(brng) * Math.sin(d) * Math.cos(lat1),
            Math.cos(d) - Math.sin(lat1) * Math.sin(lat2)
        );
        coords.push([(lon2 * 180) / Math.PI, (lat2 * 180) / Math.PI]);
    }
    if (coords.length) coords.push([coords[0][0], coords[0][1]]);
    return coords;
}

/**
 * Haversine distance between two points in Nautical Miles.
 */
export function nmBetween(a, b) {
    if (!a || !b) return null;
    const lat1 = a.latitude !== undefined ? a.latitude : a.lat;
    const lng1 = a.longitude !== undefined ? a.longitude : a.lng;
    const lat2 = b.latitude !== undefined ? b.latitude : b.lat;
    const lng2 = b.longitude !== undefined ? b.longitude : b.lng;

    if (lat1 == null || lng1 == null || lat2 == null || lng2 == null) return null;

    const toRad = d => d * Math.PI / 180;
    const R = 3440.065; // Nautical miles
    const dLat = toRad(lat2 - lat1);
    const dLon = toRad(lng2 - lng1);
    const haversine = Math.sin(dLat / 2) * Math.sin(dLat / 2) +
        Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) *
        Math.sin(dLon / 2) * Math.sin(dLon / 2);
    const c = 2 * Math.atan2(Math.sqrt(haversine), Math.sqrt(1 - haversine));
    return R * c;
}

