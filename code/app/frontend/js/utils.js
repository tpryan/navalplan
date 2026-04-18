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
