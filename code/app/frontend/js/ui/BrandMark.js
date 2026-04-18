/**
 * BrandMark — conic-gradient disc + compass glyph, 28 px
 */
export function BrandMark() {
  const el = document.createElement('div');
  el.className = 'np-brand-mark';
  el.setAttribute('aria-label', 'NavalPlan');
  el.setAttribute('role', 'img');
  el.style.cssText = [
    'width:28px',
    'height:28px',
    'border-radius:50%',
    'background:conic-gradient(from 135deg, var(--coral) 0deg, var(--amber) 90deg, var(--teal) 180deg, var(--violet) 270deg, var(--coral) 360deg)',
    'display:inline-flex',
    'align-items:center',
    'justify-content:center',
    'flex-shrink:0',
  ].join(';');

  const inner = document.createElement('span');
  inner.setAttribute('aria-hidden', 'true');
  inner.style.cssText = 'font-size:14px;line-height:1;filter:brightness(10)';
  inner.textContent = '✦';
  el.appendChild(inner);
  return el;
}
