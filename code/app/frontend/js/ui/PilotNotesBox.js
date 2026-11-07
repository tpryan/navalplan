import { esc, expandAcronymsFirstUse } from '../utils.js';

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

function isoWeekStartDateUTC(year, week) {
  const jan4 = new Date(Date.UTC(year, 0, 4));
  const dayOfWeek = jan4.getUTCDay() || 7;
  const week1Monday = new Date(jan4.getTime() - (dayOfWeek - 1) * 86400000);
  return new Date(week1Monday.getTime() + (week - 1) * 7 * 86400000);
}

function formatShortDateUTC(dateObj) {
  if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) return '';
  return dateObj.toLocaleDateString('en-US', {
    timeZone: 'UTC',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

function extractCoastPilotDeepFragment(text) {
  const s = String(text || '');
  const pageMatch = s.match(/\b(?:pp?\.?|pages?)\s*(\d{1,4})\b/i);
  if (pageMatch) {
    return `#page=${pageMatch[1]}`;
  }
  const chapMatch = s.match(/\b(Chapter\s+\d{1,2})\b/i);
  if (chapMatch) {
    return `#search=${encodeURIComponent(chapMatch[1])}`;
  }
  const parenTopic = s.match(/\(([^()]{3,40})\)/);
  if (parenTopic && !/^https?:\/\//i.test(parenTopic[1])) {
    return `#search=${encodeURIComponent(parenTopic[1].trim())}`;
  }
  return '';
}

function extractLNMDateHintFromData(data, safetyAlerts = null) {
  const texts = [];
  if (data && typeof data === 'object') {
    if (Array.isArray(data.local_notices)) texts.push(...data.local_notices);
    else if (typeof data.local_notices === 'string') texts.push(data.local_notices);
    if (Array.isArray(data.sources)) texts.push(...data.sources.map(s => (typeof s === 'string' ? s : s?.label || s?.title || '')));
  }
  if (Array.isArray(safetyAlerts)) {
    safetyAlerts.forEach(a => {
      if (a?.message) texts.push(String(a.message));
    });
  }
  for (const raw of texts) {
    const str = String(raw || '');
    const lnmFile = str.match(/\blnm(\d{2}|08[gr])(\d{2})(20\d{2})\b/i);
    if (lnmFile) {
      const wk = parseInt(lnmFile[2], 10);
      const yr = parseInt(lnmFile[3], 10);
      const dt = formatShortDateUTC(isoWeekStartDateUTC(yr, wk));
      return { distCode: lnmFile[1].toLowerCase(), week: wk, year: yr, dateLabel: `Week ${wk} (${dt})` };
    }
    const wkMatch = str.match(/\bWeek\s*(\d{1,2})\s*(?:\(([^)]+)\)|[,\s/-]+(20\d{2}))/i);
    if (wkMatch) {
      const wk = parseInt(wkMatch[1], 10);
      if (wkMatch[2]) {
        return { week: wk, dateLabel: `Week ${wk} (${wkMatch[2].trim()})` };
      }
      const yr = parseInt(wkMatch[3], 10);
      const dt = formatShortDateUTC(isoWeekStartDateUTC(yr, wk));
      return { week: wk, year: yr, dateLabel: `Week ${wk} (${dt})` };
    }
  }
  return null;
}

/**
 * Resolves a pilotage source citation string or object to an authoritative URL
 * (NOAA Coast Pilot PDF with #page/#search deep link, eCFR section/part,
 * NGA Sailing Directions, USCG NAVCEN LNM weekly PDF / district page with date, or Google Search fallback).
 *
 * @param {string|object} source
 * @param {{ dateLabel?: string, distCode?: string, week?: number, year?: number }|null} [lnmHint]
 * @returns {{ label: string, url: string }|null}
 */
export function resolvePilotSourceLink(source, lnmHint = null) {
  if (!source) return null;

  if (typeof source === 'object') {
    const rawUrl = source.url || source.uri || source.href || '';
    const rawLabel = source.title || source.name || source.label || rawUrl;
    if (!rawLabel) return null;
    if (rawUrl && /^https?:\/\//i.test(String(rawUrl).trim())) {
      return { label: String(rawLabel).trim(), url: String(rawUrl).trim() };
    }
    return resolvePilotSourceLink(String(rawLabel), lnmHint);
  }

  const sStr = String(source).trim();
  if (!sStr) return null;

  if (/^https?:\/\//i.test(sStr)) {
    return { label: sStr, url: sStr };
  }

  const embeddedUrlMatch = sStr.match(/^(.*?)\s*\((https?:\/\/[^\s)]+)\)\s*$/i);
  if (embeddedUrlMatch) {
    let label = embeddedUrlMatch[1].trim() || embeddedUrlMatch[2];
    let url = embeddedUrlMatch[2];
    if (/CPB\d+_WEB\.pdf$/i.test(url)) {
      url += extractCoastPilotDeepFragment(label);
    }
    return { label, url };
  }

  // 1. NOAA Coast Pilot (Volumes 1-10) — deep link with #page=N or #search=Chapter%20N
  const cpMatch = sStr.match(/\b(?:NOAA\s+)?Coast\s+Pilot\s*(?:Vol(?:ume)?\.?\s*)?(\d{1,2})\b/i);
  if (cpMatch) {
    const vol = parseInt(cpMatch[1], 10);
    if (vol >= 1 && vol <= 10) {
      const baseUrl = `https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp${vol}/CPB${vol}_WEB.pdf`;
      const fragment = extractCoastPilotDeepFragment(sStr);
      return {
        label: sStr,
        url: `${baseUrl}${fragment}`,
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

  // 4. Direct USCG LNM filename (e.g. "lnm11452026.pdf" or citation containing lnm11452026)
  const lnmFileMatch = sStr.match(/\blnm(\d{2}|08[gr])(\d{2})(20\d{2})(?:\.pdf)?\b/i);
  if (lnmFileMatch) {
    const distCode = lnmFileMatch[1].toLowerCase();
    const week = parseInt(lnmFileMatch[2], 10);
    const year = parseInt(lnmFileMatch[3], 10);
    const distNum = distCode.replace(/^0+/, '').replace(/[gr]$/i, '');
    const pubDate = formatShortDateUTC(isoWeekStartDateUTC(year, week));
    const pageMatch = sStr.match(/\b(?:pp?\.?|pages?)\s*(\d{1,4})\b/i);
    const pageFrag = pageMatch ? `#page=${pageMatch[1]}` : '';
    const pdfUrl = `https://www.navcen.uscg.gov/sites/default/files/pdf/lnms/lnm${distCode}${String(week).padStart(2, '0')}${year}.pdf${pageFrag}`;
    const label = /^lnm\d/i.test(sStr)
      ? `USCG District ${distNum} Local Notice to Mariners — Week ${week} (${pubDate})`
      : (/\b(20\d{2}|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\b/i.test(sStr) ? sStr : `${sStr} — Week ${week} (${pubDate})`);
    return { label, url: pdfUrl };
  }

  // 5. USCG Light List / Local Notice to Mariners
  if (/\b(?:USCG\s+)?(?:Light\s+List|Local\s+Notice\s+to\s+Mariners|LNM)\b/i.test(sStr)) {
    if (/\bLight\s+List\b/i.test(sStr) && !/\b(?:Local\s+Notice\s+to\s+Mariners|LNM)\b/i.test(sStr)) {
      return {
        label: sStr,
        url: 'https://www.navcen.uscg.gov/light-lists',
      };
    }

    const distMatch = sStr.match(/\b(?:District|Dist\.?|D)\s*(\d{1,2})\s*(?:\((Gulf|Western\s+Rivers)\)|([gr]))?\b/i);
    const wkYearMatch = sStr.match(/\b(?:Week|Wk\.?|LNM)\s*(\d{1,2})[,\s/-]+(20\d{2})\b/i);
    const pageMatch = sStr.match(/\b(?:pp?\.?|pages?)\s*(\d{1,4})\b/i);
    const pageFrag = pageMatch ? `#page=${pageMatch[1]}` : '';

    if (distMatch) {
      const distNum = parseInt(distMatch[1], 10);
      const subText = `${distMatch[2] || ''}${distMatch[3] || ''}`.toLowerCase();
      let subdistrict = 'n';
      let distPfx = String(distNum).padStart(2, '0');
      if (distNum === 8) {
        if (subText.startsWith('r') || subText.includes('river')) {
          subdistrict = 'r';
          distPfx = '08r';
        } else {
          subdistrict = 'g';
          distPfx = '08g';
        }
      }

      const weekNum = wkYearMatch ? parseInt(wkYearMatch[1], 10) : (lnmHint?.week || 0);
      const yearNum = wkYearMatch ? parseInt(wkYearMatch[2], 10) : (lnmHint?.year || 0);

      let label = sStr;
      const hasDateInLabel = /\b(20\d{2}|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec|week\s*\d+)\b/i.test(label);
      if (weekNum >= 1 && weekNum <= 53 && yearNum >= 2020) {
        const pubDate = formatShortDateUTC(isoWeekStartDateUTC(yearNum, weekNum));
        if (!hasDateInLabel && pubDate) {
          label = `${label} — Week ${weekNum} (${pubDate})`;
        } else if (wkYearMatch && pubDate && !/\([A-Za-z]{3}\s+\d{1,2},\s+20\d{2}\)/.test(label)) {
          label = `${label} (${pubDate})`;
        }
        return {
          label,
          url: `https://www.navcen.uscg.gov/sites/default/files/pdf/lnms/lnm${distPfx}${String(weekNum).padStart(2, '0')}${yearNum}.pdf${pageFrag}`,
        };
      }

      if (!hasDateInLabel && lnmHint?.dateLabel) {
        label = `${label} — ${lnmHint.dateLabel}`;
      }
      return {
        label,
        url: `https://www.navcen.uscg.gov/local-notices-to-mariners?district=${distNum}+0&subdistrict=${subdistrict}`,
      };
    }

    let label = sStr;
    const hasDateInLabel = /\b(20\d{2}|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec|week\s*\d+)\b/i.test(label);
    if (!hasDateInLabel && lnmHint?.dateLabel) {
      label = `${label} — ${lnmHint.dateLabel}`;
    }
    return {
      label,
      url: 'https://www.navcen.uscg.gov/local-notices-to-mariners-by-cg-district',
    };
  }

  // 6. Fallback: search query link for municipal/harbor codes and other named publications
  return {
    label: sStr,
    url: `https://www.google.com/search?q=${encodeURIComponent(sStr)}`,
  };
}

function renderPilotSourceTagHTML(source, lnmHint = null) {
  const resolved = resolvePilotSourceLink(source, lnmHint);
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

const LNM_SENTENCE_RE = /\b(lnm|local\s+notice\s+to\s+mariners|notice\s+to\s+mariners|uscg|coast\s+guard|discrepanc|extinguished|off[\s-]station|missing\s+buoy|unlit\s+(?:buoy|daybeacon|beacon|light|aid)|daybeacon|light\s+list|dredg(?:ing|e)|shoal(?:ing)?|temporary\s+(?:buoy|closure|bridge|restriction|safety\s+zone)|safety\s+zone|security\s+zone|marine\s+event|submerged\s+(?:wreck|obstruction))\b/i;

function resolveLocalNotices(data, safetyAlerts = null) {
  if (!data || typeof data !== 'object') return [];
  const seen = new Set();
  const out = [];
  const pushItem = (raw) => {
    const clean = String(raw || '').trim();
    if (!clean) return;
    if (/^(none|n\/a|no\s+active\s+local\s+notices)/i.test(clean) && clean.length < 45) return;
    const key = clean.toLowerCase();
    if (seen.has(key)) return;
    seen.add(key);
    out.push(clean);
  };

  const direct = data.local_notices || data.lnm || data.notices_to_mariners;
  if (Array.isArray(direct)) {
    direct.forEach(pushItem);
  } else if (typeof direct === 'string' && direct.trim()) {
    direct.split(/\s+·\s+|\n+/).forEach(pushItem);
  }

  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(data.references) ? data.references : []);
  const hasLNMSource = sources.some(s => /\b(Local\s+Notice\s+to\s+Mariners|LNM|Light\s+List)\b/i.test(String(s || '')));

  const candidateFields = [
    data.departure_and_arrival_timing?.night_entry_and_aids_warning,
    data.regulations_and_hazards || data.regulations || data.hazards,
    data.approach_and_channels || data.approach || data.channels,
    data.overview || data.summary,
  ];

  for (const field of candidateFields) {
    if (!field) continue;
    const text = Array.isArray(field) ? field.join('\n') : String(field);
    const segments = text
      .split(/\s+·\s+|\n+|(?<=[.!?])\s+(?=[A-Z0-9"'])/)
      .map(s => s.trim())
      .filter(Boolean);
    for (const seg of segments) {
      if (LNM_SENTENCE_RE.test(seg)) {
        pushItem(seg);
      }
    }
  }

  if (out.length === 0 && hasLNMSource) {
    const fallbackText = data.departure_and_arrival_timing?.night_entry_and_aids_warning || data.regulations_and_hazards || '';
    if (fallbackText) {
      String(fallbackText).split(/\s+·\s+|\n+/).forEach(pushItem);
    }
  }

  if (Array.isArray(safetyAlerts)) {
    for (const a of safetyAlerts) {
      const msg = String(a?.message || '').trim();
      if (msg && /\b(lnm|local\s+notice\s+to\s+mariners|notice\s+to\s+mariners|extinguished|off[\s-]station|discrepanc|dredg)\b/i.test(msg)) {
        pushItem(msg);
      }
    }
  }

  return out;
}

function formatLocalNoticesHTML(data, safetyAlerts = null) {
  const items = Array.isArray(data) || typeof data === 'string'
    ? resolveLocalNotices({ local_notices: data }, safetyAlerts)
    : resolveLocalNotices(data, safetyAlerts);
  if (items.length === 0) return '';
  return items.map(item => `<p class="pilot-note-card__para">${esc(item)}</p>`).join('');
}

export function formatRouteComparisonHTML(routeComp) {
  if (!routeComp) return '';
  const paragraphs = [];
  const seenAcronyms = new Set();

  if (Array.isArray(routeComp)) {
    routeComp.forEach(item => {
      const clean = expandAcronymsFirstUse(String(item || '').trim(), seenAcronyms);
      if (!clean) return;
      if (/^recommended\s+(route|plan):/i.test(clean)) {
        paragraphs.push(`<p class="pilot-note-card__lead"><strong>${esc(clean)}</strong></p>`);
      } else {
        paragraphs.push(`<p class="pilot-note-card__para">${esc(clean)}</p>`);
      }
    });
    return paragraphs.join('');
  }

  if (typeof routeComp === 'object') {
    const rec = routeComp.recommended_route || routeComp.recommended_plan || routeComp.recommended_track_name;
    if (rec) {
      paragraphs.push(
        `<p class="pilot-note-card__lead"><strong>Recommended Route: ${esc(String(rec))}</strong></p>`
      );
    }
    const orderedKeys = [
      'summary',
      'wind_and_wave_comparison',
      'tidal_and_current_comparison',
      'hazards_comparison',
      'conclusions',
    ];
    const usedKeys = new Set(['recommended_route', 'recommended_plan', 'recommended_track_name', ...orderedKeys]);
    for (const key of orderedKeys) {
      if (routeComp[key]) {
        const expanded = expandAcronymsFirstUse(String(routeComp[key]), seenAcronyms);
        paragraphs.push(`<p class="pilot-note-card__para">${esc(expanded)}</p>`);
      }
    }
    for (const [k, v] of Object.entries(routeComp)) {
      if (usedKeys.has(k) || !v) continue;
      if (typeof v === 'string') {
        const expanded = expandAcronymsFirstUse(v, seenAcronyms);
        paragraphs.push(`<p class="pilot-note-card__para">${esc(expanded)}</p>`);
      } else if (Array.isArray(v)) {
        v.forEach(entry => {
          if (typeof entry === 'string' && entry.trim()) {
            const expanded = expandAcronymsFirstUse(entry.trim(), seenAcronyms);
            paragraphs.push(`<p class="pilot-note-card__para">${esc(expanded)}</p>`);
          } else if (entry && typeof entry === 'object' && entry.name) {
            const dist = entry.distance_nm != null ? ` (${Number(entry.distance_nm).toFixed(1)} NM)` : '';
            const rawSummary = entry.summary || entry.wind_angle_summary || '';
            const summary = rawSummary ? expandAcronymsFirstUse(String(rawSummary), seenAcronyms) : '';
            paragraphs.push(
              `<p class="pilot-note-card__para"><strong>${esc(String(entry.name))}${esc(dist)}</strong>${summary ? ': ' + esc(summary) : ''}</p>`
            );
          }
        });
      }
    }
    return paragraphs.join('');
  }

  if (typeof routeComp === 'string') {
    const segments = routeComp
      .split(/\s+·\s+|\n+/)
      .map(s => s.trim())
      .filter(Boolean);
    segments.forEach(seg => {
      const expanded = expandAcronymsFirstUse(seg, seenAcronyms);
      if (/^recommended\s+(route|plan):/i.test(expanded)) {
        paragraphs.push(`<p class="pilot-note-card__lead"><strong>${esc(expanded)}</strong></p>`);
      } else {
        paragraphs.push(`<p class="pilot-note-card__para">${esc(expanded)}</p>`);
      }
    });
    return paragraphs.join('');
  }

  return '';
}

/**
 * PilotNotesBox — creates a DOM section element containing structured pilotage notes.
 * @param {object|string} pilotNotes
 * @param {Array<object>} [safetyAlerts]
 * @returns {HTMLElement|null}
 */
export function PilotNotesBox(pilotNotes, safetyAlerts = null) {
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

  const addCardHTML = (iconName, title, bodyHtml, modifier = '') => {
    if (!bodyHtml) return;
    const card = document.createElement('div');
    card.className = modifier ? `pilot-note-card ${modifier}` : 'pilot-note-card';

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

  addCardHTML('campaign', 'Local Notices to Mariners (LNM)', formatLocalNoticesHTML(data, safetyAlerts), 'pilot-note-card--lnm');
  addCardHTML('alt_route', 'Route Comparison & Recommendation', formatRouteComparisonHTML(data.route_comparison));
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
 * @param {Array<object>} [safetyAlerts]
 * @returns {string}
 */
export function renderPilotNotesHTML(pilotNotes, safetyAlerts = null) {
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
  const lnmHtml = formatLocalNoticesHTML(data, safetyAlerts);
  const routeCompHtml = formatRouteComparisonHTML(data.route_comparison);
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
  if (lnmHtml) {
    subCards.push(`
      <div class="pilot-note-card pilot-note-card--lnm">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">campaign</span>
          <strong>Local Notices to Mariners (LNM)</strong>
        </div>
        <div class="pilot-note-card__content">${lnmHtml}</div>
      </div>
    `);
  }
  if (routeCompHtml) {
    subCards.push(`
      <div class="pilot-note-card">
        <div class="pilot-note-card__header">
          <span class="material-symbols-outlined pilot-note-card__icon">alt_route</span>
          <strong>Route Comparison & Recommendation</strong>
        </div>
        <div class="pilot-note-card__content">${routeCompHtml}</div>
      </div>
    `);
  }
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
    const lnmHint = extractLNMDateHintFromData(data, safetyAlerts);
    itemsHtml += `
      <div class="pilot-notes-sources">
        <span class="pilot-notes-sources__label">Sources:</span>
        ${sources.map(s => renderPilotSourceTagHTML(s, lnmHint)).filter(Boolean).join(' ')}
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
