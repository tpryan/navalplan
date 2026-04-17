/**
 * IconBtn — 44 px round utility button (bell, help, close)
 * @param {object} opts
 * @param {string} opts.icon      text/emoji/HTML for the icon
 * @param {string} opts.label     aria-label (required)
 * @param {string} [opts.cls]
 * @param {Function} [opts.onClick]
 */
export function IconBtn({ icon, label, cls = '', onClick } = {}) {
  const el = document.createElement('button');
  el.type = 'button';
  el.setAttribute('aria-label', label);
  el.className = ['np-icon-btn', cls].filter(Boolean).join(' ');
  el.innerHTML = icon;
  el.style.cssText = [
    'display:inline-flex',
    'align-items:center',
    'justify-content:center',
    'width:44px',
    'height:44px',
    'border-radius:50%',
    'background:var(--surface)',
    'border:none',
    'cursor:pointer',
    'box-shadow:var(--shadow-soft)',
    'font-size:20px',
    'outline:none',
    'transition:opacity .15s',
  ].join(';');
  el.addEventListener('focus', () => {
    el.style.outline = '3px solid var(--focus)';
    el.style.outlineOffset = '2px';
  });
  el.addEventListener('blur', () => { el.style.outline = 'none'; });
  el.addEventListener('mouseenter', () => { el.style.opacity = '.8'; });
  el.addEventListener('mouseleave', () => { el.style.opacity = '1'; });
  if (onClick) el.addEventListener('click', onClick);
  return el;
}
