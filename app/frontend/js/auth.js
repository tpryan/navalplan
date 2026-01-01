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
    if (!container) return;

    container.innerHTML = ''; // Clear existing content

    const menu = document.createElement('div');
    menu.id = 'user-floating-menu';
    
    menu.innerHTML = `
        <img src="${person.picture_url || ''}" class="avatar" alt="${person.name}" title="${person.name}" style="background:#ccc;" />
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
    if (!container) return;

    container.innerHTML = ''; // Clear existing content

    const loginBtn = document.createElement('a');
    loginBtn.id = 'btn-login-floating';
    loginBtn.href = '/auth/google/login';
    loginBtn.className = 'btn primary';
    loginBtn.textContent = 'Login';
    
    container.appendChild(loginBtn);
}
