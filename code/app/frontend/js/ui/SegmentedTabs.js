/**
 * SegmentedTabs — pill-contained tab switcher
 * @param {object} opts
 * @param {Array<{id:string, label:string}>} opts.tabs
 * @param {string} opts.active     id of the active tab
 * @param {Function} opts.onChange called with tab id on select
 */
export function SegmentedTabs({ tabs = [], active, onChange } = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'np-seg-tabs';
  wrap.setAttribute('role', 'tablist');
  wrap.style.cssText = [
    'display:inline-flex',
    'align-items:center',
    'background:var(--chip)',
    'border-radius:var(--radius-pill)',
    'padding:4px',
    'gap:2px',
  ].join(';');

  const buttons = [];

  const setActive = (id) => {
    buttons.forEach(({ btn, tabId }) => {
      const on = tabId === id;
      btn.setAttribute('aria-selected', on);
      btn.setAttribute('tabindex', on ? '0' : '-1');
      btn.style.cssText = [
        'height:34px',
        'min-width:44px',
        'padding:0 14px',
        'border-radius:var(--radius-pill)',
        'border:none',
        'cursor:pointer',
        'font-size:14px',
        'font-weight:600',
        'font-family:var(--font)',
        'outline:none',
        on ? 'background:var(--ink);color:var(--surface)' : 'background:transparent;color:var(--muted)',
      ].join(';');
    });
  };

  tabs.forEach(({ id, label }) => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.setAttribute('role', 'tab');
    btn.textContent = label;
    btn.addEventListener('focus', () => { btn.style.outline = '3px solid var(--focus)'; btn.style.outlineOffset = '2px'; });
    btn.addEventListener('blur', () => { btn.style.outline = 'none'; });
    btn.addEventListener('click', () => {
      setActive(id);
      onChange?.(id);
    });
    btn.addEventListener('keydown', (e) => {
      const idx = buttons.findIndex(b => b.tabId === id);
      let next = -1;
      if (e.key === 'ArrowRight') next = (idx + 1) % buttons.length;
      else if (e.key === 'ArrowLeft') next = (idx - 1 + buttons.length) % buttons.length;
      else if (e.key === 'Home') next = 0;
      else if (e.key === 'End') next = buttons.length - 1;
      if (next !== -1) {
        e.preventDefault();
        const { btn: nextBtn, tabId: nextId } = buttons[next];
        setActive(nextId);
        onChange?.(nextId);
        nextBtn.focus();
      }
    });
    buttons.push({ btn, tabId: id });
    wrap.appendChild(btn);
  });

  setActive(active || tabs[0]?.id);
  return wrap;
}
