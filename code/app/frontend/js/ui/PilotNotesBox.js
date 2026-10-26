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

export function formatTimingHTML(timing) {
  if (!timing) return '';
  const paragraphs = [];

  if (typeof timing === 'object') {
    if (timing.recommended_departure_window) {
      paragraphs.push(
        `<p class="pilot-note-card__lead"><strong>Recommended Departure: ${esc(String(timing.recommended_departure_window))}</strong></p>`
      );
    }
    if (timing.departure_tide_constraint) {
      paragraphs.push(`<p class="pilot-note-card__para">${esc(String(timing.departure_tide_constraint))}</p>`);
    }
    if (timing.arrival_outlook) {
      paragraphs.push(`<p class="pilot-note-card__para">${esc(String(timing.arrival_outlook))}</p>`);
    }
    if (timing.night_entry_and_aids_warning) {
      paragraphs.push(`<p class="pilot-note-card__para">${esc(String(timing.night_entry_and_aids_warning))}</p>`);
    }
    return paragraphs.join('');
  }

  if (typeof timing === 'string') {
    const segments = timing
      .split(/\s+·\s+|\n+/)
      .map(s => s.trim())
      .filter(Boolean);
    segments.forEach(seg => {
      if (/^recommended departure:/i.test(seg)) {
        paragraphs.push(`<p class="pilot-note-card__lead"><strong>${esc(seg)}</strong></p>`);
      } else {
        paragraphs.push(`<p class="pilot-note-card__para">${esc(seg)}</p>`);
      }
    });
    return paragraphs.join('');
  }

  return '';
}

/**
 * Resolves a pilotage source citation string or object to an authoritative URL
 * (NOAA Coast Pilot PDF, eCFR section/part, NGA Sailing Directions, USCG NavCen, or Google Search fallback).
 *
 * @param {string|object} source
 * @returns {{ label: string, url: string }|null}
 */
export function resolvePilotSourceLink(source) {
  if (!source) return null;

  if (typeof source === 'object') {
    const rawUrl = source.url || source.uri || source.href || '';
    const rawLabel = source.title || source.name || source.label || rawUrl;
    if (!rawLabel) return null;
    if (rawUrl && /^https?:\/\//i.test(String(rawUrl).trim())) {
      return { label: String(rawLabel).trim(), url: String(rawUrl).trim() };
    }
    const resolved = resolvePilotSourceLink(String(rawLabel));
    return resolved;
  }

  const sStr = String(source).trim();
  if (!sStr) return null;

  if (/^https?:\/\//i.test(sStr)) {
    return { label: sStr, url: sStr };
  }

  const embeddedUrlMatch = sStr.match(/^(.*?)\s*\((https?:\/\/[^\s)]+)\)\s*$/i);
  if (embeddedUrlMatch) {
    return {
      label: embeddedUrlMatch[1].trim() || embeddedUrlMatch[2],
      url: embeddedUrlMatch[2],
    };
  }

  // 1. NOAA Coast Pilot (Volumes 1-10)
  const cpMatch = sStr.match(/\b(?:NOAA\s+)?Coast\s+Pilot\s*(?:Vol(?:ume)?\.?\s*)?(\d{1,2})\b/i);
  if (cpMatch) {
    const vol = parseInt(cpMatch[1], 10);
    if (vol >= 1 && vol <= 10) {
      return {
        label: sStr,
        url: `https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp${vol}/CPB${vol}_WEB.pdf`,
      };
    }
  }

  // 2. Code of Federal Regulations (e.g., "33 CFR § 110.159" or "33 CFR 110.159" or "33 CFR Part 110")
  const cfrMatch = sStr.match(/\b(\d+)\s*C\.?F\.?R\.?\s*(?:Part\s*|§+\s*|Sec(?:tion)?\.?\s*)?(\d+(?:\.\d+[a-z0-9-]*)?)/i);
  if (cfrMatch) {
    const titleNum = cfrMatch[1];
    const secOrPart = cfrMatch[2];
    if (secOrPart.includes('.')) {
      return {
        label: sStr,
        url: `https://www.ecfr.gov/current/title-${titleNum}/section-${secOrPart}`,
      };
    }
    return {
      label: sStr,
      url: `https://www.ecfr.gov/current/title-${titleNum}/part-${secOrPart}`,
    };
  }

  // 3. NGA Sailing Directions
  if (/\bNGA\s+Sailing\s+Directions\b/i.test(sStr)) {
    const isPlanning = /\bplanning\b/i.test(sStr);
    return {
      label: sStr,
      url: isPlanning
        ? 'https://msi.nga.mil/Publications/SDPlanning'
        : 'https://msi.nga.mil/Publications/SDEnroute',
    };
  }

  // 4. USCG Light List / Local Notice to Mariners
  if (/\b(?:USCG\s+)?(?:Light\s+List|Local\s+Notice\s+to\s+Mariners|LNM)\b/i.test(sStr)) {
    return {
      label: sStr,
      url: 'https://www.navcen.uscg.gov/',
    };
  }

  // 5. Fallback: search query link for municipal/harbor codes and other named publications
  return {
    label: sStr,
    url: `https://www.google.com/search?q=${encodeURIComponent(sStr)}`,
  };
}

function renderPilotSourceTagHTML(source) {
  const resolved = resolvePilotSourceLink(source);
  if (!resolved) return '';
  return `<a href="${esc(resolved.url)}" target="_blank" rel="noopener noreferrer" class="pilot-source-tag pilot-source-tag--link ref-anchor">${esc(resolved.label)}</a>`;
}

function formatCardParagraphsHTML(text) {
  if (!text) return '';
  const segments = String(text)
    .split(/\s+·\s+|\n+/)
    .map(s => s.trim())
    .filter(Boolean);
  if (segments.length <= 1) {
    return `<p class="pilot-note-card__para">${esc(String(text).trim())}</p>`;
  }
  return segments.map(seg => `<p class="pilot-note-card__para">${esc(seg)}</p>`).join('');
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

  const addCardHTML = (iconName, title, bodyHtml) => {
    if (!bodyHtml) return;
    const card = document.createElement('div');
    card.className = 'pilot-note-card';

    const head = document.createElement('div');
    head.className = 'pilot-note-card__header';
    head.innerHTML = `<span class="material-symbols-outlined pilot-note-card__icon">${iconName}</span><strong>${title}</strong>`;
    card.appendChild(head);

    const body = document.createElement('div');
    body.className = 'pilot-note-card__content';
    body.innerHTML = bodyHtml;
    card.appendChild(body);

    grid.appendChild(card);
  };

  addCardHTML('schedule', 'Departure & Arrival Timing', formatTimingHTML(data.departure_and_arrival_timing));
  addCardHTML('straighten', 'Approaches & Channels', formatCardParagraphsHTML(data.approach_and_channels || data.approach || data.channels));
  addCardHTML('anchor', 'Anchorages & Moorings', formatCardParagraphsHTML(data.anchorages_and_moorings || data.anchorages || data.moorings));
  addCardHTML('shield', 'Regulations & Hazards', formatCardParagraphsHTML(data.regulations_and_hazards || data.regulations || data.hazards));

  if (grid.children.length > 0) {
    container.appendChild(grid);
  }

  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(data.references) ? data.references : []);
  if (sources.length > 0) {
    const srcDiv = document.createElement('div');
    srcDiv.className = 'pilot-notes-sources';
    srcDiv.innerHTML = `<span class="pilot-notes-sources__label">Sources: </span>` + sources.map(renderPilotSourceTagHTML).filter(Boolean).join(' ');
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
  const timingHtml = formatTimingHTML(data.departure_and_arrival_timing);
  const approachHtml = formatCardParagraphsHTML(data.approach_and_channels || data.approach || data.channels || '');
  const anchoragesHtml = formatCardParagraphsHTML(data.anchorages_and_moorings || data.anchorages || data.moorings || '');
  const regulationsHtml = formatCardParagraphsHTML(data.regulations_and_hazards || data.regulations || data.hazards || '');
  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(data.references) ? data.references : []);

  let itemsHtml = '';
  if (overview) {
    itemsHtml += `<p class="pilot-notes-overview">${esc(overview)}</p>`;
  }

  const subCards = [];
  if (timingHtml) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">schedule</span>
          <strong>Departure & Arrival Timing</strong>
        </div>
        <div class="pilot-note-card__content">${timingHtml}</div>
      </div>
    `);
  }
  if (approachHtml) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">straighten</span>
          <strong>Approaches & Channels</strong>
        </div>
        <div class="pilot-note-card__content">${approachHtml}</div>
      </div>
    `);
  }
  if (anchoragesHtml) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">anchor</span>
          <strong>Anchorages & Moorings</strong>
        </div>
        <div class="pilot-note-card__content">${anchoragesHtml}</div>
      </div>
    `);
  }
  if (regulationsHtml) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">shield</span>
          <strong>Regulations & Hazards</strong>
        </div>
        <div class="pilot-note-card__content">${regulationsHtml}</div>
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
        ${sources.map(renderPilotSourceTagHTML).filter(Boolean).join(' ')}
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
