import { PilotNotesBox, renderPilotNotesHTML, normalizePilotNotes } from '../js/ui/PilotNotesBox.js';

describe('normalizePilotNotes', () => {
  it('handles null, undefined, or empty values', () => {
    expect(normalizePilotNotes(null)).toBeNull();
    expect(normalizePilotNotes(undefined)).toBeNull();
    expect(normalizePilotNotes('')).toBeNull();
    expect(normalizePilotNotes({})).toBeNull();
  });

  it('parses JSON string into an object', () => {
    const raw = '{"overview": "Great harbor", "approach_and_channels": "Deep channel"}';
    const parsed = normalizePilotNotes(raw);
    expect(typeof parsed).toBe('object');
    expect(parsed.overview).toBe('Great harbor');
    expect(parsed.approach_and_channels).toBe('Deep channel');
  });

  it('preserves non-JSON string', () => {
    const str = 'Plain text pilot guidance notes.';
    expect(normalizePilotNotes(str)).toBe(str);
  });
});

describe('PilotNotesBox DOM Component', () => {
  it('returns null for empty pilot notes', () => {
    expect(PilotNotesBox(null)).toBeNull();
    expect(PilotNotesBox({})).toBeNull();
  });

  it('renders structured pilot notes with sub-cards and sources', () => {
    const pilotNotes = {
      overview: 'Protected harbor with good all-around holding.',
      approach_and_channels: 'Controlling depth of 14 ft in main channel.',
      anchorages_and_moorings: 'Designated anchorage in North Cove.',
      regulations_and_hazards: 'Strict 5-knot no wake zone.',
      sources: ['NOAA Coast Pilot 2, Chapter 8', 'https://example.com/pilot']
    };

    const el = PilotNotesBox(pilotNotes);
    expect(el).not.toBeNull();
    expect(el.className).toContain('pilot-notes-section');

    const header = el.querySelector('h3');
    expect(header.textContent).toContain('Pilotage & Harbor Guidance');

    const overview = el.querySelector('.pilot-notes-overview');
    expect(overview.textContent).toBe(pilotNotes.overview);

    const cards = el.querySelectorAll('.pilot-note-card');
    expect(cards.length).toBe(3);

    const sources = el.querySelectorAll('.pilot-notes-sources a, .pilot-notes-sources span.pilot-source-tag');
    expect(sources.length).toBe(2);
  });

  it('renders string pilot notes', () => {
    const el = PilotNotesBox('Important pilot warning: rocky bar on south entrance.');
    expect(el).not.toBeNull();
    const p = el.querySelector('.pilot-notes-prose');
    expect(p.textContent).toBe('Important pilot warning: rocky bar on south entrance.');
  });
});

describe('renderPilotNotesHTML string renderer', () => {
  it('returns empty string for null/empty notes', () => {
    expect(renderPilotNotesHTML(null)).toBe('');
    expect(renderPilotNotesHTML({})).toBe('');
  });

  it('renders complete HTML structure for structured data without non-sailing class', () => {
    const pilotNotes = {
      overview: 'Historic harbor.',
      approach_and_channels: 'Stay mid-channel.',
      sources: ['Coast Pilot 1']
    };

    const html = renderPilotNotesHTML(pilotNotes);
    expect(html).toContain('Pilotage & Harbor Guidance');
    expect(html).toContain('Historic harbor.');
    expect(html).toContain('Stay mid-channel.');
    expect(html).toContain('Coast Pilot 1');
    expect(html).not.toContain('np-report-item--non-sailing');
  });

  it('renders string HTML without non-sailing class', () => {
    const html = renderPilotNotesHTML('Important navigation warning');
    expect(html).toContain('Important navigation warning');
    expect(html).not.toContain('np-report-item--non-sailing');
  });

  it('structures Departure & Arrival Timing with bold Recommended Departure and separate paragraphs', () => {
    const pilotNotes = {
      departure_and_arrival_timing: {
        recommended_departure_window: '12:00 PM (ETA 05:47 PM, 133 min before sunset)',
        departure_tide_constraint: 'Departing near low tide requires verifying channel depths.',
        arrival_outlook: 'Expected arrival in Talbot County around 05:47 PM.',
        night_entry_and_aids_warning: 'Exercise caution regarding unlit buoys.'
      }
    };

    const html = renderPilotNotesHTML(pilotNotes);
    expect(html).toContain('pilot-note-card__lead');
    expect(html).toContain('<strong>Recommended Departure: 12:00 PM (ETA 05:47 PM, 133 min before sunset)</strong>');
    expect(html).toContain('<p class="pilot-note-card__para">Departing near low tide requires verifying channel depths.</p>');
    expect(html).toContain('<p class="pilot-note-card__para">Expected arrival in Talbot County around 05:47 PM.</p>');
    expect(html).toContain('<p class="pilot-note-card__para">Exercise caution regarding unlit buoys.</p>');
  });

  it('links pilotage source citations to Coast Pilot PDFs, eCFR sections, and search fallbacks', () => {
    const pilotNotes = {
      overview: 'Severn River approach.',
      sources: [
        'NOAA Coast Pilot 3, Chapter 13',
        '33 CFR § 110.159',
        'Annapolis City Code Title 15.10'
      ]
    };

    const html = renderPilotNotesHTML(pilotNotes);
    expect(html).toContain('href="https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp3/CPB3_WEB.pdf"');
    expect(html).toContain('href="https://www.ecfr.gov/current/title-33/section-110.159"');
    expect(html).toContain('href="https://www.google.com/search?q=Annapolis%20City%20Code%20Title%2015.10"');
  });
});
