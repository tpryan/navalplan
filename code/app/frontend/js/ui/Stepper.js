/**
 * Stepper — research task list with check state
 * @param {object} opts
 * @param {Array<{label:string, state:'done'|'active'|'queued'}>} opts.steps
 */
export function Stepper({ steps = [] } = {}) {
  const el = document.createElement('ol');
  el.className = 'np-stepper';
  el.setAttribute('role', 'list');
  el.style.cssText = 'list-style:none;padding:0;margin:0;display:flex;flex-direction:column;gap:10px';

  steps.forEach(({ label, state }) => {
    const li = document.createElement('li');
    li.style.cssText = 'display:flex;align-items:center;gap:10px;font-size:14px;color:var(--ink2)';

    const dot = document.createElement('span');
    dot.setAttribute('aria-hidden', 'true');
    dot.style.cssText = [
      'flex-shrink:0',
      'width:22px',
      'height:22px',
      'border-radius:50%',
      'display:inline-flex',
      'align-items:center',
      'justify-content:center',
      'font-size:12px',
      'font-weight:700',
      state === 'done'
        ? 'background:var(--teal);color:#fff'
        : state === 'active'
          ? 'background:var(--sky);color:#fff'
          : 'background:var(--chip);color:var(--muted)',
    ].join(';');
    dot.textContent = state === 'done' ? '✓' : state === 'active' ? '●' : '○';

    const txt = document.createElement('span');
    txt.textContent = label;
    if (state === 'done') txt.style.color = 'var(--muted)';
    if (state === 'active') txt.style.fontWeight = '600';

    li.appendChild(dot);
    li.appendChild(txt);
    el.appendChild(li);
  });

  return el;
}
