/**
 * MapPin — numbered teardrop pin for AdvancedMarkerElement, with a larger
 * shield/crest + flag silhouette for Start (`n: 'S'`) and combined Start/End
 * (`numbers: ['S', 3]` or `numbers: [1, 4]`) nodes.
 * @param {object} opts
 * @param {string} [opts.accent]     token name (coral|teal|amber|violet|sky|green)
 * @param {string[]} [opts.accents]  token names when combining multiple stops
 * @param {number|string} [opts.n]   stop number (single stop)
 * @param {Array<number|string>} [opts.numbers] array of stop numbers when combining stops
 * @param {boolean} [opts.isStartEnd] explicit flag that this node is the trip's Start/End point
 * @param {string} [opts.label]      aria-label text
 * @returns {HTMLElement}            pass as `content` to AdvancedMarkerElement
 */
export function MapPin({ accent = 'coral', accents, n, numbers, isStartEnd = false, label } = {}) {
  const nums = Array.isArray(numbers) && numbers.length > 0
    ? numbers
    : (n != null ? [n] : []);
  const isCombined = nums.length > 1;
  const isStartNode = isStartEnd || isCombined || String(nums[0]) === 'S';
  const primaryAccent = (Array.isArray(accents) && accents[0]) || accent || 'coral';
  const secondaryAccent = (Array.isArray(accents) && accents[nums.length - 1]) || primaryAccent;

  const wrap = document.createElement('div');
  wrap.className = [
    'np-pin',
    isCombined && 'np-pin--combined',
    isStartNode && 'np-pin--start-end',
  ].filter(Boolean).join(' ');
  wrap.style.setProperty('--pin', `var(--${primaryAccent})`);
  if (isCombined) {
    wrap.style.setProperty('--pin-end', `var(--${secondaryAccent})`);
    wrap.setAttribute('data-stop-numbers', nums.join(','));
  }
  wrap.setAttribute('role', 'img');
  wrap.setAttribute('aria-label', label || (isCombined ? `Stops ${nums.join(' & ')}` : `Stop ${nums[0] ?? ''}`));
  wrap.setAttribute('tabindex', '0');

  // Shield / crest silhouette with a bold flag icon for Start & Start/End nodes
  if (isCombined || isStartNode) {
    const badgeText = isCombined ? nums.join('+') : String(nums[0] ?? 'S');
    const fontSize = badgeText.length > 3 ? 10 : 11;
    const rightFill = isCombined ? 'var(--pin-end)' : 'var(--pin)';
    const uid = Math.random().toString(36).slice(2, 8);
    wrap.innerHTML = `
      <svg viewBox="0 0 52 56" width="52" height="56" aria-hidden="true">
        <defs>
          <clipPath id="np-pin-left-${uid}">
            <rect x="0" y="0" width="26" height="56"/>
          </clipPath>
          <clipPath id="np-pin-right-${uid}">
            <rect x="26" y="0" width="26" height="56"/>
          </clipPath>
        </defs>
        <g filter="drop-shadow(0 3px 6px rgba(0,0,0,.35))">
          <path d="M10 2 H42 A8 8 0 0 1 50 10 V27 C50 38 26 54 26 54 C26 54 2 38 2 27 V10 A8 8 0 0 1 10 2 Z"
                fill="var(--pin)" clip-path="url(#np-pin-left-${uid})"/>
          <path d="M10 2 H42 A8 8 0 0 1 50 10 V27 C50 38 26 54 26 54 C26 54 2 38 2 27 V10 A8 8 0 0 1 10 2 Z"
                fill="${rightFill}" clip-path="url(#np-pin-right-${uid})"/>
          <path d="M10 2 H42 A8 8 0 0 1 50 10 V27 C50 38 26 54 26 54 C26 54 2 38 2 27 V10 A8 8 0 0 1 10 2 Z"
                fill="none" stroke="var(--surface, #fff)" stroke-width="2.2"/>
        </g>
        <rect x="6" y="7" width="40" height="22" rx="7" fill="var(--surface, #fff)"/>
        <g transform="translate(8, 11)">
          <path d="M1.5 0.5 V13.5" stroke="var(--pin)" stroke-width="1.8" stroke-linecap="round"/>
          <path d="M2.2 1 H9.5 L7.5 4.5 L9.5 8 H2.2 Z" fill="var(--pin)"/>
        </g>
        <text x="30" y="22" text-anchor="middle" font-size="${fontSize}" font-weight="800"
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
