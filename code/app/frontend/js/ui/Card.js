/**
 * Card — floating white surface on the map
 * @param {object} opts
 * @param {string} [opts.tint]   accent name (coral|teal|amber|violet|sky|green)
 * @param {string} [opts.pad]    CSS padding value (default '20px')
 * @param {string} [opts.cls]    extra class names
 * @param {Node[]} [opts.children]
 */
export function Card({ tint, pad = '20px', cls = '', children = [] } = {}) {
  const el = document.createElement('div');
  el.className = ['np-card', cls].filter(Boolean).join(' ');
  el.style.cssText = [
    `padding:${pad}`,
    `border-radius:var(--radius-card)`,
    `background:var(--surface)`,
    `box-shadow:var(--shadow)`,
    tint ? `background:color-mix(in oklab,var(--${tint}) 10%,var(--surface));box-shadow:inset 0 0 0 2px color-mix(in oklab,var(--${tint}) 30%,transparent),var(--shadow)` : '',
  ].filter(Boolean).join(';');
  children.forEach(c => el.appendChild(c));
  return el;
}
