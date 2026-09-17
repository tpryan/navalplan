import { API } from '../js/api.js';

describe('API Service', () => {
  beforeEach(() => {
    spyOn(window, 'fetch');
  });

  it('getPerson should fetch person data', async () => {
    const mockPerson = { id: 1, name: 'Test User', picture_url: 'http://example.com/pic.jpg' };
    
    // Mock successful fetch response
    window.fetch.and.resolveTo({
      ok: true,
      status: 200,
      json: async () => mockPerson
    });

    const person = await API.getPerson();
    
    // API.getPerson calls apiFetch which adds some default options and suppresses redirects for this call
    expect(window.fetch).toHaveBeenCalledWith('/api/v1/person', jasmine.objectContaining({}));
    expect(person).toEqual(mockPerson);
  });

  it('getVoyages should fetch voyages', async () => {
    const mockVoyages = [{ id: 101, name: 'Atlantic Crossing' }];
    
    window.fetch.and.resolveTo({
      ok: true,
      status: 200,
      json: async () => mockVoyages
    });

    const voyages = await API.getVoyages();
    
    expect(window.fetch).toHaveBeenCalledWith('/api/v1/voyages?page=1&limit=20', jasmine.any(Object));
    expect(voyages).toEqual(mockVoyages);
  });

  describe('GPX Track API', () => {
    it('listVoyageTracks should fetch tracks for voyage', async () => {
      const mockTracks = [{ id: 'track-123', name: 'Planned Leg 1', kind: 'planned' }];
      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => mockTracks
      });

      const res = await API.listVoyageTracks(101);
      expect(window.fetch).toHaveBeenCalledWith('/api/v1/voyages/101/track', jasmine.any(Object));
      expect(res).toEqual(mockTracks);
    });

    it('uploadVoyageTrack should post track data with multipart form', async () => {
      const mockTrack = { id: 'track-456', name: 'Recorded Passage', kind: 'recorded' };
      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => mockTrack
      });

      const blob = new Blob(['<gpx></gpx>'], { type: 'application/gpx+xml' });
      const res = await API.uploadVoyageTrack(101, blob, 'recorded', true);

      expect(window.fetch).toHaveBeenCalledWith(
        '/api/v1/voyages/101/track',
        jasmine.objectContaining({
          method: 'POST',
          body: jasmine.any(FormData)
        })
      );
      expect(res).toEqual(mockTrack);
    });

    it('uploadStopTrack should post stop-level track data', async () => {
      const mockTrack = { id: 'track-789', kind: 'planned', voyage_stop_id: 5 };
      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => mockTrack
      });

      const blob = new Blob(['<gpx></gpx>'], { type: 'application/gpx+xml' });
      const res = await API.uploadStopTrack(101, 5, blob, 'planned');

      expect(window.fetch).toHaveBeenCalledWith(
        '/api/v1/voyages/101/stops/5/track',
        jasmine.objectContaining({
          method: 'POST',
          body: jasmine.any(FormData)
        })
      );
      expect(res).toEqual(mockTrack);
    });

    it('deleteVoyageTrack should issue DELETE request', async () => {
      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => ({ message: 'deleted' })
      });

      const success = await API.deleteVoyageTrack(101, 'track-123');
      expect(window.fetch).toHaveBeenCalledWith(
        '/api/v1/voyages/101/track/track-123',
        jasmine.objectContaining({ method: 'DELETE' })
      );
      expect(success).toBe(true);
    });

    it('debriefVoyageTrack should trigger post-voyage pilot debrief', async () => {
      const mockDebrief = {
        summary: 'Excellent passage with 8.2% tacking overhead',
        recorded_distance_nm: 24.5,
        planned_distance_nm: 22.6,
        distance_variance_pct: 8.4,
        average_speed_kts: 6.2,
        observations: ['Good windward performance']
      };

      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => ({ track_id: 'track-123', debrief: mockDebrief })
      });

      const res = await API.debriefVoyageTrack(101, 'track-123');
      expect(window.fetch).toHaveBeenCalledWith(
        '/api/v1/voyages/101/track/track-123/debrief',
        jasmine.objectContaining({ method: 'POST' })
      );
      expect(res.debrief.recorded_distance_nm).toBe(24.5);
    });

    it('getPublicVoyageTracks should fetch public tracks using token', async () => {
      const mockTracks = [{ id: 'pub-track', kind: 'planned' }];
      window.fetch.and.resolveTo({
        ok: true,
        status: 200,
        json: async () => mockTracks
      });

      const res = await API.getPublicVoyageTracks('share-token-xyz');
      expect(window.fetch).toHaveBeenCalledWith('/api/v1/public/voyages/share-token-xyz/track', jasmine.any(Object));
      expect(res).toEqual(mockTracks);
    });
  });
});
