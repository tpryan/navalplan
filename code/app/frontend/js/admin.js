import DOMPurify from 'dompurify';
import { API } from './api.js';
import { showNotification } from './notifications.js';

let currentAdminPage = 1;
const ADMIN_PAGE_LIMIT = 20;

export function initAdminUI() {
    // Open modal via event delegation (button is injected by auth.js)
    document.addEventListener('click', async (e) => {
        if (!e.target.closest('#btn-open-admin')) return;
        const modal   = document.getElementById('modal-admin');
        const overlay = document.getElementById('modal-overlay');
        if (modal && overlay) {
            modal.classList.remove('hidden');
            overlay.classList.remove('hidden');
            currentAdminPage = 1;
            loadAdminUsers();
        }
    });

    const btnClose = document.getElementById('btn-close-admin');
    btnClose?.addEventListener('click', () => {
        document.getElementById('modal-admin').classList.add('hidden');
        document.getElementById('modal-overlay').classList.add('hidden');
    });

    document.getElementById('btn-admin-prev')?.addEventListener('click', () => {
        if (currentAdminPage > 1) { currentAdminPage--; loadAdminUsers(); }
    });

    document.getElementById('btn-admin-next')?.addEventListener('click', () => {
        currentAdminPage++;
        loadAdminUsers();
    });

    document.getElementById('form-invite-user')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const input = document.getElementById('invite-email');
        const email = input.value;
        const btn = e.currentTarget.querySelector('button');
        const originalText = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Inviting...';
        try {
            await API.inviteUser(email);
            input.value = '';
            loadAdminUsers();
            showNotification('User Invited', `${email} has been added to the allowlist.`);
        } catch (err) {
            console.error(err);
            showNotification('Error', 'Failed to invite user');
        } finally {
            btn.disabled = false;
            btn.textContent = originalText;
        }
    });
}

async function loadAdminUsers() {
    const tbody       = document.getElementById('admin-users-list');
    const btnPrev     = document.getElementById('btn-admin-prev');
    const btnNext     = document.getElementById('btn-admin-next');
    const pageDisplay = document.getElementById('admin-page-display');

    tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center">Loading...</td></tr>';

    try {
        const data    = await API.listAdminUsers(currentAdminPage, ADMIN_PAGE_LIMIT);
        const users   = data.users.data || [];
        const total   = data.users.total || 0;
        const invites = data.invites || [];

        tbody.innerHTML = '';

        if (currentAdminPage === 1 && invites.length) {
            invites.forEach(i => {
                const safeEmail = DOMPurify.sanitize(i.email);
                const tr = document.createElement('tr');
                tr.className = 'border-b bg-gray-light';
                tr.innerHTML = `
                    <td class="p-sm">${safeEmail}</td>
                    <td class="p-sm"><span class="badge badge-standard">Pending Invite</span></td>
                    <td class="p-sm"><button class="btn-text btn-danger font-sm p-0" data-revoke="${safeEmail}">Revoke</button></td>
                `;
                tbody.appendChild(tr);
            });
        }

        if (!users.length && !invites.length) {
            tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center">No users found</td></tr>';
        } else {
            users.forEach(u => {
                const safeEmail = DOMPurify.sanitize(u.email);
                let status = '<span class="badge badge-regional">Active User</span>';
                if (u.is_admin) status += ' <span class="badge badge-gem">Admin</span>';
                const tr = document.createElement('tr');
                tr.className = 'border-b';
                tr.innerHTML = `
                    <td class="p-sm truncate" title="${safeEmail}">${safeEmail}</td>
                    <td class="p-sm">${status}</td>
                    <td class="p-sm"><span class="text-gray font-sm">-</span></td>
                `;
                tbody.appendChild(tr);
            });
        }

        const totalPages = Math.ceil(total / ADMIN_PAGE_LIMIT);
        pageDisplay.textContent = `Page ${currentAdminPage} of ${totalPages || 1}`;
        if (btnPrev) btnPrev.disabled = currentAdminPage === 1;
        if (btnNext) btnNext.disabled = currentAdminPage >= totalPages;

    } catch (err) {
        console.error(err);
        tbody.innerHTML = '<tr><td colspan="3" class="p-sm text-center error-text">Failed to load users</td></tr>';
    }
}

// Delegated handler for Revoke buttons rendered inside the table rows.
document.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-revoke]');
    if (!btn) return;
    const email = btn.dataset.revoke;
    showNotification('Revoke Invitation', `Are you sure you want to revoke the invitation for ${email}?`, [
        {
            label: 'Revoke',
            type: 'danger',
            hideClose: true,
            callback: async () => {
                try {
                    await API.revokeInvitation(email);
                    loadAdminUsers();
                } catch (err) {
                    console.error(err);
                    showNotification('Error', 'Failed to revoke invitation');
                }
            },
        },
        { label: 'Cancel', type: 'secondary' },
    ]);
});
