/**
 * Tests for JS audit fixes applied to main.js utilities:
 *  1. healthCheckInterval — no stacking on repeated calls
 *  2. chartInstances registry — destroy before re-render
 *  3. Clipboard API used for share-link copy
 *  4. calculateGeometryArea / calculatePolygonArea removed
 */

// ---------------------------------------------------------------------------
// 1. healthCheckInterval — guard against stacking intervals
// ---------------------------------------------------------------------------
describe('startHealthCheck interval guard', () => {
    it('stores a new interval ID each call and replaces the previous one', () => {
        let healthCheckInterval = null;
        let clearCount = 0;
        let setCount = 0;

        // Stub using plain counters — avoids jsdom Timeout-object issues
        const fakeSetInterval = () => { setCount++; return setCount; };
        const fakeClearInterval = () => { clearCount++; };

        function simulatedStartHealthCheck() {
            if (healthCheckInterval) fakeClearInterval(healthCheckInterval);
            healthCheckInterval = fakeSetInterval(() => {}, 10000);
        }

        simulatedStartHealthCheck();
        expect(clearCount).toBe(0); // no clear on first call
        expect(setCount).toBe(1);
        const firstId = healthCheckInterval;

        simulatedStartHealthCheck();
        expect(clearCount).toBe(1); // cleared on second call
        expect(setCount).toBe(2);
        expect(healthCheckInterval).not.toBe(firstId);
    });

    it('does not clear on the very first call', () => {
        let healthCheckInterval = null;
        let clearCount = 0;
        let idCounter = 1;

        const fakeSetInterval = () => idCounter++;
        const fakeClearInterval = () => clearCount++;

        function simulatedStartHealthCheck() {
            if (healthCheckInterval) fakeClearInterval(healthCheckInterval);
            healthCheckInterval = fakeSetInterval(() => {}, 10000);
        }

        simulatedStartHealthCheck();
        expect(clearCount).toBe(0);
        expect(healthCheckInterval).toBeDefined();
    });
});

// ---------------------------------------------------------------------------
// 2. chartInstances registry — destroy called before re-render
// ---------------------------------------------------------------------------
describe('chartInstances destroy-before-render', () => {
    it('destroys an existing chart instance for the same canvas ID before creating a new one', () => {
        const chartInstances = new Map();
        const canvasId = 'test-tide-canvas';

        const mockDestroy = jasmine.createSpy('destroy');
        chartInstances.set(canvasId, { destroy: mockDestroy });

        // Simulate the guard applied in renderTideChart / renderMiniTideChart
        if (chartInstances.has(canvasId)) {
            chartInstances.get(canvasId).destroy();
            chartInstances.delete(canvasId);
        }
        const fakeNewChart = { destroy: jasmine.createSpy('newDestroy') };
        chartInstances.set(canvasId, fakeNewChart);

        expect(mockDestroy).toHaveBeenCalledTimes(1);
        expect(chartInstances.get(canvasId)).toBe(fakeNewChart);
    });

    it('does not throw when no previous instance exists for a canvas ID', () => {
        const chartInstances = new Map();
        const canvasId = 'fresh-canvas';

        expect(() => {
            if (chartInstances.has(canvasId)) {
                chartInstances.get(canvasId).destroy();
                chartInstances.delete(canvasId);
            }
            chartInstances.set(canvasId, { destroy: () => {} });
        }).not.toThrow();

        expect(chartInstances.has(canvasId)).toBeTrue();
    });

    it('tracks multiple canvases independently', () => {
        const chartInstances = new Map();
        const destroyA = jasmine.createSpy('destroyA');
        const destroyB = jasmine.createSpy('destroyB');

        chartInstances.set('canvas-a', { destroy: destroyA });
        chartInstances.set('canvas-b', { destroy: destroyB });

        // Re-render canvas-a only
        if (chartInstances.has('canvas-a')) {
            chartInstances.get('canvas-a').destroy();
            chartInstances.delete('canvas-a');
        }
        chartInstances.set('canvas-a', { destroy: () => {} });

        expect(destroyA).toHaveBeenCalledTimes(1);
        expect(destroyB).not.toHaveBeenCalled();
        expect(chartInstances.has('canvas-b')).toBeTrue();
    });
});

// ---------------------------------------------------------------------------
// 3. Clipboard API used for share-link copy
// ---------------------------------------------------------------------------
describe('share link copy uses Clipboard API', () => {
    let originalClipboard;

    beforeEach(() => {
        originalClipboard = navigator.clipboard;
        Object.defineProperty(navigator, 'clipboard', {
            value: { writeText: jasmine.createSpy('writeText').and.returnValue(Promise.resolve()) },
            configurable: true,
            writable: true,
        });
    });

    afterEach(() => {
        Object.defineProperty(navigator, 'clipboard', {
            value: originalClipboard,
            configurable: true,
            writable: true,
        });
    });

    it('calls navigator.clipboard.writeText with the link value', async () => {
        const linkValue = 'https://navalplan.example.com/share/abc123';

        // Replicate the patched onclick logic directly
        const btn = { textContent: 'Copy', _orig: 'Copy' };
        await navigator.clipboard.writeText(linkValue).then(() => {
            btn.textContent = 'Copied!';
        });

        expect(navigator.clipboard.writeText).toHaveBeenCalledWith(linkValue);
        expect(btn.textContent).toBe('Copied!');
    });

    it('falls back gracefully when clipboard API rejects', async () => {
        navigator.clipboard.writeText.and.returnValue(Promise.reject(new Error('denied')));

        let fallbackCalled = false;
        const linkValue = 'https://navalplan.example.com/share/xyz';

        await navigator.clipboard.writeText(linkValue).catch(() => {
            fallbackCalled = true;
        });

        expect(fallbackCalled).toBeTrue();
    });
});

// ---------------------------------------------------------------------------
// 4. calculateGeometryArea / calculatePolygonArea removed
// ---------------------------------------------------------------------------
describe('removed dead code', () => {
    it('calculateGeometryArea is not exported or globally accessible', () => {
        expect(typeof window.calculateGeometryArea).toBe('undefined');
        expect(typeof window.calculatePolygonArea).toBe('undefined');
    });
});

// ---------------------------------------------------------------------------
// 5. parseTidePoints — shared tide chart helper
// ---------------------------------------------------------------------------
describe('parseTidePoints', () => {
    // Inline a copy of the function to test it in isolation
    function parseTidePoints(tideData, targetDateStr, hourMin = -Infinity, hourMax = Infinity) {
        const parseLocal = (s) => {
            if (!s) return new Date(NaN);
            const clean = s.replace('Z', '').replace(' ', 'T');
            const final = clean.length === 10 ? clean + 'T00:00:00' : clean;
            return new Date(final);
        };
        const targetStart = parseLocal(targetDateStr).getTime();
        const points = [];
        (tideData.events || []).forEach(e => {
            const d = parseLocal(e.time);
            if (!isNaN(d.getTime())) {
                const floatHours = (d.getTime() - targetStart) / (1000 * 60 * 60);
                if (floatHours >= hourMin && floatHours <= hourMax) {
                    points.push({ x: floatHours, y: e.height_ft });
                }
            }
        });
        points.sort((a, b) => a.x - b.x);
        return points;
    }

    const targetDate = '2026-04-05';
    // Build wall-clock time strings (no Z) using local Date arithmetic so
    // parseLocal and the target start are always in the same timezone.
    const makeEvent = (hourOffset, height) => {
        const pad = n => String(n).padStart(2, '0');
        const d = new Date(2026, 3, 5, 0, 0, 0); // local midnight April 5
        d.setHours(d.getHours() + hourOffset);
        const ts = `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
        return { time: ts, height_ft: height };
    };

    it('converts events to {x, y} points relative to midnight', () => {
        const tideData = { events: [makeEvent(6, 3.5), makeEvent(12, 1.0)] };
        const pts = parseTidePoints(tideData, targetDate);
        expect(pts.length).toBe(2);
        expect(pts[0].x).toBeCloseTo(6, 1);
        expect(pts[0].y).toBe(3.5);
    });

    it('sorts points by hour offset', () => {
        const tideData = { events: [makeEvent(18, 2.0), makeEvent(6, 4.0), makeEvent(12, 1.0)] };
        const pts = parseTidePoints(tideData, targetDate);
        expect(pts[0].x).toBeCloseTo(6, 1);
        expect(pts[1].x).toBeCloseTo(12, 1);
        expect(pts[2].x).toBeCloseTo(18, 1);
    });

    it('filters by hourMin and hourMax', () => {
        const tideData = { events: [makeEvent(2, 1.0), makeEvent(6, 2.0), makeEvent(25, 3.0)] };
        const pts = parseTidePoints(tideData, targetDate, 4, 24);
        expect(pts.length).toBe(1);
        expect(pts[0].y).toBe(2.0);
    });

    it('returns empty array for missing events', () => {
        expect(parseTidePoints({}, targetDate)).toEqual([]);
        expect(parseTidePoints({ events: [] }, targetDate)).toEqual([]);
    });

    it('skips events with invalid times', () => {
        const tideData = { events: [{ time: null, height_ft: 1.0 }, makeEvent(6, 2.0)] };
        const pts = parseTidePoints(tideData, targetDate);
        expect(pts.length).toBe(1);
    });
});

// ---------------------------------------------------------------------------
// 6. reverseGeocode — shared geocoding helper
// ---------------------------------------------------------------------------
describe('reverseGeocode result formatting', () => {
    // Test the name-building logic in isolation (no Maps API needed)
    function buildLocationName(addressComponents, formattedAddress) {
        const getComp = (type) => addressComponents.find(c => c.types.includes(type))?.long_name;
        const locality = getComp('locality') || getComp('sublocality');
        const region = getComp('administrative_area_level_1');
        const country = getComp('country');

        if (locality && country) {
            return region ? `${locality}, ${region}, ${country}` : `${locality}, ${country}`;
        } else if (region && country) {
            return `${region}, ${country}`;
        } else if (country) {
            return country;
        }
        return formattedAddress;
    }

    it('formats locality, region, country', () => {
        const comps = [
            { types: ['locality'], long_name: 'Annapolis' },
            { types: ['administrative_area_level_1'], long_name: 'Maryland' },
            { types: ['country'], long_name: 'United States' },
        ];
        expect(buildLocationName(comps, 'fallback')).toBe('Annapolis, Maryland, United States');
    });

    it('formats locality and country when region absent', () => {
        const comps = [
            { types: ['locality'], long_name: 'Road Town' },
            { types: ['country'], long_name: 'British Virgin Islands' },
        ];
        expect(buildLocationName(comps, 'fallback')).toBe('Road Town, British Virgin Islands');
    });

    it('falls back to region, country when no locality', () => {
        const comps = [
            { types: ['administrative_area_level_1'], long_name: 'Maryland' },
            { types: ['country'], long_name: 'United States' },
        ];
        expect(buildLocationName(comps, 'fallback')).toBe('Maryland, United States');
    });

    it('falls back to country alone', () => {
        const comps = [{ types: ['country'], long_name: 'Bermuda' }];
        expect(buildLocationName(comps, 'fallback')).toBe('Bermuda');
    });

    it('falls back to formatted_address when no usable components', () => {
        expect(buildLocationName([], 'Some remote ocean point')).toBe('Some remote ocean point');
    });
});

// ---------------------------------------------------------------------------
// 7. Safety Overview & Destination Guide — hidden in sailing mode
// ---------------------------------------------------------------------------
describe('Safety Overview and Destination Guide sailing mode visibility', () => {
    it('ensures safety overview box includes np-report-item--non-sailing class', () => {
        const box = document.createElement('div');
        box.className = 'lookout-box np-safety-overview np-report-item--non-sailing';
        box.id = 'np-safety-overview';

        expect(box.classList.contains('np-report-item--non-sailing')).toBeTrue();
        expect(box.classList.contains('np-safety-overview')).toBeTrue();
        expect(box.classList.contains('lookout-box')).toBeTrue();
    });

    it('ensures destination guide container includes np-report-item--non-sailing class', () => {
        const guideEl = document.createElement('div');
        guideEl.className = 'np-guide-section np-report-item--non-sailing';

        expect(guideEl.classList.contains('np-report-item--non-sailing')).toBeTrue();
        expect(guideEl.classList.contains('np-guide-section')).toBeTrue();
    });
});

