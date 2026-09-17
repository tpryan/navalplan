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
    if (!tracks || tracks.length === 0) {
      container.innerHTML = '<p class="font-sm text-gray">No tracks uploaded yet for this voyage.</p>';
      if (countBadge) countBadge.textContent = '0 tracks';
      return;
    }

    if (countBadge) countBadge.textContent = `${tracks.length} track${tracks.length === 1 ? '' : 's'}`;

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
              <button class="btn secondary p-xs font-xs btn-track-debrief" data-track-id="${t.id}">Debrief</button>
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

  function renderDebriefCard(debrief) {
    if (!debrief) {
      debriefContainer.classList.add('hidden');
      return;
    }

    let parsed = debrief;
    if (typeof debrief === 'string') {
      try { parsed = JSON.parse(debrief); } catch (e) { parsed = { summary: debrief }; }
    }

    let obsHtml = '';
    if (Array.isArray(parsed.observations) && parsed.observations.length > 0) {
      obsHtml = `
        <ul class="m-0 pl-md font-sm">
          ${parsed.observations.map(o => `<li>${escapeTrackHtml(o)}</li>`).join('')}
        </ul>
      `;
    }

    debriefContainer.innerHTML = `
      <div class="debrief-card">
        <h4>Passage Debrief Analysis</h4>
        <p class="debrief-summary">${escapeTrackHtml(parsed.summary || 'Debrief complete.')}</p>
        <div class="debrief-stat-grid">
          ${parsed.recorded_distance_nm != null ? `<div class="rec-dist">${parsed.recorded_distance_nm.toFixed(1)} NM</div>` : ''}
          ${parsed.planned_distance_nm != null ? `<div class="plan-dist">${parsed.planned_distance_nm.toFixed(1)} NM</div>` : ''}
          ${parsed.distance_variance_pct != null ? `<div class="var-pct">${parsed.distance_variance_pct.toFixed(1)}%</div>` : ''}
          ${parsed.average_speed_kts != null ? `<div class="avg-spd">${parsed.average_speed_kts.toFixed(1)} kt</div>` : ''}
        </div>
        ${parsed.tacking_efficiency ? `<p class="tack-eff">${escapeTrackHtml(parsed.tacking_efficiency)}</p>` : ''}
        ${obsHtml}
      </div>
    `;
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

      // Check recorded track
      expect(items[1].querySelector('.track-badge-recorded')).not.toBeNull();
      expect(items[1].querySelector('.track-sog').textContent).toContain('5.1 kts');
    });
  });

  describe('Debrief Presentation', () => {
    it('hides debrief container when debrief is null', () => {
      renderDebriefCard(null);
      expect(debriefContainer.classList.contains('hidden')).toBe(true);
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
      expect(debriefContainer.querySelector('.var-pct').textContent).toBe('14.5%');
      expect(debriefContainer.querySelector('.rec-dist').textContent).toBe('25.2 NM');
      expect(debriefContainer.querySelector('.tack-eff').textContent).toContain('Minimal jibes');

      const lis = debriefContainer.querySelectorAll('li');
      expect(lis.length).toBe(2);
      expect(lis[0].textContent).toContain('Strong ebb tide');
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
});
