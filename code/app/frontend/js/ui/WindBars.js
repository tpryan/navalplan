/**
 * WindBars — 7–12 bar chart for hourly wind (sky→teal gradient)
 * @param {object} opts
 * @param {Array<{label:string, speed:number}>} opts.bars
 * @param {number} [opts.width]
 * @param {number} [opts.height]
 */
export function WindBars({ bars = [], width = 200, height = 80 } = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'np-wind-bars';
  wrap.style.cssText = `border-radius:var(--radius-tile);overflow:hidden;background:color-mix(in oklab,var(--sky) 6%,var(--surface))`;

  if (!bars.length) {
    wrap.style.cssText += `;height:${height}px`;
    return wrap;
  }

  const padX = 8, padY = 12;
  const barW = Math.max(6, Math.floor((width - padX * 2 - (bars.length - 1) * 4) / bars.length));
  const maxSpeed = Math.max(...bars.map(b => b.speed), 1);
  const svgH = height + 16;

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${width} ${svgH}`);
  svg.setAttribute('width', width);
  svg.setAttribute('height', svgH);
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', 'Wind speed chart');
  svg.style.display = 'block';

  const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
  defs.innerHTML = `
    <linearGradient id="wind-grad" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="var(--sky)" stop-opacity="0.9"/>
      <stop offset="100%" stop-color="var(--teal)" stop-opacity="0.7"/>
    </linearGradient>`;
  svg.appendChild(defs);

  bars.forEach((bar, i) => {
    const barH = Math.max(4, ((bar.speed / maxSpeed) * (height - padY * 2)));
    const x = padX + i * (barW + 4);
    const y = padY + (height - padY * 2) - barH;

    const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    rect.setAttribute('x', x);
    rect.setAttribute('y', y);
    rect.setAttribute('width', barW);
    rect.setAttribute('height', barH);
    rect.setAttribute('rx', Math.min(barW / 2, 6));
    rect.setAttribute('ry', Math.min(barW / 2, 6));
    rect.setAttribute('fill', 'url(#wind-grad)');
    svg.appendChild(rect);

    const lbl = document.createElementNS('http://www.w3.org/2000/svg', 'text');
    lbl.setAttribute('x', x + barW / 2);
    lbl.setAttribute('y', svgH - 2);
    lbl.setAttribute('text-anchor', 'middle');
    lbl.setAttribute('font-size', '9');
    lbl.setAttribute('fill', 'var(--muted)');
    lbl.setAttribute('font-family', 'var(--font)');
    lbl.textContent = bar.label;
    svg.appendChild(lbl);
  });

  wrap.appendChild(svg);
  return wrap;
}
