import { LookoutBox, normalizeLookoutIcon } from '../js/ui/LookoutBox.js';

describe('normalizeLookoutIcon', () => {
  it('maps bridge and clearance variations to height icon', () => {
    expect(normalizeLookoutIcon('bridge')).toBe('height');
    expect(normalizeLookoutIcon('BRIDGE')).toBe('height');
    expect(normalizeLookoutIcon('bridges')).toBe('height');
    expect(normalizeLookoutIcon('clearance')).toBe('height');
    expect(normalizeLookoutIcon('vertical_clearance')).toBe('height');
    expect(normalizeLookoutIcon('overhead')).toBe('height');
  });

  it('maps marine, navigation, route, and passage variations appropriately', () => {
    expect(normalizeLookoutIcon('route')).toBe('alt_route');
    expect(normalizeLookoutIcon('passage')).toBe('alt_route');
    expect(normalizeLookoutIcon('transit')).toBe('alt_route');
    expect(normalizeLookoutIcon('channel')).toBe('straighten');
    expect(normalizeLookoutIcon('directions_boat')).toBe('directions_boat');
    expect(normalizeLookoutIcon('boat')).toBe('directions_boat');
  });

  it('maps weather, tides, and currents appropriately', () => {
    expect(normalizeLookoutIcon('wind')).toBe('air');
    expect(normalizeLookoutIcon('tide')).toBe('waves');
    expect(normalizeLookoutIcon('current')).toBe('water');
    expect(normalizeLookoutIcon('sun')).toBe('light_mode');
    expect(normalizeLookoutIcon('sunset')).toBe('wb_twilight');
  });

  it('falls back to warning for empty or null icon', () => {
    expect(normalizeLookoutIcon('')).toBe('warning');
    expect(normalizeLookoutIcon(null)).toBe('warning');
    expect(normalizeLookoutIcon(undefined)).toBe('warning');
  });
});

describe('LookoutBox UI Component', () => {
  it('renders bridge alert with normalized height icon', () => {
    const alerts = [
      {
        severity: 'warning',
        category: 'navigation',
        message: 'Knapps Narrows features a 12-foot vertical clearance bascule bridge.',
        action: 'Check tide heights before passing.',
        icon: 'bridge'
      }
    ];

    const box = LookoutBox(alerts);
    expect(box).not.toBeNull();
    const iconEl = box.querySelector('.lookout-alert__icon');
    expect(iconEl).not.toBeNull();
    expect(iconEl.textContent).toBe('height');
  });
});
