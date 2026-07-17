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
