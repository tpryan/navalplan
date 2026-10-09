import { getDebriefStopTitle, generateDebriefsHTML, calculateTimeVariance, parseDurationToMinutes } from '../js/utils.js';

describe('GPX Track UI & Rendering', () => {
  let container;
  let countBadge;
  let debriefContainer;

  beforeEach(() => {
    document.body.innerHTML = `
      <div id="track-layer-controls" class="hidden">
        <button id="btn-toggle-planned-tracks" class="track-layer-btn active">Planned</button>
        <button id="btn-toggle-recorded-tracks" class="track-layer-btn active">Recorded</button>
      </div>
      <div id="track-hover-hud" class="track-hover-hud hidden"></div>
      <div id="tracks-count-badge"></div>
      <button id="btn-debrief-all-tracks" class="hidden">Debrief All</button>
      <div id="tracks-list-container"></div>
      <div id="debrief-container" class="hidden"></div>
    `;

    container = document.getElementById('tracks-list-container');
    countBadge = document.getElementById('tracks-count-badge');
    debriefContainer = document.getElementById('debrief-container');
  });

  afterEach(() => {
    document.body.innerHTML = '';
  });

  function escapeTrackHtml(str) {
    if (str == null) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function renderTracksList(tracks) {
    const debriefAllBtn = document.getElementById('btn-debrief-all-tracks');
    if (!tracks || tracks.length === 0) {
      container.innerHTML = '<p class="font-sm text-gray">No tracks uploaded yet for this voyage.</p>';
      if (countBadge) countBadge.textContent = '0 tracks';
      if (debriefAllBtn) debriefAllBtn.classList.add('hidden');
      return;
    }

    if (countBadge) countBadge.textContent = `${tracks.length} track${tracks.length === 1 ? '' : 's'}`;
    const hasDebriefable = tracks.some(t => t.kind !== 'planned');
    if (debriefAllBtn) {
      if (hasDebriefable) {
        debriefAllBtn.classList.remove('hidden');
      } else {
        debriefAllBtn.classList.add('hidden');
      }
    }

    let html = '<div class="flex flex-col gap-sm">';
    tracks.forEach(t => {
      const isPlanned = t.kind === 'planned';
      const badgeClass = isPlanned ? 'track-badge-planned' : 'track-badge-recorded';
      const distStr = t.distance_nm != null ? `${t.distance_nm.toFixed(1)} NM` : '--';
      const durationStr = t.duration_interval || '--';
      const avgSpeedStr = t.avg_speed_kts != null ? `${t.avg_speed_kts.toFixed(1)} kts` : '--';
      const maxSpeedStr = t.max_speed_kts != null ? `${t.max_speed_kts.toFixed(1)} kts` : '--';

      html += `
        <div class="card p-sm border flex flex-col gap-xs track-item" data-track-id="${t.id}">
          <div class="flex justify-between align-center">
            <div class="flex align-center gap-xs">
              <span class="track-badge ${badgeClass}">${t.kind}</span>
              <strong>${escapeTrackHtml(t.name || 'Unnamed Track')}</strong>
            </div>
            <div class="flex gap-xs">
              <button class="btn-icon p-xs text-danger btn-track-delete" data-track-id="${t.id}">Delete</button>
            </div>
          </div>
          <div class="font-xs text-gray flex gap-md">
            <span class="track-dist">Distance: <strong>${distStr}</strong></span>
            <span class="track-dur">Duration: <strong>${durationStr}</strong></span>
            ${!isPlanned ? `<span class="track-sog">Avg SOG: <strong>${avgSpeedStr}</strong></span>` : ''}
          </div>
        </div>
      `;
    });
    html += '</div>';
    container.innerHTML = html;
  }

  function renderDebriefCard(debrief, stops = null) {
    if (!debrief || (Array.isArray(debrief) && debrief.length === 0)) {
      debriefContainer.classList.add('hidden');
      return;
    }
    const html = generateDebriefsHTML(debrief, stops);
    if (!html) {
      debriefContainer.classList.add('hidden');
      return;
    }
    debriefContainer.innerHTML = html;
    debriefContainer.classList.remove('hidden');
  }

  describe('Tracks List', () => {
    it('shows empty state when no tracks exist', () => {
      renderTracksList([]);
      expect(container.textContent).toContain('No tracks uploaded yet');
      expect(countBadge.textContent).toBe('0 tracks');
    });

    it('renders planned and recorded tracks with badges and metrics', () => {
      const tracks = [
        {
          id: 'track-1',
          name: 'Planned Route <Leg 1>',
          kind: 'planned',
          distance_nm: 18.25,
          duration_interval: '3h 30m'
        },
        {
          id: 'track-2',
          name: 'Recorded Actual Track',
          kind: 'recorded',
          distance_nm: 21.4,
          duration_interval: '4h 12m',
          avg_speed_kts: 5.1,
          max_speed_kts: 7.8
        }
      ];

      renderTracksList(tracks);

      expect(countBadge.textContent).toBe('2 tracks');
      const items = container.querySelectorAll('.track-item');
      expect(items.length).toBe(2);

      // Check planned track
      expect(items[0].querySelector('.track-badge-planned')).not.toBeNull();
      expect(items[0].innerHTML).toContain('Planned Route &lt;Leg 1&gt;');
      expect(items[0].querySelector('.track-dist').textContent).toContain('18.3 NM');
      expect(items[0].querySelector('.btn-track-debrief')).toBeNull();

      // Check recorded track
      expect(items[1].querySelector('.track-badge-recorded')).not.toBeNull();
      expect(items[1].querySelector('.track-sog').textContent).toContain('5.1 kts');
      expect(items[1].querySelector('.btn-track-debrief')).toBeNull();
      expect(container.querySelectorAll('.btn-track-debrief').length).toBe(0);
    });
  });

  describe('Debrief Presentation', () => {
    it('hides debrief container when debrief is null', () => {
      renderDebriefCard(null);
      expect(debriefContainer.classList.contains('hidden')).toBe(true);
    });

    it('renders planned track pairing badge and tactical conclusions', () => {
      const debrief = {
        track_name: 'Leg 1 Actual',
        planned_track_name: 'Leg 1 Planned Route',
        recorded_distance_nm: 12.5,
        planned_distance_nm: 10.0,
        distance_variance_pct: 25.0,
        conclusions: 'Actual track exceeded planned route by 2.5 NM (+25.0%) due to upwind tacking. Passage was safely completed.'
      };

      renderDebriefCard(debrief);

      expect(debriefContainer.querySelector('.debrief-pair-badge').textContent).toContain('Leg 1 Planned Route');
      expect(debriefContainer.querySelector('.debrief-conclusions').textContent).toContain('Actual track exceeded planned route');
    });

    it('renders debrief summary, variance metrics, and observations', () => {
      const debrief = {
        summary: 'Solid downwind passage with low tacking overhead.',
        recorded_distance_nm: 25.2,
        planned_distance_nm: 22.0,
        distance_variance_pct: 14.5,
        average_speed_kts: 5.9,
        tacking_efficiency: 'Minimal jibes required heading south.',
        observations: [
          'Strong ebb tide accelerated arrival by 40 minutes',
          'Shallow bar crossed 1 hour before low water safely'
        ]
      };

      renderDebriefCard(debrief);

      expect(debriefContainer.classList.contains('hidden')).toBe(false);
      expect(debriefContainer.querySelector('.debrief-summary').textContent).toContain('Solid downwind passage');
      expect(debriefContainer.querySelector('.var-pct').textContent).toContain('14.5%');
      expect(debriefContainer.querySelector('.rec-dist').textContent).toBe('25.2 NM');
      expect(debriefContainer.querySelector('.tack-eff').textContent).toContain('Minimal jibes');

      const lis = debriefContainer.querySelectorAll('li');
      expect(lis.length).toBe(2);
      expect(lis[0].textContent).toContain('Strong ebb tide');
    });

    it('groups metrics into distance row (actual, planned, variance) and time row (actual, planned, variance)', () => {
      const debrief = {
        summary: 'Crossed channel with favorable wind.',
        recorded_distance_nm: 25.2,
        planned_distance_nm: 22.0,
        distance_variance_pct: 14.5,
        recorded_duration: '05:18:06',
        planned_duration: '03h 48m',
        average_speed_kts: 5.9,
        max_speed_kts: 7.8
      };

      renderDebriefCard(debrief);

      const statRows = debriefContainer.querySelectorAll('.debrief-stat-row');
      expect(statRows.length).toBe(3); // Distance row, Time row, Speed row

      // Distance Row
      const distRow = statRows[0];
      const distLabels = Array.from(distRow.querySelectorAll('.lbl')).map(el => el.textContent);
      expect(distLabels).toEqual(['Actual Dist', 'Planned Dist', 'Variance']);
      expect(distRow.querySelector('.rec-dist').textContent).toBe('25.2 NM');
      expect(distRow.querySelector('.plan-dist').textContent).toBe('22.0 NM');
      expect(distRow.querySelector('.var-pct').textContent).toBe('+14.5%');

      // Time Row
      const timeRow = statRows[1];
      const timeLabels = Array.from(timeRow.querySelectorAll('.lbl')).map(el => el.textContent);
      expect(timeLabels).toEqual(['Actual Time', 'Planned Time', 'Variance']);
      expect(timeRow.querySelector('.rec-dur').textContent).toBe('05:18:06');
      expect(timeRow.querySelector('.plan-dur').textContent).toBe('03h 48m');
      expect(timeRow.querySelector('.time-var-pct').textContent).toBe('+39.5%');
      expect(timeRow.querySelector('.time-var-pct').getAttribute('title')).toContain('+1h 30m');

      // Speed Row
      const speedRow = statRows[2];
      expect(speedRow.classList.contains('debrief-stat-row--speed')).toBe(true);
      expect(speedRow.querySelector('.avg-spd').textContent).toBe('5.9 kt');
      expect(speedRow.querySelector('.max-spd').textContent).toBe('7.8 kt');
    });

    it('correctly calculates duration variance between recorded and planned time', () => {
      expect(parseDurationToMinutes('05:18:06')).toBeCloseTo(318.1, 1);
      expect(parseDurationToMinutes('05:18')).toBe(318);
      expect(parseDurationToMinutes('03h 48m')).toBe(228);
      expect(parseDurationToMinutes('45m')).toBe(45);
      expect(parseDurationToMinutes('2.5h')).toBe(150);
      expect(parseDurationToMinutes(null)).toBeNull();

      const faster = calculateTimeVariance('03:00:00', '04h 00m');
      expect(faster.pct).toBeCloseTo(-25.0, 1);
      expect(faster.pctStr).toBe('-25.0%');
      expect(faster.deltaStr).toBe('-1h');

      const slower = calculateTimeVariance('05:18:06', '03h 48m');
      expect(slower.pct).toBeCloseTo(39.5, 1);
      expect(slower.pctStr).toBe('+39.5%');
      expect(slower.deltaStr).toBe('+1h 30m');
    });

    it('renders multiple debrief cards when given an array of debriefs', () => {
      const debriefs = [
        { track_id: 't1', track_name: 'Leg 1', summary: 'First leg complete', recorded_distance_nm: 10.0 },
        { track_id: 't2', track_name: 'Leg 2', summary: 'Second leg complete', recorded_distance_nm: 15.0 },
      ];
      renderDebriefCard(debriefs);
      expect(debriefContainer.classList.contains('hidden')).toBe(false);
      const cards = debriefContainer.querySelectorAll('.debrief-card');
      expect(cards.length).toBe(2);
      expect(cards[0].textContent).toContain('Leg 1');
      expect(cards[1].textContent).toContain('Leg 2');
    });

    it('matches debrief title in UI to stops instead of track title from uploaded tracks', () => {
      const stops = [
        { id: 10, location_name: '82GQ+6Q Marina del Rey, CA' },
        { id: 11, location_name: 'Isthmus Cove, Santa Catalina Island' },
        { id: 12, location_name: 'Avalon Harbor' },
      ];
      const debrief = {
        track_id: 'rec-1',
        track_name: '2024-08-12 14:23:10.gpx',
        start_stop_id: 10,
        summary: 'Smooth passage to Isthmus Cove',
        recorded_distance_nm: 31.4,
      };

      renderDebriefCard(debrief, stops);
      const card = debriefContainer.querySelector('.debrief-card');
      const title = (card.querySelector('.debrief-title-text') || card.querySelector('h4')).textContent.trim();

      expect(title).toBe('Passage Debrief: Marina del Rey, CA to Isthmus Cove, Santa Catalina Island');
      expect(title).not.toContain('2024-08-12 14:23:10.gpx');
    });

    it('uses debrief stop_title directly when provided by backend', () => {
      const debrief = {
        track_id: 'rec-2',
        track_name: 'Track_001.gpx',
        stop_title: 'Cowes to Newtown River',
        summary: 'Quiet anchorage reach',
      };

      renderDebriefCard(debrief);
      const card = debriefContainer.querySelector('.debrief-card');
      const title = (card.querySelector('.debrief-title-text') || card.querySelector('h4')).textContent.trim();

      expect(title).toBe('Passage Debrief: Cowes to Newtown River');
      expect(title).not.toContain('Track_001.gpx');
    });

    it('toggles single debrief button visibility based on debriefable track presence', () => {
      const debriefAllBtn = document.getElementById('btn-debrief-all-tracks');
      renderTracksList([]);
      expect(debriefAllBtn.classList.contains('hidden')).toBe(true);

      renderTracksList([{ id: 't1', kind: 'planned', name: 'Plan only' }]);
      expect(debriefAllBtn.classList.contains('hidden')).toBe(true);

      renderTracksList([{ id: 't1', kind: 'recorded', name: 'Leg 1' }]);
      expect(debriefAllBtn.classList.contains('hidden')).toBe(false);
    });
  });

  describe('Layer Toggling', () => {
    it('toggles planned and recorded track visibility state', () => {
      let showPlanned = true;
      let showRecorded = true;

      const toggleLayer = (kind) => {
        if (kind === 'planned') {
          showPlanned = !showPlanned;
          document.getElementById('btn-toggle-planned-tracks').classList.toggle('active', showPlanned);
        } else if (kind === 'recorded') {
          showRecorded = !showRecorded;
          document.getElementById('btn-toggle-recorded-tracks').classList.toggle('active', showRecorded);
        }
      };

      const btnPlanned = document.getElementById('btn-toggle-planned-tracks');
      const btnRecorded = document.getElementById('btn-toggle-recorded-tracks');

      expect(btnPlanned.classList.contains('active')).toBe(true);
      toggleLayer('planned');
      expect(showPlanned).toBe(false);
      expect(btnPlanned.classList.contains('active')).toBe(false);

      toggleLayer('planned');
      expect(showPlanned).toBe(true);
      expect(btnPlanned.classList.contains('active')).toBe(true);

      toggleLayer('recorded');
      expect(showRecorded).toBe(false);
      expect(btnRecorded.classList.contains('active')).toBe(false);
    });
  });

  describe('Voyage Report Debrief Integration', () => {
    function generateDebriefsHTML(debrief, stops = null) {
      if (!debrief || (Array.isArray(debrief) && debrief.length === 0)) {
        return '';
      }
      const debriefs = Array.isArray(debrief) ? debrief : [debrief];
      let html = '';
      debriefs.forEach((item) => {
        let parsed = item;
        if (typeof item === 'string') {
          try { parsed = JSON.parse(item); } catch (e) { parsed = { summary: item }; }
        }
        if (!parsed) return;
        const stopTitle = getDebriefStopTitle(parsed, stops);
        const title = stopTitle
          ? `Passage Debrief: ${escapeTrackHtml(stopTitle)}`
          : (parsed.track_name
            ? `Passage Debrief: ${escapeTrackHtml(parsed.track_name)}`
            : 'Passage Debrief Analysis');
        html += `
          <div class="debrief-card">
            <h4>${title}</h4>
            <p class="debrief-summary">${escapeTrackHtml(parsed.summary || 'Debrief complete.')}</p>
          </div>
        `;
      });
      return html;
    }

    function renderReportDebriefSection(debriefs, stops = null) {
      if (!Array.isArray(debriefs) || debriefs.length === 0) return '';
      const debriefContent = generateDebriefsHTML(debriefs, stops);
      if (!debriefContent) return '';
      return `
        <div class="np-report-section np-report-debriefs">
          <span class="np-section-label">Passage Tactical Debrief</span>
          ${debriefContent}
        </div>
      `;
    }

    it('returns empty string when no debriefs exist', () => {
      expect(renderReportDebriefSection([])).toBe('');
      expect(renderReportDebriefSection(null)).toBe('');
    });

    it('renders tactical debrief section with header and cards when debriefs are provided', () => {
      const debriefs = [
        { track_id: 't1', track_name: 'Leg 1: Harbor to Point', summary: 'Excellent reaching leg' },
        { track_id: 't2', track_name: 'Leg 2: Offshore run', summary: 'Good downwind sailing' },
      ];
      const html = renderReportDebriefSection(debriefs);
      expect(html).toContain('Passage Tactical Debrief');
      expect(html).toContain('Leg 1: Harbor to Point');
      expect(html).toContain('Leg 2: Offshore run');
      expect(html).toContain('np-report-debriefs');
    });

    it('renders debrief titles matching stops rather than raw uploaded track filenames', () => {
      const stops = [
        { id: 1, location_name: 'Cowes' },
        { id: 2, location_name: 'Yarmouth' },
      ];
      const debriefs = [
        { track_id: 't1', track_name: 'activity_987654.gpx', start_stop_id: 1, summary: 'Reaching along the Solent' },
      ];
      const html = renderReportDebriefSection(debriefs, stops);
      expect(html).toContain('Passage Debrief: Cowes to Yarmouth');
      expect(html).not.toContain('Passage Debrief: activity_987654.gpx');
    });

    function simulateReportStopDebriefs(stops, debriefs) {
      const allDebriefs = (Array.isArray(debriefs) ? debriefs : []).filter(Boolean);
      const stopDebriefsMap = new Map();
      const assignedDebriefs = new Set();

      // Pass 1: Leg number in track_name or planned_track_name
      allDebriefs.forEach(d => {
        if (assignedDebriefs.has(d)) return;
        const nameToTest = `${d.track_name || ''} ${d.planned_track_name || ''}`;
        const match = nameToTest.match(/\bLeg\s*#?\s*(\d+)\b/i);
        if (match) {
          const legNum = parseInt(match[1], 10);
          const sIdx = legNum - 1;
          if (sIdx >= 0 && sIdx < stops.length) {
            if (!stopDebriefsMap.has(sIdx)) stopDebriefsMap.set(sIdx, []);
            stopDebriefsMap.get(sIdx).push(d);
            assignedDebriefs.add(d);
          }
        }
      });

      // Pass 2: Starting stop location name in track name
      allDebriefs.forEach(d => {
        if (assignedDebriefs.has(d)) return;
        const nameToTest = `${d.track_name || ''} ${d.planned_track_name || ''}`.toLowerCase();
        for (let i = 0; i < stops.length - 1; i++) {
          const locName = (stops[i].location_name || stops[i].name || '').toLowerCase();
          if (locName && nameToTest.includes(locName)) {
            if (!stopDebriefsMap.has(i)) stopDebriefsMap.set(i, []);
            stopDebriefsMap.get(i).push(d);
            assignedDebriefs.add(d);
            break;
          }
        }
      });

      // Pass 3: Explicit match by start_stop_id
      allDebriefs.forEach(d => {
        if (assignedDebriefs.has(d) || d.start_stop_id == null) return;
        let sIdx = stops.findIndex(s => String(s.id) === String(d.start_stop_id));
        if (sIdx >= 0) {
          if (sIdx === stops.length - 1 && stops.length > 1) {
            sIdx = sIdx - 1;
          }
          if (!stopDebriefsMap.has(sIdx)) stopDebriefsMap.set(sIdx, []);
          stopDebriefsMap.get(sIdx).push(d);
          assignedDebriefs.add(d);
        }
      });

      // Pass 4: voyage_stop_id if set
      allDebriefs.forEach(d => {
        if (assignedDebriefs.has(d) || d.voyage_stop_id == null) return;
        let sIdx = stops.findIndex(s => String(s.id) === String(d.voyage_stop_id));
        if (sIdx > 0 && (sIdx === stops.length - 1 || !stopDebriefsMap.has(sIdx - 1))) {
          sIdx = sIdx - 1;
        }
        if (sIdx >= 0 && sIdx < stops.length) {
          if (!stopDebriefsMap.has(sIdx)) stopDebriefsMap.set(sIdx, []);
          stopDebriefsMap.get(sIdx).push(d);
          assignedDebriefs.add(d);
        }
      });

      // Pass 5: Sequential fallback for unassigned debriefs
      let nextStopIdx = 0;
      allDebriefs.forEach(d => {
        if (assignedDebriefs.has(d)) return;
        while (nextStopIdx < stops.length - 1 && stopDebriefsMap.has(nextStopIdx)) {
          nextStopIdx++;
        }
        const targetIdx = nextStopIdx < stops.length - 1 ? nextStopIdx : Math.min(nextStopIdx, stops.length - 1);
        if (!stopDebriefsMap.has(targetIdx)) stopDebriefsMap.set(targetIdx, []);
        stopDebriefsMap.get(targetIdx).push(d);
        assignedDebriefs.add(d);
        nextStopIdx++;
      });

      const result = [];
      stops.forEach((stop, idx) => {
        const legNum = idx + 1;
        const stopDebriefs = stopDebriefsMap.get(idx) || [];
        let stopHtml = `<div class="np-report-stop" data-stop-id="${stop.id}"><h3>Stop ${legNum}: ${stop.name}</h3>`;
        if (stopDebriefs.length > 0) {
          const debriefTitle = (idx < stops.length - 1)
            ? `Passage Tactical Debrief — Leg ${legNum}`
            : `Passage Tactical Debrief`;
          stopHtml += `
            <div class="np-report-stop-debrief">
              <span class="np-facilities-header">${debriefTitle}</span>
              ${generateDebriefsHTML(stopDebriefs)}
            </div>
          `;
        }
        stopHtml += `</div>`;
        result.push(stopHtml);
      });

      return {
        stopsHtml: result.join(''),
        remainingHtml: '', // Consolidated bottom section is completely eliminated
      };
    }

    it('places leg debriefing inside starting stop container for each leg', () => {
      const stops = [
        { id: 101, name: 'Marina del Rey' },
        { id: 102, name: 'Isthmus Cove' },
        { id: 103, name: 'Avalon Harbor' },
      ];
      const debriefs = [
        { track_id: 't1', track_name: 'Leg 1 Actual', start_stop_id: 101, summary: 'Smooth crossing to Isthmus' },
        { track_id: 't2', track_name: 'Leg 2 Actual', start_stop_id: 102, summary: 'Coastal reach to Avalon' },
      ];

      const { stopsHtml, remainingHtml } = simulateReportStopDebriefs(stops, debriefs);

      expect(stopsHtml).toContain('Passage Tactical Debrief — Leg 1');
      expect(stopsHtml).toContain('Smooth crossing to Isthmus');
      expect(stopsHtml).toContain('Passage Tactical Debrief — Leg 2');
      expect(stopsHtml).toContain('Coastal reach to Avalon');
      expect(remainingHtml).toBe('');
      expect(stopsHtml).not.toContain('np-report-item--non-sailing');
    });

    it('places debriefs with starting stop even if legacy debrief was stored with ending stop', () => {
      const stops = [
        { id: 101, name: 'Cowes' },
        { id: 102, name: 'Newtown' },
        { id: 103, name: 'Yarmouth' },
      ];
      // Leg 1 ending stop is Newtown (102), Leg 2 ending stop is Yarmouth (103)
      const debriefs = [
        { track_id: 't1', track_name: 'Full Voyage - Leg 1: Cowes to Newtown', voyage_stop_id: 102, summary: 'First leg crossing' },
        { track_id: 't2', track_name: 'Full Voyage - Leg 2: Newtown to Yarmouth', voyage_stop_id: 103, summary: 'Second leg run' },
      ];

      const { stopsHtml } = simulateReportStopDebriefs(stops, debriefs);

      // Stop 1 (Cowes) should receive Leg 1 debrief
      expect(stopsHtml).toMatch(/Stop 1: Cowes[\s\S]*?Passage Tactical Debrief — Leg 1[\s\S]*?First leg crossing/);
      // Stop 2 (Newtown) should receive Leg 2 debrief
      expect(stopsHtml).toMatch(/Stop 2: Newtown[\s\S]*?Passage Tactical Debrief — Leg 2[\s\S]*?Second leg run/);
      // Stop 3 (Yarmouth) is the final arrival and should have NO debrief
      expect(stopsHtml).not.toMatch(/Stop 3: Yarmouth[\s\S]*?Passage Tactical Debrief/);
    });

    it('assigns unassigned debriefs to starting stops without creating a consolidated report at the end', () => {
      const stops = [
        { id: 101, name: 'Marina del Rey' },
        { id: 102, name: 'Isthmus Cove' },
        { id: 103, name: 'Avalon Harbor' },
      ];
      const debriefs = [
        { track_id: 't1', track_name: 'Leg 1 Actual', start_stop_id: 101, summary: 'Leg 1 debrief' },
        { track_id: 't_unassigned', track_name: 'Overall Voyage Track', start_stop_id: null, voyage_stop_id: null, summary: 'Master voyage passage' },
      ];

      const { stopsHtml, remainingHtml } = simulateReportStopDebriefs(stops, debriefs);

      expect(stopsHtml).toContain('Passage Tactical Debrief — Leg 1');
      expect(stopsHtml).toContain('Leg 1 debrief');
      expect(stopsHtml).toContain('Passage Tactical Debrief — Leg 2');
      expect(stopsHtml).toContain('Master voyage passage');
      expect(remainingHtml).toBe('');
    });
  });

  describe('Direct Stop Lines Suppression when Planned Routes Exist', () => {
    function nmBetween(p1, p2) {
      if (!p1 || !p2 || p1.latitude == null || p2.latitude == null) return null;
      const R = 3440.065;
      const dLat = (p2.latitude - p1.latitude) * Math.PI / 180;
      const dLon = (p2.longitude - p1.longitude) * Math.PI / 180;
      const a = Math.sin(dLat / 2) * Math.sin(dLat / 2) +
                Math.cos(p1.latitude * Math.PI / 180) * Math.cos(p2.latitude * Math.PI / 180) *
                Math.sin(dLon / 2) * Math.sin(dLon / 2);
      return 2 * R * Math.asin(Math.sqrt(a));
    }

    function voyageHasOverallPlannedRoute(tracks, stops) {
      if (!tracks || !Array.isArray(tracks) || tracks.length === 0) return false;
      const plannedTracks = tracks.filter(t => t && t.kind === 'planned');
      if (plannedTracks.length === 0) return false;

      if (!stops || stops.length <= 2) {
        return plannedTracks.length > 0;
      }

      for (const t of plannedTracks) {
        if (!t.voyage_stop_id) {
          return true;
        }

        const rawGeo = t.simplified_geojson || t.geojson;
        if (!rawGeo) continue;
        let geoData = rawGeo;
        if (typeof rawGeo === 'string') {
          try { geoData = JSON.parse(rawGeo); } catch (e) { continue; }
        }
        const coords = geoData?.geometry?.coordinates;
        if (coords && Array.isArray(coords) && coords.length >= 2) {
          const firstStop = stops[0];
          const lastStop = stops[stops.length - 1];
          const startPt = { latitude: coords[0][1], longitude: coords[0][0] };
          const endPt = { latitude: coords[coords.length - 1][1], longitude: coords[coords.length - 1][0] };
          const dStart = nmBetween(firstStop, startPt);
          const dEnd = nmBetween(lastStop, endPt);
          if (dStart !== null && dStart < 15 && dEnd !== null && dEnd < 15) {
            return true;
          }
        }
      }

      const totalLegs = stops.length - 1;
      let legsWithPlanned = 0;
      for (let i = 0; i < totalLegs; i++) {
        if (hasPlannedRouteForLeg(stops[i], stops[i + 1], i, stops, tracks, true)) {
          legsWithPlanned++;
        }
      }
      return legsWithPlanned === totalLegs;
    }

    function hasPlannedRouteForLeg(fromStop, toStop, legIdx, stops, tracks, skipOverallCheck = false) {
      if (!tracks || !Array.isArray(tracks) || tracks.length === 0) return false;
      const plannedTracks = tracks.filter(t => t && t.kind === 'planned');
      if (plannedTracks.length === 0) return false;

      if (!skipOverallCheck && voyageHasOverallPlannedRoute(tracks, stops)) {
        return true;
      }

      const legNum = legIdx + 1;

      for (const t of plannedTracks) {
        if (t.voyage_stop_id != null && (t.voyage_stop_id === fromStop.id || t.voyage_stop_id === toStop.id)) {
          return true;
        }

        if (t.name && new RegExp(`\\bLeg\\s*${legNum}\\b`, 'i').test(t.name)) {
          return true;
        }

        const rawGeo = t.simplified_geojson || t.geojson;
        if (rawGeo) {
          let geoData = rawGeo;
          if (typeof rawGeo === 'string') {
            try { geoData = JSON.parse(rawGeo); } catch (e) { continue; }
          }
          const coords = geoData?.geometry?.coordinates;
          if (coords && Array.isArray(coords) && coords.length >= 2) {
            const startPt = { latitude: coords[0][1], longitude: coords[0][0] };
            const endPt = { latitude: coords[coords.length - 1][1], longitude: coords[coords.length - 1][0] };
            const dStart = nmBetween(fromStop, startPt);
            const dEnd = nmBetween(toStop, endPt);
            if (dStart !== null && dStart < 10 && dEnd !== null && dEnd < 10) {
              return true;
            }
          }
        }
      }

      if (stops && plannedTracks.length === stops.length - 1 && plannedTracks[legIdx]) {
        return true;
      }

      return false;
    }

    function getDrawnRouteSegments(stops, tracks) {
      if (!stops || stops.length < 2) return [];
      if (voyageHasOverallPlannedRoute(tracks, stops)) return [];

      const segments = [];
      for (let i = 0; i < stops.length - 1; i++) {
        const from = stops[i];
        const to = stops[i + 1];
        if (!hasPlannedRouteForLeg(from, to, i, stops, tracks)) {
          segments.push({ leg: i + 1, from: from.id, to: to.id });
        }
      }
      return segments;
    }

    it('draws all direct lines when no planned tracks exist', () => {
      const stops = [
        { id: 1, name: 'San Francisco', latitude: 37.8, longitude: -122.4 },
        { id: 2, name: 'Half Moon Bay', latitude: 37.5, longitude: -122.5 },
        { id: 3, name: 'Santa Cruz', latitude: 36.9, longitude: -122.0 },
      ];
      const tracks = [
        { id: 'trk-1', kind: 'recorded', name: 'Actual GPS Log' }
      ];

      const segments = getDrawnRouteSegments(stops, tracks);
      expect(segments.length).toBe(2);
      expect(segments[0].leg).toBe(1);
      expect(segments[1].leg).toBe(2);
    });

    it('suppresses all direct lines when an overall planned route covers the voyage', () => {
      const stops = [
        { id: 1, name: 'San Francisco', latitude: 37.8, longitude: -122.4 },
        { id: 2, name: 'Half Moon Bay', latitude: 37.5, longitude: -122.5 },
        { id: 3, name: 'Santa Cruz', latitude: 36.9, longitude: -122.0 },
      ];
      const tracks = [
        {
          id: 'plan-1',
          kind: 'planned',
          name: 'SF to Santa Cruz Planned Route',
          voyage_stop_id: null,
        }
      ];

      const segments = getDrawnRouteSegments(stops, tracks);
      expect(segments.length).toBe(0);
    });

    it('suppresses only the direct line for a leg that has a planned route', () => {
      const stops = [
        { id: 1, name: 'San Francisco', latitude: 37.8, longitude: -122.4 },
        { id: 2, name: 'Half Moon Bay', latitude: 37.5, longitude: -122.5 },
        { id: 3, name: 'Santa Cruz', latitude: 36.9, longitude: -122.0 },
      ];
      const tracks = [
        {
          id: 'plan-leg1',
          kind: 'planned',
          name: 'Leg 1 Plan',
          voyage_stop_id: 1,
        }
      ];

      const segments = getDrawnRouteSegments(stops, tracks);
      expect(segments.length).toBe(1);
      expect(segments[0].leg).toBe(2);
      expect(segments[0].from).toBe(2);
      expect(segments[0].to).toBe(3);
    });

    it('suppresses direct line for a 2-stop voyage when any planned track exists', () => {
      const stops = [
        { id: 10, name: 'Miami', latitude: 25.7, longitude: -80.1 },
        { id: 20, name: 'Bimini', latitude: 25.7, longitude: -79.3 },
      ];
      const tracks = [
        { id: 'plan-bimini', kind: 'planned', name: 'Bimini Crossing Route' }
      ];

      const segments = getDrawnRouteSegments(stops, tracks);
      expect(segments.length).toBe(0);
    });

    it('suppresses static map path lines when planned routes exist', () => {
      const stops = [
        { id: 1, latitude: 37.8, longitude: -122.4 },
        { id: 2, latitude: 37.5, longitude: -122.5 },
      ];
      const tracksWithPlan = [
        { id: 'p1', kind: 'planned', name: 'Plan' }
      ];
      const tracksNoPlan = [];

      function buildStaticPath(stopsToDraw, tracks) {
        let pathParam = '';
        const hasOverallPlanned = voyageHasOverallPlannedRoute(tracks, stopsToDraw);
        if (!hasOverallPlanned) {
          for (let i = 0; i < stopsToDraw.length - 1; i++) {
            const from = stopsToDraw[i];
            const to = stopsToDraw[i + 1];
            if (!hasPlannedRouteForLeg(from, to, i, stopsToDraw, tracks)) {
              pathParam += `&path=color:0x999999ff|weight:1|${from.latitude},${from.longitude}|${to.latitude},${to.longitude}`;
            }
          }
        }
        return pathParam;
      }

      expect(buildStaticPath(stops, tracksWithPlan)).toBe('');
      expect(buildStaticPath(stops, tracksNoPlan)).toContain('&path=color:0x999999ff|weight:1|37.8,-122.4|37.5,-122.5');
    });
  });

  describe('Track Persistence across Stop Research and Map Re-renders', () => {
    it('preserves track polylines and layer controls when clearMap is called with clearTracks false', () => {
      const setMapSpy = jasmine.createSpy('setMap');
      let trackPolylines = [{ id: 't1', kind: 'recorded', polyline: { setMap: setMapSpy } }];
      const trackControls = document.getElementById('track-layer-controls');
      trackControls.classList.remove('hidden');

      function clearMap(options = {}) {
        const { clearTracks = true } = options;
        if (clearTracks) {
          trackPolylines.forEach(tp => {
            if (tp.polyline) tp.polyline.setMap(null);
          });
          trackPolylines = [];
          if (trackControls) trackControls.classList.add('hidden');
        }
      }

      // Re-rendering map stops passes clearTracks: false
      clearMap({ clearTracks: false });
      expect(trackPolylines.length).toBe(1);
      expect(setMapSpy).not.toHaveBeenCalled();
      expect(trackControls.classList.contains('hidden')).toBe(false);

      // Closing voyage passes clearTracks: true (or default)
      clearMap();
      expect(trackPolylines.length).toBe(0);
      expect(setMapSpy).toHaveBeenCalledWith(null);
      expect(trackControls.classList.contains('hidden')).toBe(true);
    });

    it('re-renders tracks if polylines were empty when stops are re-rendered', async () => {
      let trackPolylines = [];
      const currentTracks = [{ id: 't1', kind: 'recorded', name: 'Leg 1 Track' }];
      const trackControls = document.getElementById('track-layer-controls');

      let renderTrackPolylinesCalled = false;
      const renderTrackPolylines = async () => {
        renderTrackPolylinesCalled = true;
        trackPolylines.push({ id: 't1', kind: 'recorded' });
      };

      const updateTrackLayerControls = () => {
        if (currentTracks && currentTracks.length > 0) {
          trackControls.classList.remove('hidden');
        } else {
          trackControls.classList.add('hidden');
        }
      };

      // Simulating renderMapStops logic after stop research rerun
      if (currentTracks && currentTracks.length > 0) {
        if (trackPolylines.length === 0) {
          await renderTrackPolylines();
        }
        updateTrackLayerControls();
      }

      expect(renderTrackPolylinesCalled).toBe(true);
      expect(trackPolylines.length).toBe(1);
      expect(trackControls.classList.contains('hidden')).toBe(false);
    });
  });
});

