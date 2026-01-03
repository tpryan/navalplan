import { API } from './api.js';

export async function checkSession() {
    console.log('Auth: Checking session...');
    try {
        const person = await API.getPerson();
        console.log('Auth: Session found', person);
        if (person) {
            updateUIForLogin(person);
        } else {
            console.warn('Auth: No person returned');
            updateUIForLogout();
        }
    } catch (e) {
        console.error('Auth: Session check failed', e);
        updateUIForLogout();
    }
}

function updateUIForLogin(person) {
    console.log('Auth: Updating UI for Login');
    const container = document.getElementById('auth-container');
    const sidebar = document.getElementById('sidebar');
    const mapContainer = document.getElementById('map-container');

    if (!container) return;

    // Show App Content
    if (sidebar) sidebar.classList.remove('hidden');
    if (mapContainer) mapContainer.classList.remove('hidden');

    // Reset Container Style (remove modal class)
    container.className = ''; 
    container.innerHTML = ''; // Clear existing content

    const menu = document.createElement('div');
    menu.id = 'user-floating-menu';
    
    menu.innerHTML = `
        <img src="${person.picture_url || ''}" class="avatar" alt="${person.name}" title="${person.name}" />
        <div class="menu-actions">
            <a href="/auth/logout" class="btn-icon" title="Logout">
                <span class="material-symbols-outlined">logout</span>
            </a>
        </div>
    `;
    
    container.appendChild(menu);
}

function updateUIForLogout() {
    console.log('Auth: Updating UI for Logout');
    const container = document.getElementById('auth-container');
    const sidebar = document.getElementById('sidebar');
    const mapContainer = document.getElementById('map-container');
    
    if (!container) return;

    // Hide App Content
    if (sidebar) sidebar.classList.add('hidden');
    if (mapContainer) mapContainer.classList.add('hidden');

    // Show Full Screen Modal
    container.className = 'full-screen-modal';
    container.innerHTML = `
        <div class="login-box">
            <h1>NavalPlan</h1>
            <p>Plan your next voyage with ease.</p>
            <a href="/auth/google/login" class="btn primary login-btn-full">
                Login with Google
            </a>
        </div>
    `;
}
