const API_BASE = '/api/v1';

export const API = {
  async getVoyages() {
    const res = await fetch(`${API_BASE}/voyages`);
    if (!res.ok) throw new Error('Failed to load voyages');
    return res.json();
  },

  async createVoyage(voyage) {
    const res = await fetch(`${API_BASE}/voyages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(voyage),
    });
    if (!res.ok) throw new Error('Failed to create voyage');
    return res.json();
  },

  async getVoyage(id) {
    const res = await fetch(`${API_BASE}/voyages/${id}`);
    if (!res.ok) throw new Error('Failed to load voyage');
    return res.json();
  },
  
  async getStops(voyageId) {
    const res = await fetch(`${API_BASE}/voyages/${voyageId}/stops`);
    if (!res.ok) throw new Error('Failed to load stops');
    return res.json();
  }
};
