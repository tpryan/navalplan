/**
 * ScoreRing — circular progress SVG (destination suitability)
 * @param {object} opts
 * @param {number} opts.score    0–100
 * @param {string} [opts.accent] token name (default coral)
 * @param {number} [opts.size]   px (default 64)
 */
export function ScoreRing({ score = 0, accent = 'coral', size = 64 } = {}) {
  const r = (size / 2) - 5;
  const circ = 2 * Math.PI * r;
  const fill = circ * (score / 100);

  const el = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  el.setAttribute('viewBox', `0 0 ${size} ${size}`);
  el.setAttribute('width', size);
  el.setAttribute('height', size);
  el.setAttribute('role', 'img');
  el.setAttribute('aria-label', `Score: ${score} out of 100`);
  el.style.display = 'block';

  const bg = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
  bg.setAttribute('cx', size / 2);
  bg.setAttribute('cy', size / 2);
  bg.setAttribute('r', r);
  bg.setAttribute('fill', 'none');
  bg.setAttribute('stroke', 'var(--hair)');
  bg.setAttribute('stroke-width', '3');
  el.appendChild(bg);

  const arc = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
  arc.setAttribute('cx', size / 2);
  arc.setAttribute('cy', size / 2);
  arc.setAttribute('r', r);
  arc.setAttribute('fill', 'none');
  arc.setAttribute('stroke', `var(--${accent})`);
  arc.setAttribute('stroke-width', '3');
  arc.setAttribute('stroke-linecap', 'round');
  arc.setAttribute('stroke-dasharray', `${fill} ${circ}`);
  arc.setAttribute('transform', `rotate(-90 ${size / 2} ${size / 2})`);
  el.appendChild(arc);

  const label = document.createElementNS('http://www.w3.org/2000/svg', 'text');
  label.setAttribute('x', size / 2);
  label.setAttribute('y', size / 2 + 5);
  label.setAttribute('text-anchor', 'middle');
  label.setAttribute('font-size', '14');
  label.setAttribute('font-weight', '800');
  label.setAttribute('fill', `var(--${accent})`);
  label.setAttribute('font-family', 'var(--font)');
  label.textContent = score;
  el.appendChild(label);

  return el;
}
