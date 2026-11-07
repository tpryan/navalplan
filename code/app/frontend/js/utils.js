// Pure utility helpers — no state, no DOM beyond the a11y announcer.
import { nmBetween } from './geometry.js';

/**
 * Announce a message to screen readers via the #a11y-announcer live region.
 */
export function announce(msg) {
    const el = document.getElementById('a11y-announcer');
    if (!el) return;
    el.textContent = '';
    requestAnimationFrame(() => { el.textContent = msg; });
}

/**
 * Strips Plus Codes (e.g. "82GQ+6Q ") from location names for cleaner UI display.
 */
export function displayLocationName(name) {
    if (!name) return '';
    return name.replace(/^[A-Z0-9]{4,8}\+[A-Z0-9]{2,3}\s*/i, '').trim();
}

/**
 * Ensures the input is an array, handling the wrapped {"recommendations": [...]} format.
 */
export function ensureRecommendationsArray(data) {
    if (!data) return [];
    if (Array.isArray(data)) return data;
    if (data.recommendations && Array.isArray(data.recommendations)) return data.recommendations;
    return [];
}

/**
 * Plain-text HTML escape — use for data fields that must never render as markup.
 */
export function esc(s) {
    return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * Render a list of reference URLs as numbered links.
 */
export function renderReferences(refs) {
    if (!refs || refs.length === 0) return '';
    return `<div class="ref-link">
        <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i + 1}]</a>`).join('')}
    </div>`;
}

/**
 * True when a voyage is a day trip — dated, with the same start and end
 * calendar date. Derived rather than stored, so it can never drift from
 * the actual dates.
 */
export function isDayTrip(voyage) {
    if (!voyage || !voyage.start_date || !voyage.end_date) return false;
    return voyage.start_date.split('T')[0] === voyage.end_date.split('T')[0];
}

export function getDayStartCoords(day) {
    if (!day) return null;
    const lat = day.start_latitude ?? day.latitude;
    const lng = day.start_longitude ?? day.longitude;
    return (typeof lat === 'number' && typeof lng === 'number' && (lat !== 0 || lng !== 0))
        ? { lat, lng }
        : null;
}

export function getDayEndCoords(day) {
    if (!day) return null;
    const lat = day.end_latitude ?? day.latitude;
    const lng = day.end_longitude ?? day.longitude;
    return (typeof lat === 'number' && typeof lng === 'number' && (lat !== 0 || lng !== 0))
        ? { lat, lng }
        : null;
}

export function isDayStartOnly(day) {
    if (!day) return false;
    const type = day.day_type || day.stop_type || 'landfall';
    if (type !== 'landfall') return false;
    const hasStart = Boolean(day.start_location_name && String(day.start_location_name).trim());
    const hasEnd = Boolean(day.end_location_name && String(day.end_location_name).trim());
    return hasStart && !hasEnd;
}

export function getDayStartName(day) {
    if (!day) return '';
    const raw = day.start_location_name !== undefined ? day.start_location_name : (day.location_name || day.name || '');
    return displayLocationName(raw || '');
}

export function getDayEndName(day) {
    if (!day) return '';
    if (isDayStartOnly(day)) return '';
    const raw = day.end_location_name !== undefined ? day.end_location_name : (day.location_name || day.name || '');
    return displayLocationName(raw || '');
}

export function isSameStartAndEnd(day, thresholdNM = 0.25) {
    if (!day) return false;
    if (isDayStartOnly(day)) return false;
    const type = day.day_type || day.stop_type;
    if (type === 'day_trip' || type === 'lay_day') return true;
    const start = getDayStartCoords(day);
    const end = getDayEndCoords(day);
    if (!start || !end) return false;
    const dist = nmBetween(start, end);
    return dist != null && dist <= thresholdNM;
}

/**
 * Formats a voyage's start/end dates for display, collapsing to a single
 * date when the voyage is a day trip instead of showing a "Jul 16 – Jul 16"
 * style range.
 *
 * @param {string|null} startDateStr - ISO date/datetime string, or null.
 * @param {string|null} endDateStr - ISO date/datetime string, or null.
 * @param {Object} [opts]
 * @param {Intl.DateTimeFormatOptions} [opts.format] - options passed to toLocaleDateString.
 * @param {string} [opts.separator] - separator used between start and end for multi-day ranges.
 */
export function formatVoyageDateRange(startDateStr, endDateStr, opts = {}) {
    const format = opts.format || { timeZone: 'UTC' };
    const separator = opts.separator ?? ' – ';
    if (!startDateStr) return '';
    const sameDay = endDateStr && startDateStr.split('T')[0] === endDateStr.split('T')[0];
    const start = new Date(startDateStr).toLocaleDateString(undefined, { ...format, timeZone: 'UTC' });
    if (!endDateStr || sameDay) return start;
    const end = new Date(endDateStr).toLocaleDateString(undefined, { ...format, timeZone: 'UTC' });
    return `${start}${separator}${end}`;
}

/**
 * Resolves the display title for a debrief based on voyage stops rather than raw track filenames.
 * e.g., "Marina del Rey to Isthmus Cove", or "Cowes to Yarmouth".
 *
 * @param {Object|string} debrief Debrief object or JSON string
 * @param {Array} [stopsContext] Optional array of stops
 * @param {Array} [tracksContext] Optional array of tracks
 * @returns {string} Formatted stop title or empty string if not resolvable
 */
export function getDebriefStopTitle(debrief, stopsContext = null, tracksContext = null) {
    if (!debrief) return '';
    let parsed = debrief;
    if (typeof debrief === 'string') {
        try { parsed = JSON.parse(debrief); } catch (e) { parsed = { summary: debrief }; }
    }
    if (!parsed || typeof parsed !== 'object') return '';

    // 1. Explicit day_title, stop_title, or leg_title on debrief object
    if (parsed.day_title && typeof parsed.day_title === 'string' && parsed.day_title.trim()) {
        return parsed.day_title.trim();
    }
    if (parsed.stop_title && typeof parsed.stop_title === 'string' && parsed.stop_title.trim()) {
        return parsed.stop_title.trim();
    }
    if (parsed.leg_title && typeof parsed.leg_title === 'string' && parsed.leg_title.trim()) {
        return parsed.leg_title.trim();
    }

    const stopsList = Array.isArray(stopsContext) && stopsContext.length > 0
        ? stopsContext
        : (typeof currentStops !== 'undefined' && Array.isArray(currentStops) ? currentStops : []);

    if (!stopsList || stopsList.length === 0) return '';

    // Sort stops chronologically / by order_index
    const sorted = [...stopsList].sort((a, b) => {
        const dateA = a.target_date ? new Date(a.target_date) : 0;
        const dateB = b.target_date ? new Date(b.target_date) : 0;
        if (dateA && dateB && dateA - dateB !== 0) return dateA - dateB;
        return (a.order_index ?? 0) - (b.order_index ?? 0);
    });

    let stopIdx = -1;

    // 2. Match by start_day_id / voyage_day_id (or legacy start_stop_id / voyage_stop_id)
    const targetStopId = parsed.start_day_id ?? parsed.voyage_day_id ?? parsed.start_stop_id ?? parsed.voyage_stop_id;
    if (targetStopId != null) {
        stopIdx = sorted.findIndex(s => String(s.id) === String(targetStopId));
        const matched = sorted[stopIdx];
        if (matched && !matched.start_location_name && stopIdx === sorted.length - 1 && sorted.length >= 2) {
            stopIdx = sorted.length - 2;
        }
    }

    // 3. Match by track's voyage_day_id (or legacy voyage_stop_id)
    if (stopIdx === -1 && parsed.track_id) {
        const tracksList = Array.isArray(tracksContext) && tracksContext.length > 0
            ? tracksContext
            : (typeof currentTracks !== 'undefined' && Array.isArray(currentTracks) ? currentTracks : []);
        const tr = tracksList.find(t => t.id === parsed.track_id);
        const trackDayId = tr ? (tr.voyage_day_id ?? tr.voyage_stop_id) : null;
        if (trackDayId != null) {
            stopIdx = sorted.findIndex(s => String(s.id) === String(trackDayId));
            const matched = sorted[stopIdx];
            if (matched && !matched.start_location_name && stopIdx === sorted.length - 1 && sorted.length >= 2) {
                stopIdx = sorted.length - 2;
            }
        }
    }

    // 4. Match explicit leg numbers (e.g. "Leg 1", "Leg 2")
    if (stopIdx === -1) {
        const names = `${parsed.planned_track_name || ''} ${parsed.track_name || ''}`;
        const legMatch = names.match(/\bLeg\s*#?\s*(\d+)\b/i);
        if (legMatch) {
            const legNum = parseInt(legMatch[1], 10);
            if (legNum >= 1 && legNum <= sorted.length) {
                stopIdx = legNum - 1;
                const matched = sorted[stopIdx];
                if (matched && !matched.start_location_name && stopIdx === sorted.length - 1 && sorted.length >= 2) {
                    stopIdx = sorted.length - 2;
                }
            }
        }
    }

    // 5. Match by stop location names mentioned in track names
    if (stopIdx === -1) {
        const names = `${parsed.planned_track_name || ''} ${parsed.track_name || ''}`.toLowerCase();
        for (let i = 0; i < sorted.length; i++) {
            const startLoc = getDayStartName(sorted[i]).toLowerCase();
            const endLoc = getDayEndName(sorted[i]).toLowerCase();
            if ((startLoc && names.includes(startLoc)) || (endLoc && names.includes(endLoc))) {
                stopIdx = i;
                break;
            }
        }
    }

    // 6. Default to first stop if multiple stops exist
    if (stopIdx === -1) {
        stopIdx = 0;
    }

    if (stopIdx >= 0 && stopIdx < sorted.length) {
        const day = sorted[stopIdx];
        if (day.start_location_name || day.end_location_name) {
            const sName = getDayStartName(day);
            const eName = getDayEndName(day);
            if (sName && eName && sName !== eName && !isSameStartAndEnd(day)) {
                return `${sName} to ${eName}`;
            }
            return eName || sName;
        }
        const startName = getDayEndName(day);
        if (stopIdx < sorted.length - 1) {
            const nextStop = sorted[stopIdx + 1];
            const nextName = getDayEndName(nextStop);
            if (startName && nextName) {
                return `${startName} to ${nextName}`;
            }
        }
        if (startName) {
            return startName;
        }
    }

    return '';
}

/**
 * Parses a duration string (e.g. "05:18:06", "05:18", "03h 48m", "3h 48m", "45m") into minutes.
 * @param {string} str
 * @returns {number|null} Duration in minutes or null
 */
export function parseDurationToMinutes(str) {
    if (!str || typeof str !== 'string') return null;
    const s = str.trim();
    if (!s) return null;

    const colonMatch = s.match(/^(\d+):(\d{2})(?::(\d{2}))?$/);
    if (colonMatch) {
        const hrs = parseInt(colonMatch[1], 10);
        const mins = parseInt(colonMatch[2], 10);
        const secs = colonMatch[3] ? parseInt(colonMatch[3], 10) : 0;
        return hrs * 60 + mins + secs / 60;
    }

    let totalMins = 0;
    let found = false;

    const hrMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:h|hr|hrs|hour|hours)\b/i);
    if (hrMatch) {
        totalMins += parseFloat(hrMatch[1]) * 60;
        found = true;
    }

    const minMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:m|min|mins|minute|minutes)\b/i);
    if (minMatch) {
        totalMins += parseFloat(minMatch[1]);
        found = true;
    }

    const secMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds)\b/i);
    if (secMatch) {
        totalMins += parseFloat(secMatch[1]) / 60;
        found = true;
    }

    return found ? totalMins : null;
}

/**
 * Calculates variance between recorded and planned duration.
 * @param {string} recDur
 * @param {string} planDur
 * @returns {{ pct: number, pctStr: string, diffMins: number, deltaStr: string }|null}
 */
export function calculateTimeVariance(recDur, planDur) {
    const recMins = parseDurationToMinutes(recDur);
    const planMins = parseDurationToMinutes(planDur);
    if (recMins == null || planMins == null || planMins <= 0) {
        return null;
    }

    const diffMins = recMins - planMins;
    const pct = (diffMins / planMins) * 100;
    const pctStr = `${pct > 0 ? '+' : ''}${pct.toFixed(1)}%`;

    const absDiff = Math.abs(diffMins);
    const hrs = Math.floor(absDiff / 60);
    const mins = Math.round(absDiff % 60);
    let deltaBody = '';
    if (hrs > 0 && mins > 0) {
        deltaBody = `${hrs}h ${mins}m`;
    } else if (hrs > 0) {
        deltaBody = `${hrs}h`;
    } else {
        deltaBody = `${mins}m`;
    }
    const deltaStr = `${diffMins >= 0 ? '+' : '-'}${deltaBody}`;

    return { pct, pctStr, diffMins, deltaStr };
}

const NAUTICAL_ACRONYM_DEFS = [
    { acronym: 'VMG', full: 'velocity made good (VMG)', wordRe: /\bVMG\b/, definedRe: /velocity\s+made\s+good\s*\(VMG\)/i },
    { acronym: 'TWA', full: 'true wind angle (TWA)', wordRe: /\bTWA\b/, definedRe: /true\s+wind\s+angle\s*\(TWA\)/i },
    { acronym: 'TWS', full: 'true wind speed (TWS)', wordRe: /\bTWS\b/, definedRe: /true\s+wind\s+speed\s*\(TWS\)/i },
    { acronym: 'AWA', full: 'apparent wind angle (AWA)', wordRe: /\bAWA\b/, definedRe: /apparent\s+wind\s+angle\s*\(AWA\)/i },
    { acronym: 'AWS', full: 'apparent wind speed (AWS)', wordRe: /\bAWS\b/, definedRe: /apparent\s+wind\s+speed\s*\(AWS\)/i },
    { acronym: 'SOG', full: 'speed over ground (SOG)', wordRe: /\bSOG\b/, definedRe: /speed\s+over\s+ground\s*\(SOG\)/i },
    { acronym: 'COG', full: 'course over ground (COG)', wordRe: /\bCOG\b/, definedRe: /course\s+over\s+ground\s*\(COG\)/i },
    { acronym: 'XTE', full: 'cross-track error (XTE)', wordRe: /\bXTE\b/, definedRe: /cross[- ]track\s+error\s*\(XTE\)/i },
    { acronym: 'ETA', full: 'estimated time of arrival (ETA)', wordRe: /\bETA\b/, definedRe: /estimated\s+time\s+of\s+arrival\s*\(ETA\)/i },
    { acronym: 'ETD', full: 'estimated time of departure (ETD)', wordRe: /\bETD\b/, definedRe: /estimated\s+time\s+of\s+departure\s*\(ETD\)/i },
    { acronym: 'LNM', full: 'Local Notice to Mariners (LNM)', wordRe: /\bLNM\b/, definedRe: /local\s+notices?\s+to\s+mariners\s*\(LNM\)/i },
];

/**
 * Expands nautical acronyms (e.g. VMG -> "velocity made good (VMG)") the first time
 * they appear across a shared `seen` Set of strings.
 *
 * @param {string} text
 * @param {Set<string>} [seen]
 * @returns {string}
 */
export function expandAcronymsFirstUse(text, seen = new Set()) {
    if (!text || typeof text !== 'string') return text || '';
    let out = text;
    for (const def of NAUTICAL_ACRONYM_DEFS) {
        if (def.definedRe.test(out)) {
            seen.add(def.acronym);
            continue;
        }
        if (seen.has(def.acronym)) continue;
        if (def.wordRe.test(out)) {
            out = out.replace(def.wordRe, def.full);
            seen.add(def.acronym);
        }
    }
    return out;
}

export const PLANNED_ROUTE_PALETTE = [
    '#00B4D8', // Cyan
    '#A855F7', // Purple
    '#10B981', // Emerald
    '#F59E0B', // Amber
    '#EC4899', // Pink
    '#3B82F6', // Blue
];

export const RECORDED_ROUTE_PALETTE = [
    '#FF6B35', // Orange
    '#E11D48', // Rose
    '#8B5CF6', // Violet
    '#14B8A6', // Teal
    '#D97706', // Amber-Dark
];

function getTrackPeriodKey(track) {
    if (!track || typeof track !== 'object') return 'voyage';
    const dayId = track.voyage_day_id ?? track.voyage_stop_id ?? track.start_day_id;
    if (dayId != null && dayId !== '') {
        return `day:${dayId}`;
    }
    const nameStr = String(track.name || '');
    const legMatch = nameStr.match(/\bLeg\s*#?\s*(\d+)\b/i);
    if (legMatch) {
        return `leg:${legMatch[1]}`;
    }
    if (track.start_time) {
        const dStr = String(track.start_time).slice(0, 10);
        if (/^\d{4}-\d{2}-\d{2}$/.test(dStr)) {
            return `date:${dStr}`;
        }
    }
    return 'voyage';
}

/**
 * Assigns distinct colors to tracks that cover the same time period / leg.
 * Returns a Map from track identifier (or track object reference / index) to hex color.
 *
 * @param {Array<Object>} tracks
 * @returns {Map<any, string>}
 */
export function assignTrackColors(tracks) {
    const colorMap = new Map();
    if (!Array.isArray(tracks) || tracks.length === 0) return colorMap;

    const periodCounters = new Map();
    tracks.forEach((track, idx) => {
        if (!track) return;
        const isPlanned = track.kind === 'planned';
        const palette = isPlanned ? PLANNED_ROUTE_PALETTE : RECORDED_ROUTE_PALETTE;
        const periodKey = `${isPlanned ? 'planned' : 'recorded'}:${getTrackPeriodKey(track)}`;
        const slot = periodCounters.get(periodKey) || 0;
        periodCounters.set(periodKey, slot + 1);
        const color = palette[slot % palette.length];
        if (track.id != null) {
            colorMap.set(String(track.id), color);
        }
        if (track.name) {
            colorMap.set(`name:${String(track.name).trim().toLowerCase()}`, color);
        }
        colorMap.set(track, color);
        colorMap.set(idx, color);
    });

    return colorMap;
}

/**
 * @param {Object|Array|string} debrief
 * @param {Array} [stopsContext]
 * @param {Array} [tracksContext]
 * @returns {string}
 */
export function generateDebriefsHTML(debrief, stopsContext = null, tracksContext = null) {
    if (!debrief || (Array.isArray(debrief) && debrief.length === 0)) {
        return '';
    }

    const debriefs = Array.isArray(debrief) ? debrief : [debrief];
    const resolvedTracks = tracksContext || (typeof currentTracks !== 'undefined' ? currentTracks : null);
    const trackColorMap = assignTrackColors(resolvedTracks || []);
    let html = '';

    debriefs.forEach((item, index) => {
        let parsed = item;
        if (typeof item === 'string') {
            try { parsed = JSON.parse(item); } catch (e) { parsed = { summary: item }; }
        }
        if (!parsed) return;

        const isComparison = parsed.mode === 'comparison' || (Array.isArray(parsed.candidate_tracks) && parsed.candidate_tracks.length >= 2);

        const stopTitle = getDebriefStopTitle(
            parsed,
            stopsContext || (typeof currentStops !== 'undefined' ? currentStops : null),
            resolvedTracks
        );

        if (isComparison) {
            const seenAcronyms = new Set();
            const allRawTexts = [
                parsed.summary,
                parsed.conclusions,
                parsed.tacking_efficiency,
                parsed.weather_impact,
                parsed.tidal_analysis,
                ...(Array.isArray(parsed.candidate_tracks) ? parsed.candidate_tracks.map(c => c?.wind_angle_summary) : []),
                ...(Array.isArray(parsed.observations) ? parsed.observations : []),
            ].filter(Boolean);
            for (const rawText of allRawTexts) {
                for (const def of NAUTICAL_ACRONYM_DEFS) {
                    if (def.definedRe.test(String(rawText))) {
                        seenAcronyms.add(def.acronym);
                    }
                }
            }

            const compTitle = stopTitle
                ? `Route Comparison: ${esc(stopTitle)}`
                : `Route Comparison: ${esc(parsed.day_title || parsed.track_name || 'Planned Routes')}`;

            const recName = parsed.recommended_track_name ? String(parsed.recommended_track_name).trim() : '';
            const rawCandidates = Array.isArray(parsed.candidate_tracks) ? parsed.candidate_tracks : [];
            const extractTrackEndpointsCoords = tr => {
                if (!tr) return null;
                let geo = tr.simplified_geojson || tr.geojson;
                if (!geo) return null;
                try {
                    if (typeof geo === 'string') geo = JSON.parse(geo);
                } catch {
                    return null;
                }
                const coords = geo && geo.type === 'LineString' && Array.isArray(geo.coordinates)
                    ? geo.coordinates
                    : (geo && geo.geometry && Array.isArray(geo.geometry.coordinates) ? geo.geometry.coordinates : null);
                if (!coords || coords.length < 2) return null;
                const s = coords[0];
                const e = coords[coords.length - 1];
                if (!Array.isArray(s) || !Array.isArray(e) || s.length < 2 || e.length < 2) return null;
                return { startLat: Number(s[1]), startLng: Number(s[0]), endLat: Number(e[1]), endLng: Number(e[0]) };
            };
            const haversineNM = (lat1, lon1, lat2, lon2) => {
                const toRad = d => (d * Math.PI) / 180;
                const dLat = toRad(lat2 - lat1);
                const dLon = toRad(lon2 - lon1);
                const a = Math.sin(dLat / 2) ** 2 + Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2;
                return 3440.065 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
            };
            const trackEndpointsById = new Map();
            const trackEndpointsByName = new Map();
            if (Array.isArray(resolvedTracks)) {
                resolvedTracks.forEach(tr => {
                    const ep = extractTrackEndpointsCoords(tr);
                    if (!ep) return;
                    if (tr.id != null) trackEndpointsById.set(String(tr.id), ep);
                    const n = String(tr.name || tr.file_name || '').trim().toLowerCase();
                    if (n) trackEndpointsByName.set(n, ep);
                });
            }
            const getCandEndpoints = c => {
                if (!c) return null;
                const cId = c.track_id || c.id;
                if (cId != null && trackEndpointsById.has(String(cId))) return trackEndpointsById.get(String(cId));
                const cName = String(c.name || c.track_name || '').trim().toLowerCase();
                if (cName && trackEndpointsByName.has(cName)) return trackEndpointsByName.get(cName);
                return null;
            };
            // Determine reference start/end endpoints when candidates have geometry in `tracks`
            let refEndpoints = null;
            const candEndpointList = rawCandidates.map(c => ({ c, ep: getCandEndpoints(c) })).filter(x => x.ep != null);
            if (candEndpointList.length >= 2) {
                for (let i = 0; i < candEndpointList.length; i++) {
                    const epA = candEndpointList[i].ep;
                    let overlapCount = 0;
                    for (let j = 0; j < candEndpointList.length; j++) {
                        const epB = candEndpointList[j].ep;
                        if (haversineNM(epA.startLat, epA.startLng, epB.startLat, epB.startLng) <= 5.0 &&
                            haversineNM(epA.endLat, epA.endLng, epB.endLat, epB.endLng) <= 5.0) {
                            overlapCount++;
                        }
                    }
                    if (overlapCount >= 2) {
                        refEndpoints = epA;
                        break;
                    }
                }
            }
            const seenCandSigs = new Set();
            const candidates = rawCandidates.filter(c => {
                if (!c) return false;
                const rawName = String(c.name || c.track_name || '').trim();
                if (refEndpoints) {
                    const ep = getCandEndpoints(c);
                    if (ep && (
                        haversineNM(refEndpoints.startLat, refEndpoints.startLng, ep.startLat, ep.startLng) > 5.0 ||
                        haversineNM(refEndpoints.endLat, refEndpoints.endLng, ep.endLat, ep.endLng) > 5.0
                    )) {
                        return false;
                    }
                }
                const cName = rawName.toLowerCase();
                const distKey = c.distance_nm != null ? Number(c.distance_nm).toFixed(1) : '';
                const hdgKey = c.dominant_heading_deg != null ? String(Math.round(Number(c.dominant_heading_deg))) : '';
                const sig = `${cName}|${distKey}|${hdgKey}`;
                if (seenCandSigs.has(sig)) return false;
                seenCandSigs.add(sig);
                return true;
            });
            if (candidates.length < 2) {
                return;
            }
            const summaryText = expandAcronymsFirstUse(parsed.summary || 'Route comparison complete.', seenAcronyms);

            let candidatesHtml = '';
            if (candidates.length > 0) {
                let recAssigned = false;
                const candidateRows = candidates.map((c, cIdx) => {
                    if (!c) return '';
                    const cName = c.name || c.track_name || 'Candidate Route';
                    const cId = c.track_id || c.id;
                    const cColor = (cId && trackColorMap.get(String(cId))) ||
                        trackColorMap.get(`name:${String(cName).trim().toLowerCase()}`) ||
                        PLANNED_ROUTE_PALETTE[cIdx % PLANNED_ROUTE_PALETTE.length];
                    const matchesRec = Boolean(
                        c.recommended ||
                        (parsed.recommended_track_id && cId && String(cId) === String(parsed.recommended_track_id)) ||
                        (!parsed.recommended_track_id && recName && String(cName).trim().toLowerCase() === recName.toLowerCase()) ||
                        (recName && String(cName).trim().toLowerCase() === recName.toLowerCase())
                    );
                    const isRec = matchesRec && !recAssigned;
                    if (isRec) {
                        recAssigned = true;
                    }
                    const distStr = c.distance_nm != null ? `${Number(c.distance_nm).toFixed(1)} NM` : '--';
                    const durStr = c.estimated_duration || c.duration_interval || '--';
                    let headingStr = '';
                    if (c.dominant_cardinal && c.dominant_heading_deg != null) {
                        headingStr = `${c.dominant_cardinal} (${Math.round(Number(c.dominant_heading_deg))}°)`;
                    } else if (c.dominant_cardinal) {
                        headingStr = String(c.dominant_cardinal);
                    } else if (c.dominant_heading_deg != null) {
                        headingStr = `${Math.round(Number(c.dominant_heading_deg))}°`;
                    }
                    const windSummary = c.wind_angle_summary
                        ? expandAcronymsFirstUse(String(c.wind_angle_summary), seenAcronyms)
                        : '';
                    const tidalSummary = c.tidal_summary
                        ? expandAcronymsFirstUse(String(c.tidal_summary), seenAcronyms)
                        : '';
                    const profileLines = Array.isArray(c.along_route_profile)
                        ? c.along_route_profile.map(line => expandAcronymsFirstUse(String(line), seenAcronyms)).filter(Boolean)
                        : [];

                    return `
                      <div class="debrief-candidate-row card p-xs border mb-2xs ${isRec ? 'debrief-candidate-recommended' : ''}">
                        <div class="flex justify-between align-center">
                          <div class="flex align-center gap-xs">
                            <span class="track-color-swatch" style="display:inline-block;width:10px;height:10px;border-radius:50%;background:${esc(cColor)};flex-shrink:0;" aria-hidden="true"></span>
                            <strong class="candidate-name">${esc(cName)}</strong>
                            ${isRec ? `<span class="track-badge track-badge-planned candidate-rec-badge">Recommended</span>` : ''}
                          </div>
                          <div class="font-xs text-gray flex gap-sm">
                            <span>Dist: <strong>${esc(distStr)}</strong></span>
                            <span>Est. Time: <strong>${esc(durStr)}</strong></span>
                            ${headingStr ? `<span>Course: <strong>${esc(headingStr)}</strong></span>` : ''}
                          </div>
                        </div>
                        ${windSummary ? `<div class="font-xs text-gray mt-2xs">Wind / Angles: ${esc(windSummary)}</div>` : ''}
                        ${tidalSummary ? `<div class="font-xs text-gray mt-2xs">Tide &amp; Current Along Route: ${esc(tidalSummary)}</div>` : ''}
                        ${profileLines.length > 0 ? `<div class="font-xs text-gray mt-2xs debrief-along-route-profile">${profileLines.map(l => `<div>• ${esc(l)}</div>`).join('')}</div>` : ''}
                      </div>
                    `;
                }).join('');

                candidatesHtml = `
                  <div class="debrief-candidates-list mt-xs mb-sm">
                    ${candidateRows}
                  </div>
                `;
            }

            const conclusionsText = parsed.conclusions ? expandAcronymsFirstUse(String(parsed.conclusions), seenAcronyms) : '';
            const tackingText = parsed.tacking_efficiency ? expandAcronymsFirstUse(String(parsed.tacking_efficiency), seenAcronyms) : '';
            const weatherText = parsed.weather_impact ? expandAcronymsFirstUse(String(parsed.weather_impact), seenAcronyms) : '';
            const tidalText = parsed.tidal_analysis ? expandAcronymsFirstUse(String(parsed.tidal_analysis), seenAcronyms) : '';

            let obsHtml = '';
            if (Array.isArray(parsed.observations) && parsed.observations.length > 0) {
                obsHtml = `
                  <div class="debrief-observations mt-sm">
                    <h5 class="m-0 mb-xs">Tactical Pilot Takeaways:</h5>
                    <ul class="m-0 pl-md">
                      ${parsed.observations.map(o => `<li>${esc(expandAcronymsFirstUse(String(o), seenAcronyms))}</li>`).join('')}
                    </ul>
                  </div>
                `;
            }

            html += `
              <div class="debrief-card debrief-card--comparison ${index > 0 ? 'mt-md' : ''}">
                <div class="flex justify-between align-center debrief-header">
                  <h4 class="m-0 flex align-center gap-xs debrief-title">
                    <span class="material-symbols-outlined text-brand-medium">alt_route</span>
                    <span class="debrief-title-text">${compTitle}</span>
                  </h4>
                  <span class="debrief-date text-gray">${new Date().toLocaleDateString()}</span>
                </div>
                ${recName ? `
                  <div class="debrief-pair-badge">
                    <span class="material-symbols-outlined font-xs">recommend</span>
                    Recommended Plan: <strong>${esc(recName)}</strong>
                  </div>
                ` : ''}
                <p class="debrief-summary mt-xs mb-sm">${esc(summaryText)}</p>
                ${candidatesHtml}
                ${conclusionsText ? `
                  <div class="debrief-conclusions mt-xs mb-xs">
                    <strong>Comparison Conclusions & Recommendation:</strong>
                    <p class="m-0 mt-2xs">${esc(conclusionsText)}</p>
                  </div>
                ` : ''}
                ${tackingText ? `
                  <p class="debrief-section-text tack-eff mt-xs mb-2xs">
                    <strong>Course & Wind Angles:</strong> ${esc(tackingText)}
                  </p>
                ` : ''}
                ${weatherText ? `
                  <p class="debrief-section-text weather-impact mt-2xs mb-xs">
                    <strong>Weather Impact:</strong> ${esc(weatherText)}
                  </p>
                ` : ''}
                ${tidalText ? `
                  <p class="debrief-section-text tidal-analysis mt-2xs mb-xs">
                    <strong>Tidal & Current Analysis:</strong> ${esc(tidalText)}
                  </p>
                ` : ''}
                ${obsHtml}
              </div>
            `;
            return;
        }

        let obsHtml = '';
        if (Array.isArray(parsed.observations) && parsed.observations.length > 0) {
            obsHtml = `
              <div class="debrief-observations mt-sm">
                <h5 class="m-0 mb-xs">Tactical Pilot Observations:</h5>
                <ul class="m-0 pl-md">
                  ${parsed.observations.map(o => `<li>${esc(o)}</li>`).join('')}
                </ul>
              </div>
            `;
        }

        const title = stopTitle
            ? `Passage Debrief: ${esc(stopTitle)}`
            : (parsed.track_name
                ? `Passage Debrief: ${esc(parsed.track_name)}`
                : 'Passage Debrief Analysis');

        // Distance Row: actual planned variance
        const hasRecDist = parsed.recorded_distance_nm != null;
        const hasPlanDist = parsed.planned_distance_nm != null;
        const hasDistRow = hasRecDist || hasPlanDist;
        const recDistStr = hasRecDist ? `${parsed.recorded_distance_nm.toFixed(1)} NM` : '--';
        const planDistStr = hasPlanDist ? `${parsed.planned_distance_nm.toFixed(1)} NM` : '--';
        let distVarStr = '--';
        if (parsed.distance_variance_pct != null) {
            distVarStr = `${parsed.distance_variance_pct > 0 ? '+' : ''}${parsed.distance_variance_pct.toFixed(1)}%`;
        } else if (hasRecDist && hasPlanDist && parsed.planned_distance_nm > 0) {
            const pct = ((parsed.recorded_distance_nm - parsed.planned_distance_nm) / parsed.planned_distance_nm) * 100;
            distVarStr = `${pct > 0 ? '+' : ''}${pct.toFixed(1)}%`;
        } else if (parsed.distance_delta_nm != null) {
            distVarStr = `${parsed.distance_delta_nm > 0 ? '+' : ''}${parsed.distance_delta_nm.toFixed(1)} NM`;
        }

        // Time Row: actual planned variance
        const hasRecDur = Boolean(parsed.recorded_duration);
        const hasPlanDur = Boolean(parsed.planned_duration);
        const hasTimeRow = hasRecDur || hasPlanDur;
        const recDurStr = hasRecDur ? esc(parsed.recorded_duration) : '--';
        const planDurStr = hasPlanDur ? esc(parsed.planned_duration) : '--';
        let timeVarStr = '--';
        let timeVarDelta = '';
        if (parsed.duration_variance_pct != null) {
            timeVarStr = `${parsed.duration_variance_pct > 0 ? '+' : ''}${parsed.duration_variance_pct.toFixed(1)}%`;
            if (parsed.duration_delta) timeVarDelta = parsed.duration_delta;
        } else if (hasRecDur && hasPlanDur) {
            const timeVar = calculateTimeVariance(parsed.recorded_duration, parsed.planned_duration);
            if (timeVar) {
                timeVarStr = timeVar.pctStr;
                timeVarDelta = timeVar.deltaStr;
            }
        }

        // Speed Row: avg speed, max speed
        const avgSpdVal = parsed.average_speed_kts ?? parsed.avg_speed_kts;
        const maxSpdVal = parsed.max_speed_kts;
        const hasSpeedRow = avgSpdVal != null || maxSpdVal != null;

        html += `
          <div class="debrief-card ${index > 0 ? 'mt-md' : ''}">
            <div class="flex justify-between align-center debrief-header">
              <h4 class="m-0 flex align-center gap-xs debrief-title">
                <span class="material-symbols-outlined text-brand-medium">flag</span>
                <span class="debrief-title-text">${title}</span>
              </h4>
              <span class="debrief-date text-gray">${new Date().toLocaleDateString()}</span>
            </div>
            ${parsed.planned_track_name ? `
              <div class="debrief-pair-badge">
                <span class="material-symbols-outlined font-xs">compare_arrows</span>
                Compared against plan: <strong>${esc(parsed.planned_track_name)}</strong>
              </div>
            ` : ''}
            <p class="debrief-summary mt-xs mb-sm">${esc(parsed.summary || 'Debrief complete.')}</p>

            <div class="debrief-stat-groups debrief-stat-grid">
              ${hasDistRow ? `
                <div class="debrief-stat-row">
                  <div class="debrief-stat-item">
                    <div class="val rec-dist">${recDistStr}</div>
                    <div class="lbl">Actual Dist</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val plan-dist">${planDistStr}</div>
                    <div class="lbl">Planned Dist</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val var-pct">${distVarStr}</div>
                    <div class="lbl">Variance</div>
                  </div>
                </div>` : ''}

              ${hasTimeRow ? `
                <div class="debrief-stat-row">
                  <div class="debrief-stat-item">
                    <div class="val rec-dur">${recDurStr}</div>
                    <div class="lbl">Actual Time</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val plan-dur">${planDurStr}</div>
                    <div class="lbl">Planned Time</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val time-var-pct var-pct-time" ${timeVarDelta ? `title="${esc(timeVarDelta)}"` : ''}>${timeVarStr}</div>
                    <div class="lbl">Variance</div>
                  </div>
                </div>` : ''}

              ${hasSpeedRow ? `
                <div class="debrief-stat-row debrief-stat-row--speed">
                  ${avgSpdVal != null ? `
                    <div class="debrief-stat-item">
                      <div class="val avg-spd">${avgSpdVal.toFixed(1)} kt</div>
                      <div class="lbl">Avg Speed</div>
                    </div>` : ''}
                  ${maxSpdVal != null ? `
                    <div class="debrief-stat-item">
                      <div class="val max-spd">${maxSpdVal.toFixed(1)} kt</div>
                      <div class="lbl">Max Speed</div>
                    </div>` : ''}
                </div>` : ''}
            </div>

            ${parsed.conclusions ? `
              <div class="debrief-conclusions mt-xs mb-xs">
                <strong>Debrief Conclusions & Takeaways:</strong>
                <p class="m-0 mt-2xs">${esc(parsed.conclusions)}</p>
              </div>
            ` : ''}

            ${parsed.tacking_efficiency ? `
              <p class="debrief-section-text tack-eff mt-xs mb-2xs">
                <strong>Tacking & Maneuvers:</strong> ${esc(parsed.tacking_efficiency)}
              </p>
            ` : ''}
            ${parsed.weather_impact ? `
              <p class="debrief-section-text weather-impact mt-2xs mb-xs">
                <strong>Weather Impact:</strong> ${esc(parsed.weather_impact)}
              </p>
            ` : ''}
            ${parsed.tidal_analysis ? `
              <p class="debrief-section-text tidal-analysis mt-2xs mb-xs">
                <strong>Tidal & Current Analysis:</strong> ${esc(parsed.tidal_analysis)}
              </p>
            ` : ''}
            ${obsHtml}
          </div>
        `;
    });

    return html;
}

/**
 * Returns true if the current time (or provided timestamp) is within the active
 * live-tracking window for a voyage: [start_date 00:00:00Z - 24h, end_date 23:59:59.999Z + 24h].
 * Undated voyages return false.
 *
 * @param {object|null|undefined} voyage
 * @param {number} [nowMs=Date.now()]
 * @returns {boolean}
 */
export function isWithinTrackingWindow(voyage, nowMs = Date.now()) {
    if (!voyage || !voyage.start_date || !voyage.end_date) return false;
    const startDay = String(voyage.start_date).split('T')[0];
    const endDay = String(voyage.end_date).split('T')[0];
    const startMs = Date.parse(`${startDay}T00:00:00Z`);
    const endMs = Date.parse(`${endDay}T23:59:59.999Z`);
    if (Number.isNaN(startMs) || Number.isNaN(endMs)) return false;
    const GRACE_MS = 24 * 60 * 60 * 1000;
    return nowMs >= (startMs - GRACE_MS) && nowMs <= (endMs + GRACE_MS);
}

/**
 * Classifies a stop facility type into report category tokens.
 * - Anchorage, Mooring, Fuel Station, Boat Launch -> 'all navigation'
 * - Marina, Yacht Club -> 'all navigation amenities'
 * - Restaurant, Bar, Cafe, Store, Pharmacy, Services (and others) -> 'all amenities'
 *
 * @param {string} type
 * @returns {string}
 */
export function classifyFacilityReportCats(type) {
    const t = String(type || '').toLowerCase().trim();
    if (t.includes('anchor') || t.includes('moor') || t.includes('fuel') || t.includes('launch') || t.includes('ramp')) {
        return 'all navigation';
    }
    if (t.includes('marina') || t.includes('yacht club') || t.includes('yacht')) {
        return 'all navigation amenities';
    }
    return 'all amenities';
}

/**
 * Formats a concise link label for facility URLs ([Google Maps] for Google Maps URLs, [Website] otherwise).
 *
 * @param {string} url
 * @returns {string}
 */
export function formatFacilityLinkLabel(url) {
    const u = String(url || '').toLowerCase();
    if (u.includes('maps.google.') || u.includes('google.com/maps') || u.includes('goo.gl/maps') || u.includes('maps.app.goo.gl')) {
        return '[Google Maps]';
    }
    return '[Website]';
}

/**
 * Builds Google Static Maps `&path=...` parameters for planned and recorded GPX tracks.
 * Downsamples coordinates per track to keep the static map URL within length limits.
 *
 * @param {Array} tracks
 * @param {number} [maxPointsPerTrack=50]
 * @returns {string}
 */
export function buildStaticMapTrackPathsParam(tracks, maxPointsPerTrack = 50) {
    if (!Array.isArray(tracks) || tracks.length === 0) return '';
    let param = '';
    const colorMap = assignTrackColors(tracks);

    tracks.forEach((track, idx) => {
        if (!track) return;
        const rawGeo = track.simplified_geojson || track.geojson;
        if (!rawGeo) return;

        let geoData = rawGeo;
        if (typeof rawGeo === 'string') {
            try { geoData = JSON.parse(rawGeo); } catch (_) { return; }
        }

        const coords = geoData?.geometry?.coordinates || geoData?.coordinates;
        if (!Array.isArray(coords) || coords.length < 2) return;

        // Downsample while preserving start and end points
        let sampled = coords;
        if (coords.length > maxPointsPerTrack && maxPointsPerTrack >= 2) {
            sampled = [];
            const step = (coords.length - 1) / (maxPointsPerTrack - 1);
            for (let i = 0; i < maxPointsPerTrack - 1; i++) {
                sampled.push(coords[Math.round(i * step)]);
            }
            sampled.push(coords[coords.length - 1]);
        }

        // GeoJSON is [lng, lat]; Static Maps path expects lat,lng
        const pointsStr = sampled
            .filter(pt => Array.isArray(pt) && pt.length >= 2 && Number.isFinite(pt[0]) && Number.isFinite(pt[1]))
            .map(pt => `${Number(pt[1]).toFixed(5)},${Number(pt[0]).toFixed(5)}`)
            .join('|');

        if (!pointsStr || !pointsStr.includes('|')) return;

        const isPlanned = track.kind === 'planned';
        const hexColor = (colorMap.get(track) || colorMap.get(idx) || (isPlanned ? '#00B4D8' : '#FF6B35')).replace(/^#/, '');
        const alpha = isPlanned ? 'd9' : 'e6';
        const weight = isPlanned ? 3 : 4;
        const style = `color:0x${hexColor}${alpha}|weight:${weight}`;
        param += `&path=${style}|${pointsStr}`;
    });

    return param;
}

/**
 * Classifies a Safety Lookout alert into report category tokens.
 * - navigation -> 'all navigation'
 * - tides      -> 'all navigation weather'
 * - weather/sun -> 'all weather'
 *
 * @param {object} alert
 * @returns {string}
 */
export function classifyAlertReportCat(alert) {
    if (!alert || typeof alert !== 'object') return 'all navigation weather';
    const cat = String(alert.category || alert.type || '').toLowerCase().trim();
    if (cat === 'navigation' || cat === 'nav') return 'all navigation';
    if (cat === 'tides' || cat === 'tide') return 'all navigation weather';
    if (cat === 'weather' || cat === 'sun' || cat === 'wind' || cat === 'waves') return 'all weather';
    if (Array.isArray(alert.travel_table) && alert.travel_table.length > 0) return 'all navigation';
    const hint = `${alert.icon || ''} ${alert.message || ''}`.toLowerCase();
    if (hint.includes('tide') || hint.includes('current') || hint.includes('slack')) return 'all navigation weather';
    if (hint.includes('wind') || hint.includes('wave') || hint.includes('storm') || hint.includes('gale') || hint.includes('sun') || hint.includes('twilight') || hint.includes('rain') || hint.includes('squall')) {
        return 'all weather';
    }
    if (hint.includes('anchor') || hint.includes('channel') || hint.includes('shoal') || hint.includes('reef') || hint.includes('depart') || hint.includes('approach')) {
        return 'all navigation';
    }
    return 'all navigation weather';
}

/**
 * Parses report tab and day filter state from a Location or URL-like object.
 * Preserves backward compatibility for /report/sailing and ?sailing=1 by mapping to tab=navigation.
 *
 * @param {Location|{pathname?: string, search?: string}|string} loc
 * @param {boolean} [legacySailing=false]
 * @returns {{ tab: string, day: string }}
 */
export function parseReportFilterFromLocation(loc, legacySailing = false) {
    let pathname = '';
    let search = '';
    if (typeof loc === 'string') {
        const qIdx = loc.indexOf('?');
        pathname = qIdx >= 0 ? loc.slice(0, qIdx) : loc;
        search = qIdx >= 0 ? loc.slice(qIdx) : '';
    } else if (loc && typeof loc === 'object') {
        pathname = String(loc.pathname || '');
        search = String(loc.search || '');
    }

    const params = new URLSearchParams(search);
    const rawTab = (params.get('tab') || '').toLowerCase().trim();
    const rawDay = (params.get('day') || '').toLowerCase().trim();
    const isSailingPath = legacySailing || pathname.endsWith('/sailing') || params.get('sailing') === '1';

    let tab = 'all';
    if (['all', 'guide', 'navigation', 'weather', 'amenities'].includes(rawTab)) {
        tab = rawTab;
    } else if (rawTab === 'destination-guide' || rawTab === 'destination') {
        tab = 'guide';
    } else if (rawTab === 'sailing' || rawTab === 'nav') {
        tab = 'navigation';
    } else if (rawTab === 'activities' || rawTab === 'shore') {
        tab = 'amenities';
    } else if (isSailingPath) {
        tab = 'navigation';
    }

    let day = 'all';
    if (rawDay && rawDay !== 'all') {
        const parsedDay = parseInt(rawDay, 10);
        if (!Number.isNaN(parsedDay) && parsedDay >= 1) {
            day = String(parsedDay);
        }
    }

    return { tab, day };
}

/**
 * Builds query string for report filter state (?tab=...&day=...).
 * Returns empty string when both tab and day are 'all'.
 *
 * @param {string} tab
 * @param {string|number} day
 * @returns {string}
 */
export function buildReportFilterSearch(tab = 'all', day = 'all') {
    const params = new URLSearchParams();
    if (tab && tab !== 'all') params.set('tab', String(tab));
    if (day && String(day) !== 'all') params.set('day', String(day));
    const qs = params.toString();
    return qs ? `?${qs}` : '';
}

/**
 * Applies category tab and day filter state to a report root container element.
 *
 * @param {Element} root
 * @param {{ tab?: string, day?: string|number }} filter
 * @returns {{ tab: string, day: string }}
 */
export function applyReportFilterToDOM(root, filter = {}) {
    if (!root) return { tab: 'all', day: 'all' };
    const currentTab = root.getAttribute('data-report-tab') || 'all';
    const currentDay = root.getAttribute('data-report-day') || 'all';

    let nextTab = filter.tab != null ? String(filter.tab).toLowerCase().trim() : currentTab;
    if (nextTab === 'destination-guide' || nextTab === 'destination') nextTab = 'guide';
    if (nextTab === 'sailing') nextTab = 'navigation';
    if (nextTab === 'activities') nextTab = 'amenities';
    if (!['all', 'guide', 'navigation', 'weather', 'amenities'].includes(nextTab)) {
        nextTab = 'all';
    }

    let nextDay = filter.day != null ? String(filter.day).toLowerCase().trim() : currentDay;
    if (nextDay !== 'all') {
        const n = parseInt(nextDay, 10);
        nextDay = (!Number.isNaN(n) && n >= 1) ? String(n) : 'all';
    }

    root.setAttribute('data-report-tab', nextTab);
    root.setAttribute('data-report-day', nextDay);
    root.classList.remove('np-report--sailing-only');

    // Update category tab buttons
    root.querySelectorAll('#report-category-tabs [data-tab], .np-report-tabs [data-tab]').forEach(btn => {
        const isMatch = btn.getAttribute('data-tab') === nextTab;
        btn.classList.toggle('active', isMatch);
        btn.setAttribute('aria-selected', isMatch ? 'true' : 'false');
    });

    // Update day pill buttons
    root.querySelectorAll('#report-day-pills [data-day], .np-report-day-pills [data-day]').forEach(pill => {
        const isMatch = pill.getAttribute('data-day') === nextDay;
        pill.classList.toggle('active', isMatch);
        pill.setAttribute('aria-selected', isMatch ? 'true' : 'false');
    });

    // Sync mobile dropdown selects if present
    const catSelect = root.querySelector('#report-category-select, .np-report-category-select');
    if (catSelect && catSelect.value !== nextTab) {
        catSelect.value = nextTab;
    }
    const daySelect = root.querySelector('#report-day-select, .np-report-day-select');
    if (daySelect && daySelect.value !== nextDay) {
        daySelect.value = nextDay;
    }

    // Update stop day visibility
    root.querySelectorAll('.np-report-stop[data-stop-day]').forEach(stopEl => {
        const stopDay = stopEl.getAttribute('data-stop-day');
        const hidden = nextDay !== 'all' && stopDay !== nextDay;
        stopEl.classList.toggle('np-report-stop--hidden-day', hidden);
    });

    // Hide facilities wrapper if zero child facility cards match the active category tab
    root.querySelectorAll('.np-facilities-wrapper').forEach(wrapper => {
        const cards = Array.from(wrapper.querySelectorAll('.np-facility-report-card'));
        const matchingCount = cards.filter(card => {
            if (nextTab === 'all') return true;
            const cats = (card.getAttribute('data-report-cats') || '').split(/\s+/);
            return cats.includes(nextTab);
        }).length;
        wrapper.classList.toggle('np-facilities-wrapper--empty', cards.length > 0 && matchingCount === 0);
    });

    return { tab: nextTab, day: nextDay };
}

/**
 * groupColocatedStops — groups landfall stops that sit at the same harbor
 * (within `thresholdNM` nautical miles of an earlier landfall stop) so they
 * share a single combined map pin and a single combined facility count badge.
 *
 * @param {Array<Object>} sortedStops - Stops sorted chronologically
 * @param {Array<string>} [accents]   - Palette token names (e.g. VOYAGE_ACCENTS)
 * @param {number} [thresholdNM=2.0]  - Max distance in NM to merge stops into one group
 * @returns {{ groups: Array<Object>, stopPositionMap: Map<any, {lat: number, lng: number}> }}
 */
export function groupColocatedStops(sortedStops, accents = ['coral', 'teal', 'violet', 'amber', 'sky', 'green'], thresholdNM = 2.0) {
    const groups = [];
    const stopPositionMap = new Map();
    if (!Array.isArray(sortedStops) || sortedStops.length === 0) {
        return { groups, stopPositionMap };
    }

    let landfallIndex = 0;
    for (const stop of sortedStops) {
        if (!stop) continue;
        const dayType = stop.day_type || stop.stop_type;
        if (dayType === 'passage_point') continue;

        const stopNumber = ++landfallIndex;
        if (isDayStartOnly(stop)) continue;
        const endCoords = getDayEndCoords(stop);
        if (!endCoords) continue;
        const accent = accents[(stopNumber - 1) % accents.length] || 'coral';

        let matchedGroup = null;
        for (const g of groups) {
            const dNM = nmBetween(g.position, endCoords);
            if (dNM != null && dNM <= thresholdNM) {
                matchedGroup = g;
                break;
            }
        }

        if (matchedGroup) {
            matchedGroup.items.push({ stop, stopNumber, accent });
            if (stop.id != null) {
                stopPositionMap.set(stop.id, matchedGroup.position);
            }
        } else {
            const position = { lat: endCoords.lat, lng: endCoords.lng };
            const newGroup = {
                position,
                primaryStop: stop,
                items: [{ stop, stopNumber, accent }],
            };
            groups.push(newGroup);
            if (stop.id != null) {
                stopPositionMap.set(stop.id, position);
            }
        }
    }

    const firstDay = sortedStops[0];
    const firstStart = firstDay ? getDayStartCoords(firstDay) : null;
    if (firstStart && !isSameStartAndEnd(firstDay)) {
        for (const g of groups) {
            const dNM = nmBetween(g.position, firstStart);
            if (dNM != null && dNM <= thresholdNM) {
                g.includesTripStart = true;
                g.startStop = firstDay;
                g.position = { lat: firstStart.lat, lng: firstStart.lng };
                for (const it of g.items) {
                    if (it.stop?.id != null) {
                        stopPositionMap.set(it.stop.id, g.position);
                    }
                }
                break;
            }
        }
    }

    return { groups, stopPositionMap };
}

export function findNearbyJoinTargetStop(stops, clickedLat, clickedLng, selectedDate, thresholdNM = 2.0) {
    if (!Array.isArray(stops) || stops.length === 0 || typeof clickedLat !== 'number' || typeof clickedLng !== 'number') {
        return null;
    }
    const sorted = [...stops].sort((a, b) => new Date(a.target_date) - new Date(b.target_date));
    const candidates = [];
    let landfallIndex = 0;

    for (const s of sorted) {
        if (!s) continue;
        const dayType = s.day_type || s.stop_type;
        if (dayType === 'passage_point') continue;
        const endCoords = getDayEndCoords(s);
        if (!endCoords) continue;
        const stopNumber = ++landfallIndex;
        const stopDate = String(s.target_date || '').split('T')[0];
        if (selectedDate && stopDate === selectedDate) continue;

        const startCoords = stopNumber === 1 ? getDayStartCoords(s) : null;
        const dEndNM = nmBetween(endCoords, { lat: clickedLat, lng: clickedLng });
        const dStartNM = startCoords ? nmBetween(startCoords, { lat: clickedLat, lng: clickedLng }) : null;
        const useStartPoint = dStartNM != null && dStartNM <= thresholdNM && (dEndNM == null || dStartNM <= dEndNM);
        const dNM = useStartPoint ? dStartNM : dEndNM;

        if (dNM != null && dNM <= thresholdNM) {
            candidates.push({
                targetStop: s,
                targetLat: useStartPoint ? startCoords.lat : endCoords.lat,
                targetLng: useStartPoint ? startCoords.lng : endCoords.lng,
                targetLocationName: useStartPoint ? (s.start_location_name || s.end_location_name || s.location_name) : (s.end_location_name || s.location_name),
                targetPreciseLocation: useStartPoint ? (s.start_precise_location || s.end_precise_location || s.precise_location) : (s.end_precise_location || s.precise_location),
                stopNumber,
                isStartStop: stopNumber === 1,
                distanceNM: dNM,
            });
        }
    }

    if (candidates.length === 0) return null;
    const startMatch = candidates.find(c => c.isStartStop);
    if (startMatch) return startMatch;
    candidates.sort((a, b) => a.distanceNM - b.distanceNM);
    return candidates[0];
}



