/**
 * TopRight — bell + help/avatar pill cluster (fixed top-right)
 * @param {object} opts
 * @param {Node[]} [opts.children]   items to place in the cluster
 */
export function TopRight({ children = [] } = {}) {
  const el = document.createElement('div');
  el.className = 'np-top-right';
  el.style.cssText = [
    'position:fixed',
    'top:20px',
    'right:20px',
    'display:flex',
    'align-items:center',
    'gap:8px',
    'z-index:20',
  ].join(';');
  children.forEach(c => el.appendChild(c));
  return el;
}
