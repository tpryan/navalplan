import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/'
});

global.window = dom.window;
global.document = dom.window.document;

// Handle navigator safely
try {
    global.navigator = dom.window.navigator;
} catch (e) {
    Object.defineProperty(global, 'navigator', {
        value: dom.window.navigator,
        configurable: true,
        writable: true
    });
}

global.HTMLElement = dom.window.HTMLElement; // Needed for some checks
global.localStorage = dom.window.localStorage;
global.sessionStorage = dom.window.sessionStorage;

// JSDOM does not implement matchMedia; provide a minimal stub.
global.matchMedia = global.matchMedia || function() {
    return { matches: false, addListener: () => {}, removeListener: () => {} };
};

// Make common globals available on window
if (!global.window.fetch) {
    global.window.fetch = global.fetch;
}

// Override global.fetch to delegate to window.fetch
// This ensures that if window.fetch is replaced (spied), global.fetch uses the replacement.
const originalFetch = global.fetch;
global.fetch = function(...args) {
    return global.window.fetch.apply(global.window, args);
};

if (!global.window.FormData) {
    global.window.FormData = global.FormData;
}

// Simple requestAnimationFrame polyfill
global.requestAnimationFrame = function(callback) {
  return setTimeout(callback, 0);
};
global.cancelAnimationFrame = function(id) {
  clearTimeout(id);
};

// Cleanup if necessary, though JSDOM usually handles itself in memory
