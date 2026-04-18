/**
 * Rail — left sidebar container
 * @param {object} opts
 * @param {number} [opts.width]    pixel width (default 340)
 * @param {Node[]} [opts.children]
 */
export function Rail({ width = 340, children = [] } = {}) {
  const el = document.createElement('aside');
  el.className = 'np-rail';
  el.style.cssText = [
    `width:${width}px`,
    'position:fixed',
    'top:20px',
    'left:20px',
    'bottom:20px',
    'border-radius:var(--radius-card)',
    'background:var(--surface)',
    'box-shadow:var(--shadow)',
    'display:flex',
    'flex-direction:column',
    'gap:14px',
    'padding:20px',
    'overflow-y:auto',
    'z-index:10',
  ].join(';');
  children.forEach(c => el.appendChild(c));
  return el;
}
