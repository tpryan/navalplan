// Export Logic for NavalPlan (Frontend)

let gapiInited = false;
let gisInited = false;
let tokenClient;

// Config - To be replaced or injected
const CLIENT_ID = '70159681032-p104gq7knn6urthgk6clprir8frbntoe.apps.googleusercontent.com';
const SCOPES = 'https://www.googleapis.com/auth/documents https://www.googleapis.com/auth/drive.file';

export async function initGoogleAuth() {
    await loadGapi();
    await loadGis();
}

function loadGapi() {
    return new Promise((resolve) => {
        const script = document.createElement('script');
        script.src = 'https://apis.google.com/js/api.js';
        script.onload = () => {
            gapi.load('client', async () => {
                await gapi.client.init({
                    // apiKey: API_KEY, // Optional if using only Identity
                    discoveryDocs: ['https://docs.googleapis.com/$discovery/rest?version=v1'],
                });
                gapiInited = true;
                resolve();
            });
        };
        document.body.appendChild(script);
    });
}

function loadGis() {
    return new Promise((resolve) => {
        const script = document.createElement('script');
        script.src = 'https://accounts.google.com/gsi/client';
        script.onload = () => {
            tokenClient = google.accounts.oauth2.initTokenClient({
                client_id: CLIENT_ID,
                scope: SCOPES,
                callback: '', // defined at request time
            });
            gisInited = true;
            resolve();
        };
        document.body.appendChild(script);
    });
}

export async function exportToGoogleDocs(voyageId) {
    if (!gapiInited || !gisInited) {
        await initGoogleAuth();
    }

    // Pre-open the window to satisfy popup blockers
    const newWindow = window.open('about:blank', '_blank');
    if (!newWindow) {
        throw new Error('Popup blocked! Please allow popups for this site.');
    }
    newWindow.document.write('Loading your Google Doc...');

    return new Promise((resolve, reject) => {
        tokenClient.callback = async (resp) => {
            if (resp.error) {
                newWindow.close();
                reject(resp);
                return;
            }
            try {
                await executeExport(voyageId, newWindow);
                resolve();
            } catch (err) {
                newWindow.close();
                reject(err);
            }
        };

        if (gapi.client.getToken() === null) {
            // Prompt the user to select a Google Account and ask for consent to share their data
            // when establishing a new session.
            tokenClient.requestAccessToken({prompt: 'consent'});
        } else {
            // Skip display of account chooser and consent dialog for an existing session.
            tokenClient.requestAccessToken({prompt: ''});
        }
    });
}

import { API } from './api.js';

async function executeExport(voyageId, newWindow) {
    // 1. Get the Blueprint from Backend
    const blueprint = await API.exportVoyage(voyageId);
    console.log('Blueprint received:', blueprint);

    // 2. Create the Document
    const createResp = await gapi.client.docs.documents.create({
        title: blueprint.title
    });
    const docId = createResp.result.documentId;
    console.log('Document created:', docId);

    // 3. Populate Content
    if (blueprint.requests && blueprint.requests.length > 0) {
        await gapi.client.docs.documents.batchUpdate({
            documentId: docId,
            resource: {
                requests: blueprint.requests
            }
        });
        console.log('Content populated');
    }
    
    // 4. Redirect the pre-opened window
    newWindow.location.href = `https://docs.google.com/document/d/${docId}`;
}
