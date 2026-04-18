/**
 * Chip — small labeled pill (category, meta, status)
 * @param {object} opts
 * @param {string} opts.label
 * @param {string} [opts.accent]   token name (coral|teal|amber|violet|sky|green)
 * @param {string} [opts.emoji]    optional leading emoji
 * @param {string} [opts.cls]
 */
export function Chip({ label, accent, emoji, cls = '' } = {}) {
  const el = document.createElement('span');
  el.className = ['np-chip', cls].filter(Boolean).join(' ');
  el.style.cssText = [
    'display:inline-flex',
    'align-items:center',
    'gap:4px',
    'height:28px',
    'padding:0 10px',
    'border-radius:14px',
    'font-size:12px',
    'font-weight:700',
    'letter-spacing:.04em',
    'white-space:nowrap',
    accent
      ? `background:color-mix(in oklab,var(--${accent}) 12%,var(--surface));color:var(--ink)`
      : 'background:var(--chip);color:var(--ink)',
  ].join(';');
  el.textContent = emoji ? `${emoji} ${label}` : label;
  return el;
}
