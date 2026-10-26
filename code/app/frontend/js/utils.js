// Pure utility helpers — no state, no DOM beyond the a11y announcer.
import { nmBetween } from './geometry.js';

/**
 * Announce a message to screen readers via the #a11y-announcer live region.
 */
export function announce(msg) {
    const el = document.getElementById('a11y-announcer');
    if (!el) return;
    el.textContent = '';
    requestAnimationFrame(() => { el.textContent = msg; });
}

/**
 * Strips Plus Codes (e.g. "82GQ+6Q ") from location names for cleaner UI display.
 */
export function displayLocationName(name) {
    if (!name) return '';
    return name.replace(/^[A-Z0-9]{4,8}\+[A-Z0-9]{2,3}\s*/i, '').trim();
}

/**
 * Ensures the input is an array, handling the wrapped {"recommendations": [...]} format.
 */
export function ensureRecommendationsArray(data) {
    if (!data) return [];
    if (Array.isArray(data)) return data;
    if (data.recommendations && Array.isArray(data.recommendations)) return data.recommendations;
    return [];
}

/**
 * Plain-text HTML escape — use for data fields that must never render as markup.
 */
export function esc(s) {
    return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * Render a list of reference URLs as numbered links.
 */
export function renderReferences(refs) {
    if (!refs || refs.length === 0) return '';
    return `<div class="ref-link">
        <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i + 1}]</a>`).join('')}
    </div>`;
}

/**
 * True when a voyage is a day trip — dated, with the same start and end
 * calendar date. Derived rather than stored, so it can never drift from
 * the actual dates.
 */
export function isDayTrip(voyage) {
    if (!voyage || !voyage.start_date || !voyage.end_date) return false;
    return voyage.start_date.split('T')[0] === voyage.end_date.split('T')[0];
}

/**
 * Formats a voyage's start/end dates for display, collapsing to a single
 * date when the voyage is a day trip instead of showing a "Jul 16 – Jul 16"
 * style range.
 *
 * @param {string|null} startDateStr - ISO date/datetime string, or null.
 * @param {string|null} endDateStr - ISO date/datetime string, or null.
 * @param {Object} [opts]
 * @param {Intl.DateTimeFormatOptions} [opts.format] - options passed to toLocaleDateString.
 * @param {string} [opts.separator] - separator used between start and end for multi-day ranges.
 */
export function formatVoyageDateRange(startDateStr, endDateStr, opts = {}) {
    const format = opts.format || { timeZone: 'UTC' };
    const separator = opts.separator ?? ' – ';
    if (!startDateStr) return '';
    const sameDay = endDateStr && startDateStr.split('T')[0] === endDateStr.split('T')[0];
    const start = new Date(startDateStr).toLocaleDateString(undefined, { ...format, timeZone: 'UTC' });
    if (!endDateStr || sameDay) return start;
    const end = new Date(endDateStr).toLocaleDateString(undefined, { ...format, timeZone: 'UTC' });
    return `${start}${separator}${end}`;
}

/**
 * Resolves the display title for a debrief based on voyage stops rather than raw track filenames.
 * e.g., "Marina del Rey to Isthmus Cove", or "Cowes to Yarmouth".
 *
 * @param {Object|string} debrief Debrief object or JSON string
 * @param {Array} [stopsContext] Optional array of stops
 * @param {Array} [tracksContext] Optional array of tracks
 * @returns {string} Formatted stop title or empty string if not resolvable
 */
export function getDebriefStopTitle(debrief, stopsContext = null, tracksContext = null) {
    if (!debrief) return '';
    let parsed = debrief;
    if (typeof debrief === 'string') {
        try { parsed = JSON.parse(debrief); } catch (e) { parsed = { summary: debrief }; }
    }
    if (!parsed || typeof parsed !== 'object') return '';

    // 1. Explicit stop_title or leg_title on debrief object
    if (parsed.stop_title && typeof parsed.stop_title === 'string' && parsed.stop_title.trim()) {
        return parsed.stop_title.trim();
    }
    if (parsed.leg_title && typeof parsed.leg_title === 'string' && parsed.leg_title.trim()) {
        return parsed.leg_title.trim();
    }

    const stopsList = Array.isArray(stopsContext) && stopsContext.length > 0
        ? stopsContext
        : (typeof currentStops !== 'undefined' && Array.isArray(currentStops) ? currentStops : []);

    if (!stopsList || stopsList.length === 0) return '';

    // Sort stops chronologically / by order_index
    const sorted = [...stopsList].sort((a, b) => {
        const dateA = a.target_date ? new Date(a.target_date) : 0;
        const dateB = b.target_date ? new Date(b.target_date) : 0;
        if (dateA && dateB && dateA - dateB !== 0) return dateA - dateB;
        return (a.order_index ?? 0) - (b.order_index ?? 0);
    });

    let stopIdx = -1;

    // 2. Match by start_stop_id or voyage_stop_id
    const targetStopId = parsed.start_stop_id != null ? parsed.start_stop_id : parsed.voyage_stop_id;
    if (targetStopId != null) {
        stopIdx = sorted.findIndex(s => String(s.id) === String(targetStopId));
        if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
            stopIdx = sorted.length - 2;
        }
    }

    // 3. Match by track's voyage_stop_id
    if (stopIdx === -1 && parsed.track_id) {
        const tracksList = Array.isArray(tracksContext) && tracksContext.length > 0
            ? tracksContext
            : (typeof currentTracks !== 'undefined' && Array.isArray(currentTracks) ? currentTracks : []);
        const tr = tracksList.find(t => t.id === parsed.track_id);
        if (tr && tr.voyage_stop_id != null) {
            stopIdx = sorted.findIndex(s => String(s.id) === String(tr.voyage_stop_id));
            if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
                stopIdx = sorted.length - 2;
            }
        }
    }

    // 4. Match explicit leg numbers (e.g. "Leg 1", "Leg 2")
    if (stopIdx === -1) {
        const names = `${parsed.planned_track_name || ''} ${parsed.track_name || ''}`;
        const legMatch = names.match(/\bLeg\s*#?\s*(\d+)\b/i);
        if (legMatch) {
            const legNum = parseInt(legMatch[1], 10);
            if (legNum >= 1 && legNum <= sorted.length) {
                stopIdx = legNum - 1;
                if (stopIdx === sorted.length - 1 && sorted.length >= 2) {
                    stopIdx = sorted.length - 2;
                }
            }
        }
    }

    // 5. Match by stop location names mentioned in track names
    if (stopIdx === -1) {
        const names = `${parsed.planned_track_name || ''} ${parsed.track_name || ''}`.toLowerCase();
        for (let i = 0; i < sorted.length - 1; i++) {
            const loc = displayLocationName(sorted[i].location_name || sorted[i].name || '').toLowerCase();
            if (loc && names.includes(loc)) {
                stopIdx = i;
                break;
            }
        }
    }

    // 6. Default to first stop if multiple stops exist
    if (stopIdx === -1) {
        stopIdx = 0;
    }

    if (stopIdx >= 0 && stopIdx < sorted.length) {
        const startStop = sorted[stopIdx];
        const startName = displayLocationName(startStop.location_name || startStop.name || '');
        if (stopIdx < sorted.length - 1) {
            const nextStop = sorted[stopIdx + 1];
            const nextName = displayLocationName(nextStop.location_name || nextStop.name || '');
            if (startName && nextName) {
                return `${startName} to ${nextName}`;
            }
        }
        if (startName) {
            return startName;
        }
    }

    return '';
}

/**
 * Parses a duration string (e.g. "05:18:06", "05:18", "03h 48m", "3h 48m", "45m") into minutes.
 * @param {string} str
 * @returns {number|null} Duration in minutes or null
 */
export function parseDurationToMinutes(str) {
    if (!str || typeof str !== 'string') return null;
    const s = str.trim();
    if (!s) return null;

    const colonMatch = s.match(/^(\d+):(\d{2})(?::(\d{2}))?$/);
    if (colonMatch) {
        const hrs = parseInt(colonMatch[1], 10);
        const mins = parseInt(colonMatch[2], 10);
        const secs = colonMatch[3] ? parseInt(colonMatch[3], 10) : 0;
        return hrs * 60 + mins + secs / 60;
    }

    let totalMins = 0;
    let found = false;

    const hrMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:h|hr|hrs|hour|hours)\b/i);
    if (hrMatch) {
        totalMins += parseFloat(hrMatch[1]) * 60;
        found = true;
    }

    const minMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:m|min|mins|minute|minutes)\b/i);
    if (minMatch) {
        totalMins += parseFloat(minMatch[1]);
        found = true;
    }

    const secMatch = s.match(/(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds)\b/i);
    if (secMatch) {
        totalMins += parseFloat(secMatch[1]) / 60;
        found = true;
    }

    return found ? totalMins : null;
}

/**
 * Calculates variance between recorded and planned duration.
 * @param {string} recDur
 * @param {string} planDur
 * @returns {{ pct: number, pctStr: string, diffMins: number, deltaStr: string }|null}
 */
export function calculateTimeVariance(recDur, planDur) {
    const recMins = parseDurationToMinutes(recDur);
    const planMins = parseDurationToMinutes(planDur);
    if (recMins == null || planMins == null || planMins <= 0) {
        return null;
    }

    const diffMins = recMins - planMins;
    const pct = (diffMins / planMins) * 100;
    const pctStr = `${pct > 0 ? '+' : ''}${pct.toFixed(1)}%`;

    const absDiff = Math.abs(diffMins);
    const hrs = Math.floor(absDiff / 60);
    const mins = Math.round(absDiff % 60);
    let deltaBody = '';
    if (hrs > 0 && mins > 0) {
        deltaBody = `${hrs}h ${mins}m`;
    } else if (hrs > 0) {
        deltaBody = `${hrs}h`;
    } else {
        deltaBody = `${mins}m`;
    }
    const deltaStr = `${diffMins >= 0 ? '+' : '-'}${deltaBody}`;

    return { pct, pctStr, diffMins, deltaStr };
}

/**
 * Generates formatted HTML for passage debrief cards with grouped metrics:
 * Row 1: Actual Dist | Planned Dist | Variance
 * Row 2: Actual Time | Planned Time | Variance
 * Row 3: Avg Speed | Max Speed
 *
 * @param {Object|Array|string} debrief
 * @param {Array} [stopsContext]
 * @param {Array} [tracksContext]
 * @returns {string}
 */
export function generateDebriefsHTML(debrief, stopsContext = null, tracksContext = null) {
    if (!debrief || (Array.isArray(debrief) && debrief.length === 0)) {
        return '';
    }

    const debriefs = Array.isArray(debrief) ? debrief : [debrief];
    let html = '';

    debriefs.forEach((item, index) => {
        let parsed = item;
        if (typeof item === 'string') {
            try { parsed = JSON.parse(item); } catch (e) { parsed = { summary: item }; }
        }
        if (!parsed) return;

        let obsHtml = '';
        if (Array.isArray(parsed.observations) && parsed.observations.length > 0) {
            obsHtml = `
              <div class="debrief-observations mt-sm">
                <h5 class="m-0 mb-xs">Tactical Pilot Observations:</h5>
                <ul class="m-0 pl-md">
                  ${parsed.observations.map(o => `<li>${esc(o)}</li>`).join('')}
                </ul>
              </div>
            `;
        }

        const stopTitle = getDebriefStopTitle(
            parsed,
            stopsContext || (typeof currentStops !== 'undefined' ? currentStops : null),
            tracksContext || (typeof currentTracks !== 'undefined' ? currentTracks : null)
        );

        const title = stopTitle
            ? `Passage Debrief: ${esc(stopTitle)}`
            : (parsed.track_name
                ? `Passage Debrief: ${esc(parsed.track_name)}`
                : 'Passage Debrief Analysis');

        // Distance Row: actual planned variance
        const hasRecDist = parsed.recorded_distance_nm != null;
        const hasPlanDist = parsed.planned_distance_nm != null;
        const hasDistRow = hasRecDist || hasPlanDist;
        const recDistStr = hasRecDist ? `${parsed.recorded_distance_nm.toFixed(1)} NM` : '--';
        const planDistStr = hasPlanDist ? `${parsed.planned_distance_nm.toFixed(1)} NM` : '--';
        let distVarStr = '--';
        if (parsed.distance_variance_pct != null) {
            distVarStr = `${parsed.distance_variance_pct > 0 ? '+' : ''}${parsed.distance_variance_pct.toFixed(1)}%`;
        } else if (hasRecDist && hasPlanDist && parsed.planned_distance_nm > 0) {
            const pct = ((parsed.recorded_distance_nm - parsed.planned_distance_nm) / parsed.planned_distance_nm) * 100;
            distVarStr = `${pct > 0 ? '+' : ''}${pct.toFixed(1)}%`;
        } else if (parsed.distance_delta_nm != null) {
            distVarStr = `${parsed.distance_delta_nm > 0 ? '+' : ''}${parsed.distance_delta_nm.toFixed(1)} NM`;
        }

        // Time Row: actual planned variance
        const hasRecDur = Boolean(parsed.recorded_duration);
        const hasPlanDur = Boolean(parsed.planned_duration);
        const hasTimeRow = hasRecDur || hasPlanDur;
        const recDurStr = hasRecDur ? esc(parsed.recorded_duration) : '--';
        const planDurStr = hasPlanDur ? esc(parsed.planned_duration) : '--';
        let timeVarStr = '--';
        let timeVarDelta = '';
        if (parsed.duration_variance_pct != null) {
            timeVarStr = `${parsed.duration_variance_pct > 0 ? '+' : ''}${parsed.duration_variance_pct.toFixed(1)}%`;
            if (parsed.duration_delta) timeVarDelta = parsed.duration_delta;
        } else if (hasRecDur && hasPlanDur) {
            const timeVar = calculateTimeVariance(parsed.recorded_duration, parsed.planned_duration);
            if (timeVar) {
                timeVarStr = timeVar.pctStr;
                timeVarDelta = timeVar.deltaStr;
            }
        }

        // Speed Row: avg speed, max speed
        const avgSpdVal = parsed.average_speed_kts ?? parsed.avg_speed_kts;
        const maxSpdVal = parsed.max_speed_kts;
        const hasSpeedRow = avgSpdVal != null || maxSpdVal != null;

        html += `
          <div class="debrief-card ${index > 0 ? 'mt-md' : ''}">
            <div class="flex justify-between align-center debrief-header">
              <h4 class="m-0 flex align-center gap-xs debrief-title">
                <span class="material-symbols-outlined text-brand-medium">flag</span>
                <span class="debrief-title-text">${title}</span>
              </h4>
              <span class="debrief-date text-gray">${new Date().toLocaleDateString()}</span>
            </div>
            ${parsed.planned_track_name ? `
              <div class="debrief-pair-badge">
                <span class="material-symbols-outlined font-xs">compare_arrows</span>
                Compared against plan: <strong>${esc(parsed.planned_track_name)}</strong>
              </div>
            ` : ''}
            <p class="debrief-summary mt-xs mb-sm">${esc(parsed.summary || 'Debrief complete.')}</p>

            <div class="debrief-stat-groups debrief-stat-grid">
              ${hasDistRow ? `
                <div class="debrief-stat-row">
                  <div class="debrief-stat-item">
                    <div class="val rec-dist">${recDistStr}</div>
                    <div class="lbl">Actual Dist</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val plan-dist">${planDistStr}</div>
                    <div class="lbl">Planned Dist</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val var-pct">${distVarStr}</div>
                    <div class="lbl">Variance</div>
                  </div>
                </div>` : ''}

              ${hasTimeRow ? `
                <div class="debrief-stat-row">
                  <div class="debrief-stat-item">
                    <div class="val rec-dur">${recDurStr}</div>
                    <div class="lbl">Actual Time</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val plan-dur">${planDurStr}</div>
                    <div class="lbl">Planned Time</div>
                  </div>
                  <div class="debrief-stat-item">
                    <div class="val time-var-pct var-pct-time" ${timeVarDelta ? `title="${esc(timeVarDelta)}"` : ''}>${timeVarStr}</div>
                    <div class="lbl">Variance</div>
                  </div>
                </div>` : ''}

              ${hasSpeedRow ? `
                <div class="debrief-stat-row debrief-stat-row--speed">
                  ${avgSpdVal != null ? `
                    <div class="debrief-stat-item">
                      <div class="val avg-spd">${avgSpdVal.toFixed(1)} kt</div>
                      <div class="lbl">Avg Speed</div>
                    </div>` : ''}
                  ${maxSpdVal != null ? `
                    <div class="debrief-stat-item">
                      <div class="val max-spd">${maxSpdVal.toFixed(1)} kt</div>
                      <div class="lbl">Max Speed</div>
                    </div>` : ''}
                </div>` : ''}
            </div>

            ${parsed.conclusions ? `
              <div class="debrief-conclusions mt-xs mb-xs">
                <strong>Debrief Conclusions & Takeaways:</strong>
                <p class="m-0 mt-2xs">${esc(parsed.conclusions)}</p>
              </div>
            ` : ''}

            ${parsed.tacking_efficiency ? `
              <p class="debrief-section-text tack-eff mt-xs mb-2xs">
                <strong>Tacking & Maneuvers:</strong> ${esc(parsed.tacking_efficiency)}
              </p>
            ` : ''}
            ${parsed.weather_impact ? `
              <p class="debrief-section-text weather-impact mt-2xs mb-xs">
                <strong>Weather Impact:</strong> ${esc(parsed.weather_impact)}
              </p>
            ` : ''}
            ${obsHtml}
          </div>
        `;
    });

    return html;
}

/**
 * Returns true if the current time (or provided timestamp) is within the active
 * live-tracking window for a voyage: [start_date 00:00:00Z - 24h, end_date 23:59:59.999Z + 24h].
 * Undated voyages return false.
 *
 * @param {object|null|undefined} voyage
 * @param {number} [nowMs=Date.now()]
 * @returns {boolean}
 */
export function isWithinTrackingWindow(voyage, nowMs = Date.now()) {
    if (!voyage || !voyage.start_date || !voyage.end_date) return false;
    const startDay = String(voyage.start_date).split('T')[0];
    const endDay = String(voyage.end_date).split('T')[0];
    const startMs = Date.parse(`${startDay}T00:00:00Z`);
    const endMs = Date.parse(`${endDay}T23:59:59.999Z`);
    if (Number.isNaN(startMs) || Number.isNaN(endMs)) return false;
    const GRACE_MS = 24 * 60 * 60 * 1000;
    return nowMs >= (startMs - GRACE_MS) && nowMs <= (endMs + GRACE_MS);
}

/**
 * Classifies a stop facility type into report category tokens.
 * - Anchorage, Mooring, Fuel Station, Boat Launch -> 'all navigation'
 * - Marina, Yacht Club -> 'all navigation amenities'
 * - Restaurant, Bar, Cafe, Store, Pharmacy, Services (and others) -> 'all amenities'
 *
 * @param {string} type
 * @returns {string}
 */
export function classifyFacilityReportCats(type) {
    const t = String(type || '').toLowerCase().trim();
    if (t.includes('anchor') || t.includes('moor') || t.includes('fuel') || t.includes('launch') || t.includes('ramp')) {
        return 'all navigation';
    }
    if (t.includes('marina') || t.includes('yacht club') || t.includes('yacht')) {
        return 'all navigation amenities';
    }
    return 'all amenities';
}

/**
 * Formats a concise link label for facility URLs ([Google Maps] for Google Maps URLs, [Website] otherwise).
 *
 * @param {string} url
 * @returns {string}
 */
export function formatFacilityLinkLabel(url) {
    const u = String(url || '').toLowerCase();
    if (u.includes('maps.google.') || u.includes('google.com/maps') || u.includes('goo.gl/maps') || u.includes('maps.app.goo.gl')) {
        return '[Google Maps]';
    }
    return '[Website]';
}

/**
 * Builds Google Static Maps `&path=...` parameters for planned and recorded GPX tracks.
 * Downsamples coordinates per track to keep the static map URL within length limits.
 *
 * @param {Array} tracks
 * @param {number} [maxPointsPerTrack=50]
 * @returns {string}
 */
export function buildStaticMapTrackPathsParam(tracks, maxPointsPerTrack = 50) {
    if (!Array.isArray(tracks) || tracks.length === 0) return '';
    let param = '';

    tracks.forEach(track => {
        if (!track) return;
        const rawGeo = track.simplified_geojson || track.geojson;
        if (!rawGeo) return;

        let geoData = rawGeo;
        if (typeof rawGeo === 'string') {
            try { geoData = JSON.parse(rawGeo); } catch (_) { return; }
        }

        const coords = geoData?.geometry?.coordinates || geoData?.coordinates;
        if (!Array.isArray(coords) || coords.length < 2) return;

        // Downsample while preserving start and end points
        let sampled = coords;
        if (coords.length > maxPointsPerTrack && maxPointsPerTrack >= 2) {
            sampled = [];
            const step = (coords.length - 1) / (maxPointsPerTrack - 1);
            for (let i = 0; i < maxPointsPerTrack - 1; i++) {
                sampled.push(coords[Math.round(i * step)]);
            }
            sampled.push(coords[coords.length - 1]);
        }

        // GeoJSON is [lng, lat]; Static Maps path expects lat,lng
        const pointsStr = sampled
            .filter(pt => Array.isArray(pt) && pt.length >= 2 && Number.isFinite(pt[0]) && Number.isFinite(pt[1]))
            .map(pt => `${Number(pt[1]).toFixed(5)},${Number(pt[0]).toFixed(5)}`)
            .join('|');

        if (!pointsStr || !pointsStr.includes('|')) return;

        const isPlanned = track.kind === 'planned';
        const style = isPlanned ? 'color:0x00B4D8d9|weight:3' : 'color:0xFF6B35e6|weight:4';
        param += `&path=${style}|${pointsStr}`;
    });

    return param;
}

/**
 * Classifies a Safety Lookout alert into report category tokens.
 * - navigation -> 'all navigation'
 * - tides      -> 'all navigation weather'
 * - weather/sun -> 'all weather'
 *
 * @param {object} alert
 * @returns {string}
 */
export function classifyAlertReportCat(alert) {
    if (!alert || typeof alert !== 'object') return 'all navigation weather';
    const cat = String(alert.category || alert.type || '').toLowerCase().trim();
    if (cat === 'navigation' || cat === 'nav') return 'all navigation';
    if (cat === 'tides' || cat === 'tide') return 'all navigation weather';
    if (cat === 'weather' || cat === 'sun' || cat === 'wind' || cat === 'waves') return 'all weather';
    if (Array.isArray(alert.travel_table) && alert.travel_table.length > 0) return 'all navigation';
    const hint = `${alert.icon || ''} ${alert.message || ''}`.toLowerCase();
    if (hint.includes('tide') || hint.includes('current') || hint.includes('slack')) return 'all navigation weather';
    if (hint.includes('wind') || hint.includes('wave') || hint.includes('storm') || hint.includes('gale') || hint.includes('sun') || hint.includes('twilight') || hint.includes('rain') || hint.includes('squall')) {
        return 'all weather';
    }
    if (hint.includes('anchor') || hint.includes('channel') || hint.includes('shoal') || hint.includes('reef') || hint.includes('depart') || hint.includes('approach')) {
        return 'all navigation';
    }
    return 'all navigation weather';
}

/**
 * Parses report tab and day filter state from a Location or URL-like object.
 * Preserves backward compatibility for /report/sailing and ?sailing=1 by mapping to tab=navigation.
 *
 * @param {Location|{pathname?: string, search?: string}|string} loc
 * @param {boolean} [legacySailing=false]
 * @returns {{ tab: string, day: string }}
 */
export function parseReportFilterFromLocation(loc, legacySailing = false) {
    let pathname = '';
    let search = '';
    if (typeof loc === 'string') {
        const qIdx = loc.indexOf('?');
        pathname = qIdx >= 0 ? loc.slice(0, qIdx) : loc;
        search = qIdx >= 0 ? loc.slice(qIdx) : '';
    } else if (loc && typeof loc === 'object') {
        pathname = String(loc.pathname || '');
        search = String(loc.search || '');
    }

    const params = new URLSearchParams(search);
    const rawTab = (params.get('tab') || '').toLowerCase().trim();
    const rawDay = (params.get('day') || '').toLowerCase().trim();
    const isSailingPath = legacySailing || pathname.endsWith('/sailing') || params.get('sailing') === '1';

    let tab = 'all';
    if (['all', 'guide', 'navigation', 'weather', 'amenities'].includes(rawTab)) {
        tab = rawTab;
    } else if (rawTab === 'destination-guide' || rawTab === 'destination') {
        tab = 'guide';
    } else if (rawTab === 'sailing' || rawTab === 'nav') {
        tab = 'navigation';
    } else if (rawTab === 'activities' || rawTab === 'shore') {
        tab = 'amenities';
    } else if (isSailingPath) {
        tab = 'navigation';
    }

    let day = 'all';
    if (rawDay && rawDay !== 'all') {
        const parsedDay = parseInt(rawDay, 10);
        if (!Number.isNaN(parsedDay) && parsedDay >= 1) {
            day = String(parsedDay);
        }
    }

    return { tab, day };
}

/**
 * Builds query string for report filter state (?tab=...&day=...).
 * Returns empty string when both tab and day are 'all'.
 *
 * @param {string} tab
 * @param {string|number} day
 * @returns {string}
 */
export function buildReportFilterSearch(tab = 'all', day = 'all') {
    const params = new URLSearchParams();
    if (tab && tab !== 'all') params.set('tab', String(tab));
    if (day && String(day) !== 'all') params.set('day', String(day));
    const qs = params.toString();
    return qs ? `?${qs}` : '';
}

/**
 * Applies category tab and day filter state to a report root container element.
 *
 * @param {Element} root
 * @param {{ tab?: string, day?: string|number }} filter
 * @returns {{ tab: string, day: string }}
 */
export function applyReportFilterToDOM(root, filter = {}) {
    if (!root) return { tab: 'all', day: 'all' };
    const currentTab = root.getAttribute('data-report-tab') || 'all';
    const currentDay = root.getAttribute('data-report-day') || 'all';

    let nextTab = filter.tab != null ? String(filter.tab).toLowerCase().trim() : currentTab;
    if (nextTab === 'destination-guide' || nextTab === 'destination') nextTab = 'guide';
    if (nextTab === 'sailing') nextTab = 'navigation';
    if (nextTab === 'activities') nextTab = 'amenities';
    if (!['all', 'guide', 'navigation', 'weather', 'amenities'].includes(nextTab)) {
        nextTab = 'all';
    }

    let nextDay = filter.day != null ? String(filter.day).toLowerCase().trim() : currentDay;
    if (nextDay !== 'all') {
        const n = parseInt(nextDay, 10);
        nextDay = (!Number.isNaN(n) && n >= 1) ? String(n) : 'all';
    }

    root.setAttribute('data-report-tab', nextTab);
    root.setAttribute('data-report-day', nextDay);
    root.classList.remove('np-report--sailing-only');

    // Update category tab buttons
    root.querySelectorAll('#report-category-tabs [data-tab], .np-report-tabs [data-tab]').forEach(btn => {
        const isMatch = btn.getAttribute('data-tab') === nextTab;
        btn.classList.toggle('active', isMatch);
        btn.setAttribute('aria-selected', isMatch ? 'true' : 'false');
    });

    // Update day pill buttons
    root.querySelectorAll('#report-day-pills [data-day], .np-report-day-pills [data-day]').forEach(pill => {
        const isMatch = pill.getAttribute('data-day') === nextDay;
        pill.classList.toggle('active', isMatch);
        pill.setAttribute('aria-selected', isMatch ? 'true' : 'false');
    });

    // Sync mobile dropdown selects if present
    const catSelect = root.querySelector('#report-category-select, .np-report-category-select');
    if (catSelect && catSelect.value !== nextTab) {
        catSelect.value = nextTab;
    }
    const daySelect = root.querySelector('#report-day-select, .np-report-day-select');
    if (daySelect && daySelect.value !== nextDay) {
        daySelect.value = nextDay;
    }

    // Update stop day visibility
    root.querySelectorAll('.np-report-stop[data-stop-day]').forEach(stopEl => {
        const stopDay = stopEl.getAttribute('data-stop-day');
        const hidden = nextDay !== 'all' && stopDay !== nextDay;
        stopEl.classList.toggle('np-report-stop--hidden-day', hidden);
    });

    // Hide facilities wrapper if zero child facility cards match the active category tab
    root.querySelectorAll('.np-facilities-wrapper').forEach(wrapper => {
        const cards = Array.from(wrapper.querySelectorAll('.np-facility-report-card'));
        const matchingCount = cards.filter(card => {
            if (nextTab === 'all') return true;
            const cats = (card.getAttribute('data-report-cats') || '').split(/\s+/);
            return cats.includes(nextTab);
        }).length;
        wrapper.classList.toggle('np-facilities-wrapper--empty', cards.length > 0 && matchingCount === 0);
    });

    return { tab: nextTab, day: nextDay };
}

/**
 * groupColocatedStops — groups landfall stops that sit at the same harbor
 * (within `thresholdNM` nautical miles of an earlier landfall stop) so they
 * share a single combined map pin and a single combined facility count badge.
 *
 * @param {Array<Object>} sortedStops - Stops sorted chronologically
 * @param {Array<string>} [accents]   - Palette token names (e.g. VOYAGE_ACCENTS)
 * @param {number} [thresholdNM=2.0]  - Max distance in NM to merge stops into one group
 * @returns {{ groups: Array<Object>, stopPositionMap: Map<any, {lat: number, lng: number}> }}
 */
export function groupColocatedStops(sortedStops, accents = ['coral', 'teal', 'violet', 'amber', 'sky', 'green'], thresholdNM = 2.0) {
    const groups = [];
    const stopPositionMap = new Map();
    if (!Array.isArray(sortedStops) || sortedStops.length === 0) {
        return { groups, stopPositionMap };
    }

    let landfallIndex = 0;
    for (const stop of sortedStops) {
        if (!stop || !stop.latitude || !stop.longitude) continue;
        if (stop.stop_type === 'passage_point') continue;

        const stopNumber = ++landfallIndex;
        const accent = accents[(stopNumber - 1) % accents.length] || 'coral';

        let matchedGroup = null;
        for (const g of groups) {
            const dNM = nmBetween(g.position, stop);
            if (dNM != null && dNM <= thresholdNM) {
                matchedGroup = g;
                break;
            }
        }

        if (matchedGroup) {
            matchedGroup.items.push({ stop, stopNumber, accent });
            if (stop.id != null) {
                stopPositionMap.set(stop.id, matchedGroup.position);
            }
        } else {
            const position = { lat: stop.latitude, lng: stop.longitude };
            const newGroup = {
                position,
                primaryStop: stop,
                items: [{ stop, stopNumber, accent }],
            };
            groups.push(newGroup);
            if (stop.id != null) {
                stopPositionMap.set(stop.id, position);
            }
        }
    }

    return { groups, stopPositionMap };
}

/**
 * findNearbyJoinTargetStop — checks whether a map click at (`clickedLat`, `clickedLng`)
 * for `selectedDate` is within `thresholdNM` of an existing landfall stop on a
 * different date (prioritizing Stop 1, the start stop), so the UI can ask the user
 * if they want to join the start and stop at the exact same coordinates.
 *
 * @param {Array<Object>} stops
 * @param {number} clickedLat
 * @param {number} clickedLng
 * @param {string} selectedDate - "YYYY-MM-DD"
 * @param {number} [thresholdNM=2.0]
 * @returns {{ targetStop: Object, stopNumber: number, isStartStop: boolean, distanceNM: number } | null}
 */
export function findNearbyJoinTargetStop(stops, clickedLat, clickedLng, selectedDate, thresholdNM = 2.0) {
    if (!Array.isArray(stops) || stops.length === 0 || typeof clickedLat !== 'number' || typeof clickedLng !== 'number') {
        return null;
    }
    const sorted = [...stops].sort((a, b) => new Date(a.target_date) - new Date(b.target_date));
    const candidates = [];
    let landfallIndex = 0;

    for (const s of sorted) {
        if (!s || !s.latitude || !s.longitude || s.stop_type === 'passage_point') continue;
        const stopNumber = ++landfallIndex;
        const stopDate = String(s.target_date || '').split('T')[0];
        if (selectedDate && stopDate === selectedDate) continue;

        const dNM = nmBetween(s, { lat: clickedLat, lng: clickedLng });
        if (dNM != null && dNM <= thresholdNM) {
            candidates.push({
                targetStop: s,
                stopNumber,
                isStartStop: stopNumber === 1,
                distanceNM: dNM,
            });
        }
    }

    if (candidates.length === 0) return null;
    // Prefer joining with Stop 1 (Start) when within threshold, otherwise closest stop
    const startMatch = candidates.find(c => c.isStartStop);
    if (startMatch) return startMatch;
    candidates.sort((a, b) => a.distanceNM - b.distanceNM);
    return candidates[0];
}



