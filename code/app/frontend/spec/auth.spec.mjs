import { checkSession } from '../js/auth.js';
import { API } from '../js/api.js';

describe('Auth Service', () => {
  let authContainer, sidebar, mapContainer;

  beforeEach(() => {
    // Setup mock DOM elements that auth.js expects
    authContainer = document.createElement('div');
    authContainer.id = 'auth-container';
    document.body.appendChild(authContainer);

    sidebar = document.createElement('div');
    sidebar.id = 'sidebar';
    // Start hidden as per initial state often expected
    sidebar.classList.add('hidden');
    document.body.appendChild(sidebar);

    mapContainer = document.createElement('div');
    mapContainer.id = 'map-container';
    mapContainer.classList.add('hidden');
    document.body.appendChild(mapContainer);

    // Spy on API methods
    spyOn(API, 'getPerson');
  });

  afterEach(() => {
    // Cleanup DOM to avoid polluting other tests
    if (document.body.contains(authContainer)) document.body.removeChild(authContainer);
    if (document.body.contains(sidebar)) document.body.removeChild(sidebar);
    if (document.body.contains(mapContainer)) document.body.removeChild(mapContainer);
  });

  it('checkSession should show app content when user is logged in', async () => {
    const mockPerson = { id: 1, name: 'Alice', picture_url: 'pic.jpg' };
    API.getPerson.and.resolveTo(mockPerson);

    await checkSession();

    // UI should be revealed
    expect(sidebar.classList.contains('hidden')).toBeFalse();
    expect(mapContainer.classList.contains('hidden')).toBeFalse();
    
    // Auth container should show user menu
    expect(authContainer.innerHTML).toContain('Alice');
    expect(authContainer.innerHTML).toContain('logout');
  });

  it('checkSession should show login screen when user is not logged in', async () => {
    // API returns null or undefined when no session
    API.getPerson.and.resolveTo(null);

    await checkSession();

    // UI should NOT remain hidden (allow exploration)
    expect(sidebar.classList.contains('hidden')).toBeFalse();
    expect(mapContainer.classList.contains('hidden')).toBeFalse();
    
    // Auth container should show login button
    expect(authContainer.innerHTML).toContain('Login');
  });

  it('checkSession should handle API errors gracefully (treat as logout)', async () => {
    API.getPerson.and.rejectWith(new Error('Network error'));

    await checkSession();

    // Should default to logout state
    expect(sidebar.classList.contains('hidden')).toBeFalse();
    expect(mapContainer.classList.contains('hidden')).toBeFalse();
    expect(authContainer.innerHTML).toContain('Login');
  });
});
