const API_BASE = '/api/v1';

async function apiFetch(url, options = {}, suppressAuthRedirect = false) {
  const res = await fetch(url, options);
  if (res.status === 401) {
    if (!suppressAuthRedirect) {
        window.location.href = '/auth/google/login'; 
    }
    throw new Error('Unauthorized');
  }
  return res;
}

export const API = {
  async getPerson() {
    // Suppress redirect so checkSession can handle UI state
    const res = await apiFetch(`${API_BASE}/person`, {}, true);
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
		return res.json();
	},

	  async getStops(voyageId, page = 1, limit = 50) {
	    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/stops?page=${page}&limit=${limit}`);
	    if (!res.ok) throw new Error('Failed to load stops');
	    return res.json();
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

  async getVoyageGuide(id) {
    const res = await apiFetch(`${API_BASE}/voyages/${id}/guide`);
    if (res.status === 404) return null;
    if (!res.ok) throw new Error('Failed to get voyage guide');
    return res.json();
  },

  async uploadVoyageMap(id, imageBlob) {
    const formData = new FormData();
    formData.append('image', imageBlob);
    
    const res = await apiFetch(`${API_BASE}/voyages/${id}/guide/map_image`, {
      method: 'POST',
      body: formData
    });
    
    if (!res.ok) throw new Error('Failed to upload map image');
    return res.json();
  },

  async exportVoyage(voyageId) {
    const res = await apiFetch(`${API_BASE}/voyages/${voyageId}/export`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'docs' })
    });
    if (!res.ok) throw new Error('Failed to export voyage');
    return res.json();
  }
};
