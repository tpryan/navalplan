import './helpers/dom-setup.mjs';
import { MapPin } from '../js/ui/MapPin.js';
import { groupColocatedStops, findNearbyJoinTargetStop } from '../js/utils.js';

describe('Combined Start/Stop MapPin and Co-located Stop Grouping', () => {
    it('renders a standard single-stop MapPin when one stop number is provided', () => {
        const el = MapPin({ accent: 'coral', n: 1, label: 'Stop 1: Long Beach' });
        expect(el.className).toBe('np-pin');
        expect(el.getAttribute('aria-label')).toBe('Stop 1: Long Beach');
        expect(el.textContent.trim()).toBe('1');
    });

    it('renders a dual-accent combined MapPin when multiple stop numbers are provided', () => {
        const el = MapPin({
            numbers: [1, 4],
            accents: ['coral', 'amber'],
            label: 'Stops 1 & 4: Long Beach',
        });
        expect(el.className).toContain('np-pin--combined');
        expect(el.getAttribute('data-stop-numbers')).toBe('1,4');
        expect(el.style.getPropertyValue('--pin')).toBe('var(--coral)');
        expect(el.style.getPropertyValue('--pin-end')).toBe('var(--amber)');
        expect(el.textContent.trim()).toBe('1+4');
    });

    it('groups start and finish stops within 2 NM into one group and maps both to the start position', () => {
        const stops = [
            { id: 10, location_name: 'Long Beach, CA, USA', latitude: 33.7600, longitude: -118.1900, target_date: '2027-04-30T00:00:00Z' },
            { id: 11, location_name: 'Los Angeles County, CA, USA', latitude: 33.4200, longitude: -118.5000, target_date: '2027-05-01T00:00:00Z' },
            { id: 12, location_name: 'Avalon, CA, USA', latitude: 33.3428, longitude: -118.3282, target_date: '2027-05-02T00:00:00Z' },
            { id: 13, location_name: 'Long Beach, CA, USA', latitude: 33.7560, longitude: -118.1870, target_date: '2027-05-03T00:00:00Z' },
        ];
        const { groups, stopPositionMap } = groupColocatedStops(stops, ['coral', 'teal', 'violet', 'amber'], 2.0);
        expect(groups.length).toBe(3);
        expect(groups[0].items.map(i => i.stopNumber)).toEqual([1, 4]);
        expect(groups[0].items.map(i => i.accent)).toEqual(['coral', 'amber']);
        expect(stopPositionMap.get(13)).toEqual({ lat: 33.7600, lng: -118.1900 });
    });

    it('identifies Stop 1 when clicking within 2 NM on a later stop date', () => {
        const stops = [
            { id: 10, location_name: 'Long Beach, CA, USA', latitude: 33.7600, longitude: -118.1900, target_date: '2027-04-30T00:00:00Z' },
            { id: 11, location_name: 'Avalon, CA, USA', latitude: 33.3428, longitude: -118.3282, target_date: '2027-05-02T00:00:00Z' },
        ];
        const match = findNearbyJoinTargetStop(stops, 33.7580, -118.1885, '2027-05-03', 2.0);
        expect(match).not.toBeNull();
        expect(match.isStartStop).toBeTrue();
        expect(match.stopNumber).toBe(1);
        expect(match.targetStop.id).toBe(10);

        // Same date as Stop 1 should not prompt to join with itself
        const selfMatch = findNearbyJoinTargetStop(stops, 33.7580, -118.1885, '2027-04-30', 2.0);
        expect(selfMatch).toBeNull();
    });
});
