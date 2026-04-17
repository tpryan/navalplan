/**
 * DataTile — metric block (wind, tide, temp, distance)
 * @param {object} opts
 * @param {string} opts.label      all-caps label (10 px)
 * @param {string} opts.value      primary value (15–20 px / 800)
 * @param {string} [opts.sub]      secondary line (11 px)
 * @param {string} [opts.emoji]    optional icon above label
 * @param {string} [opts.accent]   token name for tint bg
 */
export function DataTile({ label, value, sub, emoji, accent = 'sky' } = {}) {
  const el = document.createElement('div');
  el.className = 'np-data-tile';
  el.style.cssText = [
    'display:flex',
    'flex-direction:column',
    'gap:2px',
    'padding:12px 14px',
    `border-radius:var(--radius-tile)`,
    `background:color-mix(in oklab,var(--${accent}) 10%,var(--surface))`,
    'min-width:80px',
  ].join(';');

  if (emoji) {
    const ico = document.createElement('span');
    ico.setAttribute('aria-hidden', 'true');
    ico.style.cssText = 'font-size:16px;line-height:1;margin-bottom:2px';
    ico.textContent = emoji;
    el.appendChild(ico);
  }

  const lbl = document.createElement('span');
  lbl.style.cssText = 'font-size:10px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)';
  lbl.textContent = label;
  el.appendChild(lbl);

  const val = document.createElement('span');
  val.style.cssText = 'font-size:18px;font-weight:800;color:var(--ink);line-height:1.1';
  val.textContent = value;
  el.appendChild(val);

  if (sub) {
    const s = document.createElement('span');
    s.style.cssText = 'font-size:11px;color:var(--muted)';
    s.textContent = sub;
    el.appendChild(s);
  }

  return el;
}
