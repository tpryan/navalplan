import { API } from './api.js';

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

    // Show App Content
    if (sidebar) sidebar.classList.remove('hidden');
    if (mapContainer) mapContainer.classList.remove('hidden');
    if (btnNewVoyage) btnNewVoyage.classList.remove('hidden');

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
}

function updateUIForLogout() {
    console.log('Auth: Updating UI for Logout');
    const container = document.getElementById('auth-container');
    const sidebar = document.getElementById('sidebar');
    const mapContainer = document.getElementById('map-container');
    const btnNewVoyage = document.getElementById('btn-new-voyage');

    if (!container) return;

    // Show App Content (allow exploration without login)
    if (sidebar) sidebar.classList.remove('hidden');
    if (mapContainer) mapContainer.classList.remove('hidden');
    if (btnNewVoyage) btnNewVoyage.classList.add('hidden');

    // Show simple floating login button + help link
    container.className = '';
    container.innerHTML = `
        <a href="/help" id="btn-help-floating" class="btn secondary" target="_blank" title="User Guide">
            <span class="material-symbols-outlined">help_outline</span>
        </a>
        <a href="/auth/google/login" id="btn-login-floating" class="btn primary">
            Login
        </a>
    `;
}
