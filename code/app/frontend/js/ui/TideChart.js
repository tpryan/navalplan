/**
 * TideChart — sine curve with filled gradient area (inline SVG)
 * @param {object} opts
 * @param {Array<{time:string, height:number}>} opts.points   tide data points
 * @param {number} [opts.width]   svg width (default 320)
 * @param {number} [opts.height]  svg height (default 80)
 */
export function TideChart({ points = [], width = 320, height = 80 } = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'np-tide-chart';
  wrap.style.cssText = `border-radius:var(--radius-tile);overflow:hidden;background:color-mix(in oklab,var(--coral) 6%,var(--surface))`;

  if (!points.length) {
    wrap.style.height = `${height}px`;
    return wrap;
  }

  const pad = 8;
  const maxH = Math.max(...points.map(p => p.height));
  const minH = Math.min(...points.map(p => p.height));
  const range = maxH - minH || 1;

  const toX = (i) => pad + (i / (points.length - 1)) * (width - pad * 2);
  const toY = (h) => pad + (1 - (h - minH) / range) * (height - pad * 2);

  const pathD = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${toX(i).toFixed(1)},${toY(p.height).toFixed(1)}`).join(' ');
  const areaD = `${pathD} L${toX(points.length - 1).toFixed(1)},${height} L${toX(0).toFixed(1)},${height} Z`;

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
  svg.setAttribute('width', width);
  svg.setAttribute('height', height);
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', 'Tide chart');
  svg.style.display = 'block';

  const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
  defs.innerHTML = `
    <linearGradient id="tide-fill" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="var(--coral)" stop-opacity="0.25"/>
      <stop offset="100%" stop-color="var(--coral)" stop-opacity="0.03"/>
    </linearGradient>`;
  svg.appendChild(defs);

  const area = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  area.setAttribute('d', areaD);
  area.setAttribute('fill', 'url(#tide-fill)');
  svg.appendChild(area);

  const line = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  line.setAttribute('d', pathD);
  line.setAttribute('fill', 'none');
  line.setAttribute('stroke', 'var(--coral)');
  line.setAttribute('stroke-width', '2');
  line.setAttribute('stroke-linecap', 'round');
  line.setAttribute('stroke-linejoin', 'round');
  svg.appendChild(line);

  wrap.appendChild(svg);
  return wrap;
}
