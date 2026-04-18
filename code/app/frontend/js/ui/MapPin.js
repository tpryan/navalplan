/**
 * MapPin — numbered teardrop pin for AdvancedMarkerElement
 * @param {object} opts
 * @param {string} opts.accent   token name (coral|teal|amber|violet|sky|green)
 * @param {number} opts.n        stop number
 * @param {string} opts.label    aria-label text
 * @returns {HTMLElement}        pass as `content` to AdvancedMarkerElement
 */
export function MapPin({ accent = 'coral', n, label } = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'np-pin';
  wrap.style.setProperty('--pin', `var(--${accent})`);
  wrap.setAttribute('role', 'img');
  wrap.setAttribute('aria-label', label || `Stop ${n}`);
  wrap.setAttribute('tabindex', '0');
  wrap.innerHTML = `
    <svg viewBox="0 0 32 42" width="32" height="42" aria-hidden="true">
      <path d="M16 0C7.2 0 0 7.2 0 16c0 11 16 26 16 26s16-15 16-26C32 7.2 24.8 0 16 0z"
            fill="var(--pin)" filter="drop-shadow(0 2px 4px rgba(0,0,0,.25))"/>
      <circle cx="16" cy="16" r="9" fill="var(--surface)"/>
      <text x="16" y="20" text-anchor="middle" font-size="12" font-weight="800"
            fill="var(--pin)" font-family="var(--font)">${n}</text>
    </svg>`;
  return wrap;
}
