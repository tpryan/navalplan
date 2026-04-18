import { announce } from './utils.js';

/**
 * Display a modal notification.
 * @param {string} title
 * @param {string} message
 * @param {Array<{label, type, hideClose, callback}>|null} actions  Optional action buttons.
 *   Set hideClose:true or type:'secondary' on any action to suppress the default Close button.
 */
export function showNotification(title, message, actions = null) {
    const modal          = document.getElementById('modal-notification');
    const modalOverlay   = document.getElementById('modal-overlay');
    const titleEl        = document.getElementById('notification-title');
    const msgEl          = document.getElementById('notification-message');
    const actionsContainer = document.getElementById('notification-actions');
    const closeBtn       = document.getElementById('btn-close-notification');

    if (!modal || !titleEl || !msgEl) {
        alert(`${title}\n\n${message}`);
        return;
    }

    titleEl.textContent = title;
    msgEl.textContent   = message;
    announce(`${title}: ${message}`);

    if (actionsContainer) {
        actionsContainer.querySelectorAll('.custom-action').forEach(b => b.remove());

        if (actions) {
            const actionList = Array.isArray(actions) ? actions : [actions];
            const hasCancelAction = actionList.some(a =>
                a.hideClose || a.type === 'secondary' || (a.label || '').toLowerCase() === 'cancel'
            );
            closeBtn.classList.toggle('hidden', hasCancelAction);

            actionList.forEach(action => {
                const btn = document.createElement('button');
                btn.className = `btn ${action.type || 'primary'} w-full custom-action`;
                btn.textContent = action.label;
                btn.onclick = () => {
                    modal.classList.add('hidden');
                    modalOverlay.classList.add('hidden');
                    if (action.callback) action.callback();
                };
                actionsContainer.insertBefore(btn, closeBtn);
            });
        } else {
            closeBtn.classList.remove('hidden');
        }
    }

    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');
}
