export const API_BASE = '/api/v1';

let error503Count = 0;
let last503Time = 0;
let on503Callback = null;

export function set503Callback(cb) {
  on503Callback = cb;
}

async function apiFetch(url, options = {}) {
  const headers = {
    'X-Requested-With': 'XMLHttpRequest',
    ...options.headers
  };

  const maxRetries = 2;
  let attempt = 0;

  while (attempt <= maxRetries) {
    try {
      const res = await fetch(url, { ...options, headers });
      
      if (res.status === 401) {
        throw new Error('Unauthorized');
      }

      if (res.status === 503 || (res.status === 500 && attempt < maxRetries)) {
        // Check if it's a 503 or a 500 that might be a transient model error
        // Note: The log showed 500 from agent, but the message was 503.
        
        error503Count++;
        last503Time = Date.now();
        
        if (on503Callback) {
            on503Callback(error503Count);
        }

        if (attempt < maxRetries) {
            attempt++;
            const delay = Math.pow(2, attempt) * 1000; // 2s, 4s backoff
            console.warn(`API: 503/500 detected. Retrying in ${delay}ms... (Attempt ${attempt})`);
            await new Promise(r => setTimeout(r, delay));
            continue;
        }
      }

      // Reset count on success if enough time has passed
      if (res.ok && Date.now() - last503Time > 30000) {
        error503Count = 0;
      }

      return res;
    } catch (err) {
      if (err.message === 'Unauthorized') throw err;
      if (attempt >= maxRetries) throw err;
      
      attempt++;
      await new Promise(r => setTimeout(r, 1000 * attempt));
    }
  }
}

export const API = {
  async getPerson() {
    const res = await apiFetch(`${API_BASE}/person`);
    if (!res.ok) throw new Error('Failed to load person');
    return res.json();
  },

  async updatePerson(person) {
    const res = await apiFetch(`${API_BASE}/person`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(person),
    });
    if (!res.ok) throw new Error('Failed to update person');
    return res.json();
  },

  async getVoyages(page = 1, limit = 20) {
    const res = await apiFetch(`${API_BASE}/voyages?page=${page}&limit=${limit}`);
    if (!res.ok) throw new Error('Failed to load voyages');
    return res.json();
  },

  async getVoyage(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}`);
    if (!res.ok) throw new Error('Failed to load voyage');
    return res.json();
  },

  async createVoyage(voyage) {
    const res = await apiFetch(`${API_BASE}/voyages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(voyage),
    });
    if (!res.ok) throw new Error('Failed to create voyage');
    return res.json();
  },

  async updateVoyage(id, voyage) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(voyage),
    });
    if (!res.ok) throw new Error('Failed to update voyage');
    return res.json();
  },

  async deleteVoyage(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}`, {
      method: 'DELETE',
    });
    if (!res.ok) throw new Error('Failed to delete voyage');
    return res.json();
  },

  async triggerResearch(stopId) {
    const res = await apiFetch(`${API_BASE}/stops/${stopId}/research`, { method: 'POST' });
    if (res.status === 409) { const e = new Error('Research already in progress for this stop'); e.conflict = true; throw e; }
    if (!res.ok) throw new Error('Failed to trigger research');
    return res.json();
  },

  async updateVoyageWeather(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/weather`, { method: 'POST' });
    if (!res.ok) throw new Error('Failed to trigger weather update');
    return res.json();
  },

  async triggerVoyageLookout(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/lookout`, { method: 'POST' });
    if (res.status === 409) { const e = new Error('Safety audit already in progress'); e.conflict = true; throw e; }
    if (!res.ok) throw new Error('Failed to trigger voyage safety audit');
    return res.json();
  },

  async triggerLookoutAudit(stopId) {
    const res = await apiFetch(`${API_BASE}/stops/${stopId}/lookout`, { method: 'POST' });
    if (res.status === 404) { const e = new Error('No briefing found — run research first'); e.noBriefing = true; throw e; }
    if (res.status === 409) { const e = new Error('Safety audit already in progress'); e.conflict = true; throw e; }
    if (!res.ok) throw new Error('Failed to trigger safety audit');
    return res.json();
  },

	async getBriefing(stopId) {
		const res = await apiFetch(`${API_BASE}/stops/${stopId}/briefing`);
		if (res.status === 404) return null;
		if (!res.ok) throw new Error('Failed to get briefing');
		return res.json();
	},

	async getVoyageBriefings(voyageId) {
		const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/briefings`);
		if (!res.ok) throw new Error('Failed to get voyage briefings');
		const data = await res.json();
		return data || [];
	},

	  async getStops(voyageId, page = 1, limit = 50) {
	    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/stops?page=${page}&limit=${limit}`);
	    if (!res.ok) throw new Error('Failed to load stops');
	    const data = await res.json();
	    return data || [];
	  },
  async createStop(voyageId, stop) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/stops`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(stop),
    });
    if (!res.ok) throw new Error('Failed to create stop');
    return res.json();
  },

  async updateStop(stopId, stop) {
    const res = await apiFetch(`${API_BASE}/stops/${stopId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(stop),
    });
    if (!res.ok) throw new Error('Failed to update stop');
    return res.json();
  },

  async deleteStop(stopId) {
    const res = await apiFetch(`${API_BASE}/stops/${stopId}`, {
      method: 'DELETE',
    });
    if (!res.ok) throw new Error('Failed to delete stop');
    return res.json();
  },

  async triggerVoyageGuideResearch(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/research_guide`, { method: 'POST' });
    if (res.status === 409) { const e = new Error('Guide research already in progress for this voyage'); e.conflict = true; throw e; }
    if (!res.ok) throw new Error('Failed to trigger guide research');
    return res.json();
  },

  async triggerFullResearch(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/research`, { method: 'POST' });
    if (res.status === 409) { const e = new Error('Full voyage research is already in progress'); e.conflict = true; throw e; }
    if (!res.ok) throw new Error('Failed to trigger full research');
    return res.json();
  },

  async getRecommendations(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/recommendations`);
    if (!res.ok) throw new Error('Failed to load recommendations');
    return res.json();
  },

  async generateRecommendations(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/recommendations/generate`, { method: 'POST' });
    if (!res.ok) throw new Error('Failed to generate recommendations');
    return res.json();
  },

  async getVoyageGuide(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}/guide`);
    if (res.status === 404) return null;
    if (!res.ok) throw new Error('Failed to get voyage guide');
    return res.json();
  },

  async getPilotReport(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}/pilot_report`);
    if (res.status === 404) throw new Error('Voyage not found');
    if (!res.ok) throw new Error('Failed to get pilot report');
    return res.json();
  },

  async getPublicVoyageGuide(token) {
    const res = await apiFetch(`${API_BASE}/public/voyages/${token}/guide`);
    if (!res.ok) throw new Error('Failed to load public guide');
    return res.json();
  },

  async enableSharing(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}/share`, { method: 'POST' });
    if (!res.ok) throw new Error('Failed to enable sharing');
    return res.json();
  },

  async disableSharing(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}/share`, { method: 'DELETE' });
    if (!res.ok) throw new Error('Failed to disable sharing');
    return res.json();
  },

  async uploadVoyageMap(id, imageBlob) {
    const formData = new FormData();
    formData.append('image', imageBlob);
    
    const res = await apiFetch(`${API_BASE}/voyages/${id}/guide/snapshot`, {
      method: 'POST',
      body: formData
    });
    
    if (!res.ok) throw new Error('Failed to upload map image');
    return res.json();
  },

  async logout() {
    await apiFetch('/auth/logout', { method: 'POST' });
  },

  async getDiscoveryRegions(month) {
    const res = await apiFetch(`${API_BASE}/discovery/regions?month=${month}`);
    if (!res.ok) throw new Error('Failed to load discovery regions');
    return res.json();
  },

      async triggerDiscoveryMining(month) {

          const res = await apiFetch(`${API_BASE}/discovery/mine?month=${month}`, { method: 'POST' });

          if (!res.ok) throw new Error('Failed to trigger discovery mining');

          return res;

      },

  

      async deleteDiscoverySeasonality(regionID, month) {

          const res = await apiFetch(`${API_BASE}/discovery/regions/${regionID}/months/${month}`, { method: 'DELETE' });

          if (!res.ok) throw new Error('Failed to delete discovery seasonality');

          return res;

      },

    // Admin
    async listAdminUsers(page = 1, limit = 20) {
        const res = await apiFetch(`/api/admin/users?page=${page}&limit=${limit}`);
        if (!res.ok) throw new Error('Failed to load users');
        return res.json();
    },

    async inviteUser(email) {
        const res = await apiFetch('/api/admin/invite', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email })
        });
        if (!res.ok) throw new Error('Failed to invite user');
        return res.json();
    },

    async revokeInvitation(email) {
        const res = await apiFetch(`/api/admin/invite/${encodeURIComponent(email)}`, {
            method: 'DELETE'
        });
        if (!res.ok) throw new Error('Failed to revoke invitation');
        return true;
    },

    streamProgress(sessionID, onProgress) {
    console.log('[progress] connecting', sessionID);
    const es = new EventSource(`${API_BASE}/progress/stream?session_id=${encodeURIComponent(sessionID)}`);
    es.addEventListener('progress', (e) => {
      try {
        const evt = JSON.parse(e.data);
        console.log('[progress]', evt.stage, evt.message);
        onProgress(evt);
      } catch (_) { /* ignore parse errors */ }
    });
    es.onerror = (err) => {
      console.warn('[progress] stream error, closing', err);
      es.close();
    };
    return es;
  },

  async checkHealth() {
        try {
            const res = await fetch('/health');
            if (res.ok) return { ok: true };
            
            const text = await res.text();
            return { ok: false, status: res.status, message: text };
        } catch (e) {
            return { ok: false, status: 0, message: 'Backend Connection Lost' };
        }
    }
  };
