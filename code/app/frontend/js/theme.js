const THEMES = [
    { id: 'nautical',         label: 'Nautical' },
    { id: 'midnight-mariner', label: 'Midnight Mariner' },
];

const STORAGE_KEY = 'navalplan-theme';

function applyTheme(id) {
    document.documentElement.setAttribute('data-theme', id);
}

function setTheme(id) {
    localStorage.setItem(STORAGE_KEY, id);
    applyTheme(id);
}

function loadTheme() {
    const saved = localStorage.getItem(STORAGE_KEY) || 'nautical';
    applyTheme(saved);
    return saved;
}

function currentTheme() {
    return localStorage.getItem(STORAGE_KEY) || 'nautical';
}

function cycleTheme() {
    const idx = THEMES.findIndex(t => t.id === currentTheme());
    const next = THEMES[(idx + 1) % THEMES.length];
    setTheme(next.id);
    return next;
}

export { THEMES, setTheme, loadTheme, currentTheme, cycleTheme };
