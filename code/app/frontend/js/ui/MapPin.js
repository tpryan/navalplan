/**
 * MapPin — numbered teardrop pin for AdvancedMarkerElement.
 * Supports a single stop (`n`, `accent`) or a combined multi-stop pin
 * (`numbers: [1, 4]`, `accents: ['coral', 'amber']`).
 * @param {object} opts
 * @param {string} [opts.accent]     token name (coral|teal|amber|violet|sky|green)
 * @param {string[]} [opts.accents]  token names when combining multiple stops
 * @param {number|string} [opts.n]   stop number (single stop)
 * @param {number[]} [opts.numbers]  array of stop numbers when combining stops
 * @param {string} [opts.label]      aria-label text
 * @returns {HTMLElement}            pass as `content` to AdvancedMarkerElement
 */
export function MapPin({ accent = 'coral', accents, n, numbers, label } = {}) {
  const nums = Array.isArray(numbers) && numbers.length > 0
    ? numbers
    : (n != null ? [n] : []);
  const isCombined = nums.length > 1;
  const primaryAccent = (Array.isArray(accents) && accents[0]) || accent || 'coral';
  const secondaryAccent = (Array.isArray(accents) && accents[nums.length - 1]) || primaryAccent;

  const wrap = document.createElement('div');
  wrap.className = isCombined ? 'np-pin np-pin--combined' : 'np-pin';
  wrap.style.setProperty('--pin', `var(--${primaryAccent})`);
  if (isCombined) {
    wrap.style.setProperty('--pin-end', `var(--${secondaryAccent})`);
    wrap.setAttribute('data-stop-numbers', nums.join(','));
  }
  wrap.setAttribute('role', 'img');
  wrap.setAttribute('aria-label', label || (isCombined ? `Stops ${nums.join(' & ')}` : `Stop ${nums[0] ?? ''}`));
  wrap.setAttribute('tabindex', '0');

  if (isCombined) {
    const badgeText = nums.join('+');
    const fontSize = badgeText.length > 3 ? 10 : 11;
    wrap.innerHTML = `
      <svg viewBox="0 0 40 46" width="40" height="46" aria-hidden="true">
        <defs>
          <clipPath id="np-pin-left">
            <rect x="0" y="0" width="20" height="46"/>
          </clipPath>
          <clipPath id="np-pin-right">
            <rect x="20" y="0" width="20" height="46"/>
          </clipPath>
        </defs>
        <g filter="drop-shadow(0 2px 4px rgba(0,0,0,.28))">
          <path d="M20 0C9.5 0 2 7.5 2 17c0 12 18 29 18 29s18-17 18-29C38 7.5 30.5 0 20 0z"
                fill="var(--pin)" clip-path="url(#np-pin-left)"/>
          <path d="M20 0C9.5 0 2 7.5 2 17c0 12 18 29 18 29s18-17 18-29C38 7.5 30.5 0 20 0z"
                fill="var(--pin-end)" clip-path="url(#np-pin-right)"/>
        </g>
        <rect x="6" y="7" width="28" height="20" rx="10" fill="var(--surface)"/>
        <text x="20" y="21" text-anchor="middle" font-size="${fontSize}" font-weight="800"
              fill="var(--ink, #1e293b)" font-family="var(--font)">${badgeText}</text>
      </svg>`;
    return wrap;
  }

  wrap.innerHTML = `
    <svg viewBox="0 0 32 42" width="32" height="42" aria-hidden="true">
      <path d="M16 0C7.2 0 0 7.2 0 16c0 11 16 26 16 26s16-15 16-26C32 7.2 24.8 0 16 0z"
            fill="var(--pin)" filter="drop-shadow(0 2px 4px rgba(0,0,0,.25))"/>
      <circle cx="16" cy="16" r="9" fill="var(--surface)"/>
      <text x="16" y="20" text-anchor="middle" font-size="12" font-weight="800"
            fill="var(--pin)" font-family="var(--font)">${nums[0] ?? ''}</text>
    </svg>`;
  return wrap;
}
