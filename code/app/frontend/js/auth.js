import { API } from './api.js';
import { currentTheme } from './theme.js';

export let currentUser = null;

export async function checkSession() {
    console.log('Auth: Checking session...');
    try {
        const person = await API.getPerson();
        console.log('Auth: Session found', person);
        if (person) {
            currentUser = person;
            updateUIForLogin(person);
        } else {
            console.warn('Auth: No person returned');
            currentUser = null;
            updateUIForLogout();
        }
    } catch (e) {
        console.error('Auth: Session check failed', e);
        currentUser = null;
        updateUIForLogout();
    }
}

function updateUIForLogin(person) {
    console.log('Auth: Updating UI for Login');
    const container = document.getElementById('auth-container');
    const sidebar = document.getElementById('sidebar');
    const mapContainer = document.getElementById('map-container');
    const btnNewVoyage = document.getElementById('btn-new-voyage');

    if (!container) return;

    const sidebarActions = document.querySelector('.sidebar-actions');

    // Show App Content
    if (sidebar) sidebar.classList.remove('hidden');
    if (mapContainer) mapContainer.classList.remove('hidden');
    if (btnNewVoyage) btnNewVoyage.classList.remove('hidden');
    if (sidebarActions) sidebarActions.classList.remove('hidden');

    // Reset Container Style (remove modal class)
    container.className = ''; 
    container.innerHTML = ''; // Clear existing content

    const menu = document.createElement('div');
    menu.id = 'user-floating-menu';

    const img = document.createElement('img');
    img.src = person.picture_url || '';
    img.className = 'avatar';
    img.alt = person.name;
    img.title = person.name;

    const actions = document.createElement('div');
    actions.className = 'menu-actions';

    const logoutLink = document.createElement('button');
    logoutLink.className = 'btn-icon';
    logoutLink.title = 'Logout';
    logoutLink.onclick = async (e) => {
        e.preventDefault();
        try {
            // Using API fetch automatically adds CSRF header
            // We use 'suppressAuthRedirect' = true to avoid infinite loops if already out
            await API.logout(); 
            window.location.href = '/'; 
        } catch (err) {
            console.error('Logout failed', err);
            // Force reload anyway
            window.location.href = '/';
        }
    };

    const logoutIcon = document.createElement('span');
    logoutIcon.className = 'material-symbols-outlined';
    logoutIcon.textContent = 'logout';

    logoutLink.appendChild(logoutIcon);
    
    if (person.is_admin) {
        const adminBtn = document.createElement('button');
        adminBtn.className = 'btn-icon';
        adminBtn.title = 'Admin Console';
        adminBtn.id = 'btn-open-admin'; 
        adminBtn.innerHTML = '<span class="material-symbols-outlined">admin_panel_settings</span>';
        // We will attach listener in main.js using event delegation or direct access
        actions.appendChild(adminBtn);
    }

    const helpLink = document.createElement('a');
    helpLink.href = '/help';
    helpLink.target = '_blank';
    helpLink.className = 'btn-icon';
    helpLink.title = 'User Guide';
    helpLink.innerHTML = '<span class="material-symbols-outlined">help_outline</span>';

    actions.appendChild(helpLink);
    actions.appendChild(logoutLink);
    menu.appendChild(img);
    menu.appendChild(actions);

    container.appendChild(menu);
    container.appendChild(makeThemeButton());
}


function updateUIForLogout() {
    console.log('Auth: Updating UI for Logout');
    const container = document.getElementById('auth-container');
    const sidebar = document.getElementById('sidebar');
    const mapContainer = document.getElementById('map-container');
    const btnNewVoyage = document.getElementById('btn-new-voyage');
    const sidebarActions = document.querySelector('.sidebar-actions');
    const voyageList = document.getElementById('voyage-list');

    if (!container) return;

    if (sidebar) sidebar.classList.remove('hidden');
    if (mapContainer) mapContainer.classList.remove('hidden');
    if (btnNewVoyage) btnNewVoyage.classList.add('hidden');
    if (sidebarActions) sidebarActions.classList.add('hidden');

    // Auth-container: help + login pill buttons
    container.className = '';
    container.innerHTML = `
        <a href="/help" id="btn-help-floating" class="btn secondary" target="_blank" title="User Guide"
           style="width:44px;height:44px;padding:0;border-radius:50%;box-shadow:var(--shadow-soft)">
            <span class="material-symbols-outlined" style="font-size:20px;margin:0">help_outline</span>
        </a>
        <a href="/auth/google/login" id="btn-login-floating" class="btn primary">Sign in</a>
    `;
    container.appendChild(makeThemeButton());

    // Sidebar greeting (§5.1)
    if (voyageList) {
        voyageList.innerHTML = '';
        const greeting = document.createElement('div');
        greeting.style.cssText = 'display:flex;flex-direction:column;gap:18px;padding:4px 4px 8px';

        const headline = document.createElement('p');
        headline.style.cssText = 'font-size:26px;font-weight:800;line-height:1.15;letter-spacing:-.5px;color:var(--ink);margin:0';
        headline.innerHTML = 'Plan your perfect <span style="color:var(--coral)">voyage.</span>';
        greeting.appendChild(headline);

        const chips = document.createElement('div');
        chips.style.cssText = 'display:flex;flex-wrap:wrap;gap:6px';
        [
            { label: 'Anchorages', emoji: '⚓', accent: 'teal' },
            { label: 'Marinas',    emoji: '⛵', accent: 'amber' },
            { label: 'Tides',      emoji: '🌊', accent: 'violet' },
            { label: 'Winds',      emoji: '💨', accent: 'sky' },
        ].forEach(({ label, emoji, accent }) => {
            const chip = document.createElement('span');
            chip.style.cssText = `display:inline-flex;align-items:center;gap:4px;height:28px;padding:0 10px;border-radius:14px;font-size:12px;font-weight:700;background:color-mix(in oklab,var(--${accent}) 12%,var(--surface));color:var(--ink)`;
            chip.textContent = `${emoji} ${label}`;
            chips.appendChild(chip);
        });
        greeting.appendChild(chips);

        const btnWrap = document.createElement('div');
        btnWrap.style.cssText = 'display:flex;flex-direction:column;gap:8px;margin-top:4px';

        const signIn = document.createElement('a');
        signIn.href = '/auth/google/login';
        signIn.className = 'btn primary';
        signIn.textContent = 'Sign in →';

        const explore = document.createElement('button');
        explore.type = 'button';
        explore.className = 'btn secondary';
        explore.style.cssText += ';border:1.5px solid var(--hair);background:transparent;color:var(--ink)';
        explore.textContent = 'Explore the map';
        explore.addEventListener('click', () => {
            if (sidebar) sidebar.classList.add('hidden');
        });

        btnWrap.appendChild(signIn);
        btnWrap.appendChild(explore);
        greeting.appendChild(btnWrap);
        voyageList.appendChild(greeting);
    }
}

function makeThemeButton() {
    const btn = document.createElement('button');
    btn.id = 'btn-theme-toggle';
    btn.type = 'button';
    btn.setAttribute('aria-label', 'Toggle dark mode');
    btn.setAttribute('aria-pressed', currentTheme() === 'dark' ? 'true' : 'false');
    btn.style.cssText = [
        'display:inline-flex', 'align-items:center', 'justify-content:center',
        'width:44px', 'height:44px', 'border-radius:50%',
        'background:var(--surface)', 'border:none', 'cursor:pointer',
        'box-shadow:var(--shadow-soft)', 'font-size:20px', 'flex-shrink:0',
        'transition:opacity .15s',
    ].join(';');
    btn.textContent = currentTheme() === 'dark' ? '☀️' : '🌙';
    btn.addEventListener('mouseenter', () => { btn.style.opacity = '.8'; });
    btn.addEventListener('mouseleave', () => { btn.style.opacity = '1'; });
    btn.addEventListener('focus', () => { btn.style.outline = '3px solid var(--focus)'; btn.style.outlineOffset = '2px'; });
    btn.addEventListener('blur', () => { btn.style.outline = 'none'; });
    return btn;
}
