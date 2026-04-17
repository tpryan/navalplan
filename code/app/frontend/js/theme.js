const STORAGE_KEY = 'theme';
const LEGACY_KEY = 'navalplan-theme';

const LEGACY_MAP = {
    'nautical': 'light',
    'midnight-mariner': 'dark',
};

function applyTheme(id) {
    document.documentElement.setAttribute('data-theme', id);
}

function resolveTheme() {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) return saved;

    const legacy = localStorage.getItem(LEGACY_KEY);
    if (legacy) {
        const mapped = LEGACY_MAP[legacy] || 'light';
        localStorage.setItem(STORAGE_KEY, mapped);
        localStorage.removeItem(LEGACY_KEY);
        return mapped;
    }

    return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function setTheme(id) {
    localStorage.setItem(STORAGE_KEY, id);
    applyTheme(id);
}

function loadTheme() {
    const theme = resolveTheme();
    applyTheme(theme);
    return theme;
}

function currentTheme() {
    return localStorage.getItem(STORAGE_KEY) || resolveTheme();
}

function toggleTheme() {
    const next = currentTheme() === 'dark' ? 'light' : 'dark';
    setTheme(next);
    return next;
}

export { setTheme, loadTheme, currentTheme, toggleTheme };
