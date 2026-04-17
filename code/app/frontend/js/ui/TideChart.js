/**
 * TideChart — smooth catmull-rom tide curve with filled gradient area (inline SVG)
 * @param {object} opts
 * @param {Array<{time:string, height:number}>} opts.points   tide event points (high/low events)
 * @param {number} [opts.width]   svg width (default 320)
 * @param {number} [opts.height]  svg height (default 80)
 */
export function TideChart({ points = [], width = 320, height = 80 } = {}) {
  const wrap = document.createElement('div');
  wrap.className = 'np-tide-chart';
  wrap.style.cssText = `border-radius:var(--radius-tile);overflow:hidden;background:color-mix(in oklab,var(--coral) 6%,var(--surface));width:100%;height:${height}px;display:block`;

  if (!points.length) {
    wrap.style.height = `${height}px`;
    return wrap;
  }

  const pad = 8;

  // Parse time string to decimal hours (0–24). Falls back to evenly-distributed if unparseable.
  const parseHours = (timeStr) => {
    if (!timeStr) return null;
    const clean = timeStr.replace('Z', '').replace(' ', 'T');
    const d = new Date(clean.length === 10 ? clean + 'T00:00:00' : clean);
    if (isNaN(d.getTime())) return null;
    return d.getHours() + d.getMinutes() / 60 + d.getSeconds() / 3600;
  };

  const mapped = points.map((p, i) => {
    const h = parseHours(p.time);
    return { x: h !== null ? h : (i / Math.max(points.length - 1, 1)) * 24, y: p.height };
  });
  mapped.sort((a, b) => a.x - b.x);

  const maxH = Math.max(...mapped.map(p => p.y));
  const minH = Math.min(...mapped.map(p => p.y));
  const range = maxH - minH || 1;

  const toX = (hours) => pad + (hours / 24) * (width - pad * 2);
  const toY = (h) => pad + (1 - (h - minH) / range) * (height - pad * 2);

  const pts = mapped.map(p => ({ x: toX(p.x), y: toY(p.y) }));

  // Catmull-Rom → cubic bezier for smooth tide curve
  const get = (i) => pts[Math.max(0, Math.min(pts.length - 1, i))];
  let pathD = `M${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = get(i - 1), p1 = get(i), p2 = get(i + 1), p3 = get(i + 2);
    const cp1x = p1.x + (p2.x - p0.x) / 6;
    const cp1y = p1.y + (p2.y - p0.y) / 6;
    const cp2x = p2.x - (p3.x - p1.x) / 6;
    const cp2y = p2.y - (p3.y - p1.y) / 6;
    pathD += ` C${cp1x.toFixed(1)},${cp1y.toFixed(1)} ${cp2x.toFixed(1)},${cp2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`;
  }
  const last = pts[pts.length - 1];
  const areaD = `${pathD} L${last.x.toFixed(1)},${height} L${pts[0].x.toFixed(1)},${height} Z`;

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', 'Tide chart');
  svg.style.cssText = 'display:block;width:100%;height:100%';

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
