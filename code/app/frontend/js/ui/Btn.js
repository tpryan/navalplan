/**
 * Btn — pill button, min 44×44 hit target
 * @param {object} opts
 * @param {string} opts.label
 * @param {boolean} [opts.primary]   coral fill + white text
 * @param {boolean} [opts.ghost]     transparent + ink border
 * @param {string} [opts.accent]     override accent for primary (default coral)
 * @param {string} [opts.cls]
 * @param {Function} [opts.onClick]
 */
export function Btn({ label, primary, ghost, accent = 'coral', cls = '', onClick } = {}) {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = ['np-btn', primary ? 'np-btn--primary' : ghost ? 'np-btn--ghost' : 'np-btn--secondary', cls].filter(Boolean).join(' ');
  el.textContent = label;

  const base = [
    'display:inline-flex',
    'align-items:center',
    'justify-content:center',
    'min-height:44px',
    'min-width:44px',
    'padding:0 20px',
    'border-radius:var(--radius-pill)',
    'font-size:15px',
    'font-weight:700',
    'font-family:var(--font)',
    'cursor:pointer',
    'border:none',
    'outline:none',
    'transition:opacity .15s,box-shadow .15s',
    'white-space:nowrap',
  ];

  if (primary) {
    el.style.cssText = [...base,
      `background:var(--${accent})`,
      'color:#fff',
      `box-shadow:0 4px 14px -4px color-mix(in oklab,var(--${accent}) 80%,transparent)`,
    ].join(';');
  } else if (ghost) {
    el.style.cssText = [...base,
      'background:transparent',
      'color:var(--ink)',
      `border:1.5px solid var(--hair)`,
    ].join(';');
  } else {
    el.style.cssText = [...base,
      'background:var(--chip)',
      'color:var(--ink)',
    ].join(';');
  }

  el.addEventListener('mouseenter', () => { el.style.opacity = '.85'; });
  el.addEventListener('mouseleave', () => { el.style.opacity = '1'; });
  el.addEventListener('focus', () => {
    el.style.outline = '3px solid var(--focus)';
    el.style.outlineOffset = '2px';
  });
  el.addEventListener('blur', () => { el.style.outline = 'none'; });
  if (onClick) el.addEventListener('click', onClick);
  return el;
}
