/**
 * DataTile — metric block (wind, tide, temp, distance)
 * @param {object} opts
 * @param {string} opts.label      all-caps label (10 px)
 * @param {string} opts.value      primary value (15–20 px / 800)
 * @param {string} [opts.sub]      secondary line (11 px)
 * @param {string} [opts.emoji]    optional icon above label
 * @param {string} [opts.accent]   token name for tint bg
 * @param {boolean} [opts.detail]  use smaller, non-bold value style
 * @param {boolean} [opts.compact] constrain to 25% of containing row
 */
export function DataTile({ label, value, sub, emoji, icon, accent = 'sky', detail = false, compact = false } = {}) {
  const el = document.createElement('div');
  el.className = ['np-data-tile', compact && 'np-data-tile--compact'].filter(Boolean).join(' ');
  el.style.setProperty('--accent', `var(--${accent})`);

  if (icon) {
    const ico = document.createElement('span');
    ico.className = 'material-symbols-outlined np-data-tile__icon';
    ico.setAttribute('aria-hidden', 'true');
    ico.textContent = icon;
    el.appendChild(ico);
  } else if (emoji) {
    const ico = document.createElement('span');
    ico.className = 'np-data-tile__emoji';
    ico.setAttribute('aria-hidden', 'true');
    ico.textContent = emoji;
    el.appendChild(ico);
  }

  const lbl = document.createElement('span');
  lbl.className = 'np-data-tile__label';
  lbl.textContent = label;
  el.appendChild(lbl);

  const val = document.createElement('span');
  val.className = detail ? 'np-data-tile__value np-data-tile__value--detail' : 'np-data-tile__value';
  val.textContent = value;
  el.appendChild(val);

  if (sub) {
    const s = document.createElement('span');
    s.className = 'np-data-tile__sub';
    s.textContent = sub;
    el.appendChild(s);
  }

  return el;
}
