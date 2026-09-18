// Pure utility helpers — no state, no DOM beyond the a11y announcer.

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

    // 1. Explicit stop_title or leg_title on debrief object
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

    // 2. Match by start_stop_id or voyage_stop_id
    const targetStopId = parsed.start_stop_id != null ? parsed.start_stop_id : parsed.voyage_stop_id;
    if (targetStopId != null) {
        stopIdx = sorted.findIndex(s => String(s.id) === String(targetStopId));
        if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
            stopIdx = sorted.length - 2;
        }
    }

    // 3. Match by track's voyage_stop_id
    if (stopIdx === -1 && parsed.track_id) {
        const tracksList = Array.isArray(tracksContext) && tracksContext.length > 0
            ? tracksContext
            : (typeof currentTracks !== 'undefined' && Array.isArray(currentTracks) ? currentTracks : []);
        const tr = tracksList.find(t => t.id === parsed.track_id);
        if (tr && tr.voyage_stop_id != null) {
            stopIdx = sorted.findIndex(s => String(s.id) === String(tr.voyage_stop_id));
            if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
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
                if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
                    stopIdx = sorted.length - 2;
                }
            }
        }
    }

    // 5. Match by stop location names mentioned in track names
    if (stopIdx === -1) {
        const names = `${parsed.planned_track_name || ''} ${parsed.track_name || ''}`.toLowerCase();
        for (let i = 0; i < sorted.length - 1; i++) {
            const loc = displayLocationName(sorted[i].location_name || sorted[i].name || '').toLowerCase();
            if (loc && names.includes(loc)) {
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
        const startStop = sorted[stopIdx];
        const startName = displayLocationName(startStop.location_name || startStop.name || '');
        if (stopIdx < sorted.length - 1) {
            const nextStop = sorted[stopIdx + 1];
            const nextName = displayLocationName(nextStop.location_name || nextStop.name || '');
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
