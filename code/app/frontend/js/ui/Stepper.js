/**
 * Stepper — research task list with check state
 * @param {object} opts
 * @param {Array<{label:string, state:'done'|'active'|'queued'}>} opts.steps
 */
export function Stepper({ steps = [] } = {}) {
  const el = document.createElement('ol');
  el.className = 'np-stepper';
  el.setAttribute('role', 'list');

  steps.forEach(({ label, state }) => {
    const li = document.createElement('li');
    li.className = 'np-stepper__item';

    const dot = document.createElement('span');
    dot.setAttribute('aria-hidden', 'true');
    dot.className = `np-stepper__dot np-stepper__dot--${state}`;
    dot.textContent = state === 'done' ? '✓' : state === 'active' ? '●' : '○';

    const txt = document.createElement('span');
    txt.className = `np-stepper__text--${state}`;
    txt.textContent = label;

    li.appendChild(dot);
    li.appendChild(txt);
    el.appendChild(li);
  });

  return el;
}
