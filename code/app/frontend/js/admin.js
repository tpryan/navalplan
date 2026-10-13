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
        const expiresInput = document.getElementById('invite-expires');
        const email = input.value;
        const expiresAt = expiresInput ? expiresInput.value : '';
        const btn = e.currentTarget.querySelector('button');
        const originalText = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Inviting...';
        try {
            await API.inviteUser(email, expiresAt);
            input.value = '';
            if (expiresInput) expiresInput.value = '';
            loadAdminUsers();
            showNotification('User Invited', `${email} has been added to the allowlist${expiresAt ? ` (expires ${expiresAt})` : ''}.`);
        } catch (err) {
            console.error(err);
            showNotification('Error', 'Failed to invite user');
        } finally {
            btn.disabled = false;
            btn.textContent = originalText;
        }
    });
}

// Returns an ISO expiry as a YYYY-MM-DD value for a date input, or '' if none.
function expiryDateValue(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    return d.toISOString().split('T')[0];
}

// Returns an HTML badge describing an expiry (expired vs upcoming), or '' if none.
function expiryStatusBadge(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    if (d.getTime() < Date.now()) return ' <span class="error-text font-sm">Expired</span>';
    return ` <span class="badge badge-standard">Expires ${d.toLocaleDateString()}</span>`;
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
                    <td class="p-sm"><span class="badge badge-standard">Pending Invite</span>${expiryStatusBadge(i.expires_at)}</td>
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
                status += expiryStatusBadge(u.expires_at);
                const tr = document.createElement('tr');
                tr.className = 'border-b';
                tr.innerHTML = `
                    <td class="p-sm truncate" title="${safeEmail}">${safeEmail}</td>
                    <td class="p-sm">${status}</td>
                    <td class="p-sm">
                        <div class="flex flex-col gap-xs items-start">
                            <input type="date" class="font-sm p-xs border border-radius w-full" data-expiry-input="${u.id}" value="${expiryDateValue(u.expires_at)}" title="Account expiration (blank = permanent)" />
                            <button class="btn-text font-sm p-0" data-set-expiry="${u.id}">Save</button>
                        </div>
                    </td>
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

// Delegated handler for the per-user "Save" expiration buttons.
document.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-set-expiry]');
    if (!btn) return;
    const id = btn.dataset.setExpiry;
    const input = document.querySelector(`[data-expiry-input="${id}"]`);
    const value = input ? input.value : '';
    btn.disabled = true;
    try {
        await API.setUserExpiration(id, value);
        showNotification('Expiration Updated', value ? `Account expires ${value}.` : 'Expiration cleared — account is now permanent.');
        loadAdminUsers();
    } catch (err) {
        console.error(err);
        showNotification('Error', 'Failed to update expiration');
    } finally {
        btn.disabled = false;
    }
});
