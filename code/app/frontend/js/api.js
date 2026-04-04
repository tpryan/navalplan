export const API_BASE = '/api/v1';

async function apiFetch(url, options = {}) {
  const headers = {
    'X-Requested-With': 'XMLHttpRequest',
    ...options.headers
  };
  const res = await fetch(url, { ...options, headers });
  if (res.status === 401) {
    throw new Error('Unauthorized');
  }
  return res;
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
    if (!res.ok) throw new Error('Failed to trigger research');
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
    if (!res.ok) throw new Error('Failed to trigger guide research');
    return res.json();
  },

  async triggerFullResearch(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/research`, { method: 'POST' });
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

  
