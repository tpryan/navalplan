// Pure utility helpers — no state, no DOM beyond the a11y announcer.

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

