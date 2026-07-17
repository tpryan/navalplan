import { isDayTrip, formatVoyageDateRange } from '../js/utils.js';

describe('isDayTrip', () => {
    it('is true when start_date and end_date fall on the same calendar date', () => {
        const voyage = { start_date: '2026-07-19T00:00:00Z', end_date: '2026-07-19T00:00:00Z' };
        expect(isDayTrip(voyage)).toBeTrue();
    });

    it('is false when end_date is a later date', () => {
        const voyage = { start_date: '2026-07-19T00:00:00Z', end_date: '2026-07-20T00:00:00Z' };
        expect(isDayTrip(voyage)).toBeFalse();
    });

    it('is false when the voyage has no dates set', () => {
        expect(isDayTrip({ start_date: null, end_date: null })).toBeFalse();
        expect(isDayTrip({})).toBeFalse();
    });

    it('is false when only one of start/end is set', () => {
        expect(isDayTrip({ start_date: '2026-07-19T00:00:00Z', end_date: null })).toBeFalse();
        expect(isDayTrip({ start_date: null, end_date: '2026-07-19T00:00:00Z' })).toBeFalse();
    });

    it('handles null voyage gracefully', () => {
        expect(isDayTrip(null)).toBeFalse();
        expect(isDayTrip(undefined)).toBeFalse();
    });
});

describe('formatVoyageDateRange', () => {
    it('collapses to a single date for a day trip instead of a dash range', () => {
        const result = formatVoyageDateRange('2026-07-19T00:00:00Z', '2026-07-19T00:00:00Z');
        expect(result).not.toContain('–');
        expect(result).toContain('2026');
    });

    it('renders a dash-separated range for a multi-day voyage', () => {
        const result = formatVoyageDateRange('2026-07-19T00:00:00Z', '2026-07-24T00:00:00Z');
        expect(result).toContain('–');
    });

    it('supports a custom separator', () => {
        const result = formatVoyageDateRange('2026-07-19T00:00:00Z', '2026-07-24T00:00:00Z', { separator: ' - ' });
        expect(result).toContain(' - ');
        expect(result).not.toContain('–');
    });

    it('returns just the start date when there is no end date', () => {
        const result = formatVoyageDateRange('2026-07-19T00:00:00Z', null);
        expect(result).not.toContain('–');
        expect(result.length).toBeGreaterThan(0);
    });

    it('returns an empty string when there is no start date', () => {
        expect(formatVoyageDateRange(null, null)).toBe('');
    });
});
