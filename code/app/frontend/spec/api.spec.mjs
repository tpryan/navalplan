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
});
