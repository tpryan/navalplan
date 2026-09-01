import { esc } from '../utils.js';

/**
 * Normalizes pilot_notes data from object, JSON string, or markdown string.
 * @param {object|string} pilotNotes
 * @returns {object|string|null}
 */
export function normalizePilotNotes(pilotNotes) {
  if (!pilotNotes) return null;
  let data = pilotNotes;
  if (typeof data === 'string') {
    try {
      data = JSON.parse(data);
    } catch (_) {
      // Keep as string
    }
  }
  if (!data) return null;
  if (typeof data === 'object' && Object.keys(data).length === 0) return null;
  return data;
}

/**
 * PilotNotesBox — creates a DOM section element containing structured pilotage notes.
 * @param {object|string} pilotNotes
 * @returns {HTMLElement|null}
 */
export function PilotNotesBox(pilotNotes) {
  const data = normalizePilotNotes(pilotNotes);
  if (!data) return null;

  const sec = document.createElement('div');
  sec.className = 'briefing-section pilot-notes-section';

  const header = document.createElement('h3');
  header.className = 'briefing-header-icon';
  header.innerHTML = '<span class="material-symbols-outlined">menu_book</span> Pilotage & Harbor Guidance';
  sec.appendChild(header);

  const container = document.createElement('div');
  container.className = 'pilot-notes-container';

  if (typeof data === 'string') {
    const p = document.createElement('p');
    p.className = 'pilot-notes-prose';
    p.textContent = data;
    container.appendChild(p);
    sec.appendChild(container);
    return sec;
  }

  const overview = data.overview || data.summary || data.description;
  if (overview) {
    const p = document.createElement('p');
    p.className = 'pilot-notes-overview';
    p.textContent = overview;
    container.appendChild(p);
  }

  const grid = document.createElement('div');
  grid.className = 'pilot-notes-grid';

  const addCard = (iconName, title, text) => {
    if (!text) return;
    const card = document.createElement('div');
    card.className = 'pilot-note-card';

    const head = document.createElement('div');
    head.className = 'pilot-note-card__header';
    head.innerHTML = `<span class="material-symbols-outlined pilot-note-card__icon">${iconName}</span><strong>${title}</strong>`;
    card.appendChild(head);

    const body = document.createElement('div');
    body.className = 'pilot-note-card__content';
    body.textContent = text;
    card.appendChild(body);

    grid.appendChild(card);
  };

  addCard('straighten', 'Approaches & Channels', data.approach_and_channels || data.approach || data.channels);
  addCard('anchor', 'Anchorages & Moorings', data.anchorages_and_moorings || data.anchorages || data.moorings);
  addCard('shield', 'Regulations & Hazards', data.regulations_and_hazards || data.regulations || data.hazards);

  if (grid.children.length > 0) {
    container.appendChild(grid);
  }

  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(data.references) ? data.references : []);
  if (sources.length > 0) {
    const srcDiv = document.createElement('div');
    srcDiv.className = 'pilot-notes-sources';
    srcDiv.innerHTML = `<span class="pilot-notes-sources__label">Sources: </span>` + sources.map(s => {
      const sStr = String(s);
      if (sStr.startsWith('http://') || sStr.startsWith('https://')) {
        return `<a href="${esc(sStr)}" target="_blank" rel="noopener" class="ref-anchor">${esc(sStr)}</a>`;
      }
      return `<span class="pilot-source-tag">${esc(sStr)}</span>`;
    }).join(' ');
    container.appendChild(srcDiv);
  }

  sec.appendChild(container);
  return sec;
}

/**
 * renderPilotNotesHTML — returns an HTML string for static report rendering.
 * @param {object|string} pilotNotes
 * @returns {string}
 */
export function renderPilotNotesHTML(pilotNotes) {
  const data = normalizePilotNotes(pilotNotes);
  if (!data) return '';

  if (typeof data === 'string') {
    return `
      <div class="briefing-section pilot-notes-section">
        <h3 class="briefing-header-icon"><span class="material-symbols-outlined">menu_book</span> Pilotage & Harbor Guidance</h3>
        <div class="pilot-notes-container">
          <p class="pilot-notes-prose">${esc(data)}</p>
        </div>
      </div>
    `;
  }

  const overview = data.overview || data.summary || data.description || '';
  const approach = data.approach_and_channels || data.approach || data.channels || '';
  const anchorages = data.anchorages_and_moorings || data.anchorages || data.moorings || '';
  const regulations = data.regulations_and_hazards || data.regulations || data.hazards || '';
  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(data.references) ? data.references : []);

  let itemsHtml = '';
  if (overview) {
    itemsHtml += `<p class="pilot-notes-overview">${esc(overview)}</p>`;
  }

  const subCards = [];
  if (approach) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">straighten</span>
          <strong>Approaches & Channels</strong>
        </div>
        <div class="pilot-note-card__content">${esc(approach)}</div>
      </div>
    `);
  }
  if (anchorages) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">anchor</span>
          <strong>Anchorages & Moorings</strong>
        </div>
        <div class="pilot-note-card__content">${esc(anchorages)}</div>
      </div>
    `);
  }
  if (regulations) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">shield</span>
          <strong>Regulations & Hazards</strong>
        </div>
        <div class="pilot-note-card__content">${esc(regulations)}</div>
      </div>
    `);
  }

  if (subCards.length > 0) {
    itemsHtml += `<div class="pilot-notes-grid">${subCards.join('')}</div>`;
  }

  if (sources.length > 0) {
    itemsHtml += `
      <div class="pilot-notes-sources">
        <span class="pilot-notes-sources__label">Sources:</span>
        ${sources.map(s => {
          const sStr = String(s);
          if (sStr.startsWith('http://') || sStr.startsWith('https://')) {
            return `<a href="${esc(sStr)}" target="_blank" rel="noopener" class="ref-anchor">${esc(sStr)}</a>`;
          }
          return `<span class="pilot-source-tag">${esc(sStr)}</span>`;
        }).join(' ')}
      </div>
    `;
  }

  return `
    <div class="briefing-section pilot-notes-section">
      <h3 class="briefing-header-icon"><span class="material-symbols-outlined">menu_book</span> Pilotage & Harbor Guidance</h3>
      <div class="pilot-notes-container">
        ${itemsHtml}
      </div>
    </div>
  `;
}
