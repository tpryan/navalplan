/**
 * Tests for discovery routing and month parsing logic.
 */

describe('Discovery Routing & Month Parsing', () => {
    const MONTH_SLUGS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
    const FULL_MONTH_NAMES = ['january', 'february', 'march', 'april', 'may', 'june', 'july', 'august', 'september', 'october', 'november', 'december'];

    function parseMonthSlug(slug) {
        if (!slug) return new Date().getMonth() + 1;
        const lower = String(slug).toLowerCase().trim();
        if (/^\d+$/.test(lower)) {
            const num = parseInt(lower, 10);
            if (num >= 1 && num <= 12) return num;
        }
        const abbrIndex = MONTH_SLUGS.indexOf(lower);
        if (abbrIndex !== -1) return abbrIndex + 1;
        const fullNameIndex = FULL_MONTH_NAMES.indexOf(lower);
        if (fullNameIndex !== -1) return fullNameIndex + 1;
        return new Date().getMonth() + 1;
    }

    describe('parseMonthSlug', () => {
        it('parses numeric month strings between 1 and 12', () => {
            expect(parseMonthSlug('1')).toBe(1);
            expect(parseMonthSlug('5')).toBe(5);
            expect(parseMonthSlug('05')).toBe(5);
            expect(parseMonthSlug('12')).toBe(12);
        });

        it('parses 3-letter month abbreviations case-insensitively', () => {
            expect(parseMonthSlug('jan')).toBe(1);
            expect(parseMonthSlug('Jan')).toBe(1);
            expect(parseMonthSlug('MAY')).toBe(5);
            expect(parseMonthSlug('dec')).toBe(12);
        });

        it('parses full month names case-insensitively', () => {
            expect(parseMonthSlug('january')).toBe(1);
            expect(parseMonthSlug('January')).toBe(1);
            expect(parseMonthSlug('SEPTEMBER')).toBe(9);
            expect(parseMonthSlug('december')).toBe(12);
        });

        it('defaults to current calendar month for missing or invalid values', () => {
            const currentMonth = new Date().getMonth() + 1;
            expect(parseMonthSlug('')).toBe(currentMonth);
            expect(parseMonthSlug(null)).toBe(currentMonth);
            expect(parseMonthSlug(undefined)).toBe(currentMonth);
            expect(parseMonthSlug('invalid')).toBe(currentMonth);
            expect(parseMonthSlug('0')).toBe(currentMonth);
            expect(parseMonthSlug('13')).toBe(currentMonth);
        });
    });

    describe('canonical URL generation', () => {
        it('maps month numbers 1-12 to standard 3-letter slugs', () => {
            expect(MONTH_SLUGS[0]).toBe('jan');
            expect(MONTH_SLUGS[4]).toBe('may');
            expect(MONTH_SLUGS[11]).toBe('dec');
        });

        it('generates canonical path /discover/[month]', () => {
            const month = parseMonthSlug('September');
            const canonicalPath = `/discover/${MONTH_SLUGS[month - 1]}`;
            expect(canonicalPath).toBe('/discover/sep');
        });
    });
});
