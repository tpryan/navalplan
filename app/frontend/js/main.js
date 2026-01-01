import mapboxgl from 'mapbox-gl';
import Chart from 'chart.js/auto';
import { API } from './api.js';
import { exportToGoogleDocs } from './google_export.js';
import { checkSession } from './auth.js';

// Configuration
const MAPBOX_TOKEN = __MAPBOX_TOKEN__; 

// State
let voyages = [];
let currentVoyage = null;
let currentStops = [];
let selectedDate = null;
let map = null;
let markers = [];
let editingVoyageId = null;

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  checkSession();
  initMap();
  initUI();
  loadVoyages();
}

function initUI() {
  const btnNewVoyage = document.getElementById('btn-new-voyage');
  const modalOverlay = document.getElementById('modal-overlay');
  const modalNewVoyage = document.getElementById('modal-new-voyage');
  const btnCancelVoyage = document.getElementById('btn-cancel-voyage');
  const formNewVoyage = document.getElementById('form-new-voyage');
  const btnBack = document.getElementById('btn-back-voyages');
  const btnExport = document.getElementById('btn-export-voyage');
  const btnEditVoyage = document.getElementById('btn-edit-voyage');

  const btnUseMapCenter = document.getElementById('btn-use-map-center');
  const displayCoords = document.getElementById('voyage-coords-display');
  const inputLat = document.getElementById('voyage-lat');
  const inputLng = document.getElementById('voyage-lng');
  const modalTitle = modalNewVoyage.querySelector('h2');
  const submitBtn = formNewVoyage.querySelector('button[type="submit"]');

  // Open Modal (Create Mode)
  btnNewVoyage.addEventListener('click', () => {
    editingVoyageId = null;
    modalTitle.textContent = 'Plan a New Voyage';
    submitBtn.textContent = 'Create Voyage';
    modalOverlay.classList.remove('hidden');
    modalNewVoyage.classList.remove('hidden');
    // Set default dates
    const today = new Date().toISOString().split('T')[0];
    document.getElementById('voyage-start').value = today;
    document.getElementById('voyage-end').value = '';
    document.getElementById('voyage-title').value = '';
    document.getElementById('voyage-location-name').value = '';
    displayCoords.textContent = '';
    inputLat.value = '';
    inputLng.value = '';
  });

  // Close Modal Helper
  const closeModal = () => {
    modalOverlay.classList.add('hidden');
    modalNewVoyage.classList.add('hidden');
    formNewVoyage.reset();
    displayCoords.textContent = '';
    inputLat.value = '';
    inputLng.value = '';
    editingVoyageId = null;
  };

  btnCancelVoyage.addEventListener('click', closeModal);
  modalOverlay.addEventListener('click', closeModal);

  // Auto-set End Date
  const inputStart = document.getElementById('voyage-start');
  const inputEnd = document.getElementById('voyage-end');

  inputStart.addEventListener('change', () => {
    if (inputStart.value && !inputEnd.value) {
      const d = new Date(inputStart.value);
      d.setUTCDate(d.getUTCDate() + 1);
      inputEnd.value = d.toISOString().split('T')[0];
    }
  });

  // Use Map Center
  if (btnUseMapCenter) {
    btnUseMapCenter.addEventListener('click', () => {
      if (!map) return;
      const center = map.getCenter();
      inputLat.value = center.lat;
      inputLng.value = center.lng;
      displayCoords.textContent = `Lat: ${center.lat.toFixed(4)}, Lng: ${center.lng.toFixed(4)}`;
    });
  }

  // Handle Form Submit
  formNewVoyage.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(formNewVoyage);
    const voyageData = {
      title: formData.get('title'),
      start_date: formData.get('start_date') + 'T00:00:00Z',
      end_date: formData.get('end_date') + 'T00:00:00Z',
      location_name: formData.get('location_name'),
      latitude: formData.get('latitude') ? parseFloat(formData.get('latitude')) : null,
      longitude: formData.get('longitude') ? parseFloat(formData.get('longitude')) : null
    };

    try {
      let savedVoyage;
      if (editingVoyageId) {
        savedVoyage = await API.updateVoyage(editingVoyageId, voyageData);
      } else {
        savedVoyage = await API.createVoyage(voyageData);
      }
      closeModal();
      loadVoyages(); // Refresh list

      // If we are currently viewing this voyage, refresh the view
      if (currentVoyage && currentVoyage.id === savedVoyage.id) {
        selectVoyage(savedVoyage);
      }
    } catch (err) {
      console.error(err);
      alert('Failed to save voyage. Check console.');
    }
  });

  // Back Button
  if (btnBack) {
    btnBack.addEventListener('click', showVoyageList);
  }

  // Export Button
  if (btnExport) {
    btnExport.addEventListener('click', handleShowReport);
  }

  // Guide Button
  const btnViewGuide = document.getElementById('btn-view-guide');
  if (btnViewGuide) {
      btnViewGuide.addEventListener('click', () => {
          if (currentVoyage) {
              handleGuideClick(currentVoyage, btnViewGuide);
          }
      });
  }

  // Capture Map Button
  const btnCaptureMap = document.getElementById('btn-capture-map');
  if (btnCaptureMap) {
      btnCaptureMap.addEventListener('click', async () => {
          if (!currentVoyage || !map) return;
          
          const originalContent = btnCaptureMap.innerHTML;
          btnCaptureMap.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
          btnCaptureMap.disabled = true;

          try {
              // Create blob from map canvas
              map.getCanvas().toBlob(async (blob) => {
                  if (!blob) {
                      throw new Error('Failed to generate map image');
                  }
                  
                  try {
                      await API.uploadVoyageMap(currentVoyage.id, blob);
                      showNotification('Map Captured', 'Current map view has been saved to the voyage report.');
                  } catch (err) {
                      console.error(err);
                      alert('Failed to upload map image.');
                  } finally {
                      btnCaptureMap.innerHTML = originalContent;
                      btnCaptureMap.disabled = false;
                  }
              });
          } catch (err) {
              console.error(err);
              btnCaptureMap.innerHTML = originalContent;
              btnCaptureMap.disabled = false;
              alert('Failed to capture map.');
          }
      });
  }

  // Research All Button
  const btnResearchAll = document.getElementById('btn-research-all');
  if (btnResearchAll) {
      btnResearchAll.addEventListener('click', async () => {
          if (!currentVoyage) return;
          if (confirm('This will trigger research for the entire voyage and all stops. Continue?')) {
              const originalContent = btnResearchAll.innerHTML;
              btnResearchAll.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
              btnResearchAll.disabled = true;
              
              // 1. Visual Indicators: Spin all microscope icons
              const researchBtns = document.querySelectorAll('.day-actions .research');
              researchBtns.forEach(btn => {
                  if (!btn.querySelector('.spin')) { // Don't double spin if already spinning
                     btn.dataset.originalContent = btn.innerHTML;
                     btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
                     btn.disabled = true;
                  }
              });

              try {
                  await API.triggerFullResearch(currentVoyage.id);
                  showNotification('Research Started', 'Full voyage research has started. The agent is analyzing the destination guide and all stops in the background.');
                  
                  // 2. Poll for completion
                  const startTime = Date.now();
                  const TIMEOUT_MS = 120000; // 2 minutes timeout
                  
                  const pendingStops = [...currentStops]; // Clone
                  let guideComplete = false;
                  
                  const poll = setInterval(async () => {
                      // Check timeout
                      if (Date.now() - startTime > TIMEOUT_MS) {
                          clearInterval(poll);
                          btnResearchAll.innerHTML = originalContent;
                          btnResearchAll.disabled = false;
                          // Revert stuck spinners
                          researchBtns.forEach(btn => {
                             if (btn.disabled) {
                                 btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
                                 btn.disabled = false;
                             }
                          });
                          showNotification('Research Timeout', 'Research is taking longer than expected. Please check individual stops.');
                          return;
                      }

                      try {
                          // Check Guide
                          if (!guideComplete) {
                              const g = await API.getVoyageGuide(currentVoyage.id);
                              if (g) guideComplete = true;
                          }

                          // Check Stops
                          // We iterate backwards to remove completed ones
                          for (let i = pendingStops.length - 1; i >= 0; i--) {
                              const stop = pendingStops[i];
                              const b = await API.getBriefing(stop.id);
                              if (b) {
                                  // Find button and update
                                  // We can't easily query by ID unless we add ID to button, but we can rely on DOM order if stable
                                  // Better: Find stop in currentStops to get index?
                                  // For now, let's just mark the stop as done.
                                  // To update UI, we re-render itinerary? That might be disruptive.
                                  // Let's just find the button row.
                                  // Implementation Detail: In renderItinerary, we didn't add IDs to buttons.
                                  // We can assume renderItinerary hasn't changed structure.
                                  pendingStops.splice(i, 1);
                              }
                          }

                          // If all done
                          if (guideComplete && pendingStops.length === 0) {
                              clearInterval(poll);
                              btnResearchAll.innerHTML = originalContent;
                              btnResearchAll.disabled = false;
                              
                              // Re-render to show normal buttons (microscopes) or maybe checkmarks?
                              // Simple approach: re-render itinerary to reset buttons to interactive state
                              renderItinerary(); 
                              renderMapStops();
                              
                              showNotification('Research Complete', 'All research tasks have been completed successfully.');
                          }

                      } catch (err) {
                          console.error("Polling error", err);
                      }
                  }, 4000); // Poll every 4 seconds

              } catch (err) {
                  console.error(err);
                  btnResearchAll.innerHTML = originalContent;
                  btnResearchAll.disabled = false;
                  // Revert spinners
                  researchBtns.forEach(btn => {
                     btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
                     btn.disabled = false;
                  });
                  alert('Failed to trigger research.');
              }
          }
      });
  }

  // Edit Voyage Button (Itinerary View)
  if (btnEditVoyage) {
    btnEditVoyage.addEventListener('click', () => {
      if (currentVoyage) {
        openEditModal(currentVoyage);
      }
    });
  }

  // Report Modal Close Handler
  const modalReport = document.getElementById('modal-report');
  const btnCloseReport = document.getElementById('btn-close-report');
  const btnCopyReport = document.getElementById('btn-copy-report');
  
  const closeReport = () => {
      modalReport.classList.add('hidden');
      if (document.getElementById('modal-new-voyage').classList.contains('hidden')) {
          modalOverlay.classList.add('hidden');
      }
  };
  
  if (btnCloseReport) {
      btnCloseReport.onclick = closeReport;
  }

  // Notification Modal Handlers
  const modalNotification = document.getElementById('modal-notification');
  const btnCloseNotification = document.getElementById('btn-close-notification');
  
  if (btnCloseNotification) {
      btnCloseNotification.onclick = () => {
          modalNotification.classList.add('hidden');
          const modalOverlay = document.getElementById('modal-overlay');
          // Only hide overlay if no other modal is open
          if (document.getElementById('modal-new-voyage').classList.contains('hidden') &&
              document.getElementById('modal-briefing').classList.contains('hidden') && 
              document.getElementById('modal-report').classList.contains('hidden') &&
              document.getElementById('modal-guide').classList.contains('hidden')) {
              modalOverlay.classList.add('hidden');
          }
      };
  }

  // Guide Modal Close Handler
  const modalGuide = document.getElementById('modal-guide');
  const btnCloseGuide = document.getElementById('btn-close-guide');

  const closeGuide = () => {
      modalGuide.classList.add('hidden');
      if (document.getElementById('modal-new-voyage').classList.contains('hidden')) {
          modalOverlay.classList.add('hidden');
      }
  };
  
  if (btnCloseGuide) btnCloseGuide.onclick = closeGuide;
  
  if (btnCopyReport) {
    btnCopyReport.onclick = async () => {
        const content = document.getElementById('report-content');
        const originalText = btnCopyReport.textContent;
        btnCopyReport.textContent = 'Processing...';
        btnCopyReport.disabled = true;

        // 0. Convert Remote Images (like the Map) to Data URIs
        const remoteImages = content.querySelectorAll('img');
        const processedImages = [];
        
        for (const img of remoteImages) {
             // Skip if already data URI
             if (img.src.startsWith('data:')) continue;
             
             try {
                 const resp = await fetch(img.src);
                 const blob = await resp.blob();
                 const dataUrl = await new Promise(resolve => {
                     const reader = new FileReader();
                     reader.onload = () => resolve(reader.result);
                     reader.readAsDataURL(blob);
                 });
                 
                 processedImages.push({ el: img, src: img.src });
                 img.src = dataUrl;
             } catch (err) {
                 console.warn('Failed to embed image:', img.src, err);
             }
        }
        
        // 1. Convert Canvases to Images
        const canvases = content.querySelectorAll('canvas');
        const tempImages = [];
        
        canvases.forEach(canvas => {
            const img = document.createElement('img');
            img.src = canvas.toDataURL();
            img.style.width = '100%';
            img.style.height = 'auto';
            
            // Insert image, hide canvas
            canvas.parentNode.insertBefore(img, canvas);
            canvas.style.display = 'none';
            tempImages.push({ canvas, img });
        });

        // 2. Remove Icons (to avoid copying their text)
        const icons = content.querySelectorAll('.material-symbols-outlined');
        const removedIcons = [];
        icons.forEach(icon => {
            const placeholder = document.createComment('icon-placeholder');
            const parent = icon.parentNode;
            removedIcons.push({ icon, parent, next: icon.nextSibling, placeholder });
            parent.replaceChild(placeholder, icon);
        });

        // 2b. Convert Facility Lists to Divs with H4s
        const facilityLists = content.querySelectorAll('.facility-list');
        const modifiedLists = [];
        
        facilityLists.forEach(ul => {
            const container = document.createElement('div');
            const listItems = ul.querySelectorAll('li.facility-item');
            const originalItems = [];
            
            listItems.forEach(li => {
                const h4 = document.createElement('h4');
                // Move all children
                while (li.firstChild) {
                    h4.appendChild(li.firstChild);
                }
                container.appendChild(h4);
                originalItems.push({ li, h4 });
            });
            
            // Replace UL with Container
            ul.parentNode.insertBefore(container, ul);
            ul.style.display = 'none';
            
            modifiedLists.push({ ul, container, originalItems });
        });

        // 3. Strip Styles and Classes
        const allElements = content.querySelectorAll('*');
        const originalAttributes = [];
        
        allElements.forEach(el => {
            // Skip the temp images we just created
            if (tempImages.some(t => t.img === el)) return;
            // Skip the original ULs we just hid
            if (modifiedLists.some(m => m.ul === el)) return;

            originalAttributes.push({
                el: el,
                style: el.getAttribute('style'),
                class: el.getAttribute('class')
            });
            
            el.removeAttribute('style');
            el.removeAttribute('class');
        });

        // 3a. Apply specific clipboard styles (e.g. left-align headers)
        const ths = content.querySelectorAll('th');
        ths.forEach(th => {
            th.style.textAlign = 'left';
        });

        // 4. Select and Copy
        const range = document.createRange();
        range.selectNode(content);
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
        
        try {
            document.execCommand('copy');
            
            btnCopyReport.textContent = 'Copied!';
            setTimeout(() => btnCopyReport.textContent = originalText, 2000);
        } catch (err) {
            console.error('Failed to copy', err);
            alert('Failed to copy report to clipboard');
            btnCopyReport.textContent = originalText;
        } finally {
            // 5. Restore Attributes, Icons, Canvases, and Lists
            window.getSelection().removeAllRanges();
            
            // Restore attributes first
            originalAttributes.forEach(({ el, style, class: cls }) => {
                if (style !== null) el.setAttribute('style', style);
                else el.removeAttribute('style');

                if (cls !== null) el.setAttribute('class', cls);
                else el.removeAttribute('class');
            });

            // Restore Lists
            modifiedLists.forEach(({ ul, container, originalItems }) => {
                originalItems.forEach(({ li, h4 }) => {
                    while (h4.firstChild) {
                        li.appendChild(h4.firstChild);
                    }
                });
                container.remove();
                ul.style.display = '';
            });

            // Restore Icons
            removedIcons.forEach(({ icon, parent, placeholder }) => {
                parent.replaceChild(icon, placeholder);
            });

            // Restore images/canvases
            tempImages.forEach(({ canvas, img }) => {
                canvas.style.display = '';
                img.remove();
            });
            
            // Restore Remote Images
            processedImages.forEach(({ el, src }) => {
                el.src = src;
            });
            
            btnCopyReport.disabled = false;
        }
    };
  }
}

async function loadVoyages() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '<p class="loading-text">Loading voyages...</p>';

  try {
    voyages = await API.getVoyages();
    renderVoyageList();
  } catch (err) {
    console.error(err);
    listContainer.innerHTML = '<p class="loading-text error">Failed to load voyages.</p>';
  }
}

function renderVoyageList() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '';

  if (!voyages || voyages.length === 0) {
    listContainer.innerHTML = '<p class="loading-text">No voyages yet. Plan your first trip!</p>';
    return;
  }

  voyages.forEach(voyage => {
    const el = document.createElement('div');
    el.className = 'voyage-item';
    el.innerHTML = `
      <div class="voyage-info">
        <h3>${voyage.title}</h3>
        <p>${new Date(voyage.start_date).toLocaleDateString()} - ${new Date(voyage.end_date).toLocaleDateString()}</p>
        ${voyage.location_name ? '<p style="font-size:0.8rem; color:#888">📍 ' + voyage.location_name + '</p>' : ''}
      </div>
      <div class="voyage-actions">
        <button class="btn-icon edit" title="Edit">
          <span class="material-symbols-outlined">edit</span>
        </button>
        <button class="btn-icon delete" title="Delete">
          <span class="material-symbols-outlined">delete</span>
        </button>
      </div>
    `;
    
    // Select Voyage
    el.querySelector('.voyage-info').addEventListener('click', () => selectVoyage(voyage));

    // Edit Voyage
    const btnEdit = el.querySelector('.edit');
    btnEdit.addEventListener('click', (e) => {
      e.stopPropagation();
      openEditModal(voyage);
    });

    // Delete Voyage
    const btnDelete = el.querySelector('.delete');
    btnDelete.addEventListener('click', async (e) => {
      e.stopPropagation();
      if (confirm(`Are you sure you want to delete "${voyage.title}"?`)) {
        try {
          await API.deleteVoyage(voyage.id);
          loadVoyages();
          if (currentVoyage && currentVoyage.id === voyage.id) {
             showVoyageList(); // Reset view if we deleted the current voyage
          }
        } catch (err) {
          console.error(err);
          alert('Failed to delete voyage');
        }
      }
    });

    listContainer.appendChild(el);
  });
}

function openEditModal(voyage) {
    editingVoyageId = voyage.id;
    const modalOverlay = document.getElementById('modal-overlay');
    const modalNewVoyage = document.getElementById('modal-new-voyage');
    const modalTitle = modalNewVoyage.querySelector('h2');
    const submitBtn = document.querySelector('#form-new-voyage button[type="submit"]');

    modalTitle.textContent = 'Edit Voyage';
    submitBtn.textContent = 'Update Voyage';
    
    document.getElementById('voyage-title').value = voyage.title;
    document.getElementById('voyage-start').value = voyage.start_date.split('T')[0];
    document.getElementById('voyage-end').value = voyage.end_date.split('T')[0];
    document.getElementById('voyage-location-name').value = voyage.location_name || '';
    
    if (voyage.latitude != null && voyage.longitude != null) {
        document.getElementById('voyage-lat').value = voyage.latitude;
        document.getElementById('voyage-lng').value = voyage.longitude;
        document.getElementById('voyage-coords-display').textContent = `Lat: ${voyage.latitude.toFixed(4)}, Lng: ${voyage.longitude.toFixed(4)}`;
    } else {
        document.getElementById('voyage-lat').value = '';
        document.getElementById('voyage-lng').value = '';
        document.getElementById('voyage-coords-display').textContent = '';
    }

    modalOverlay.classList.remove('hidden');
    modalNewVoyage.classList.remove('hidden');
}

function showVoyageList() {
    document.getElementById('voyage-list').classList.remove('hidden');
    document.getElementById('itinerary-view').classList.add('hidden');
    document.querySelector('.sidebar-actions').classList.remove('hidden');
    
    currentVoyage = null;
    selectedDate = null;
    clearMap();
}

function updateItineraryHeader(voyage) {
    document.getElementById('itinerary-title').textContent = voyage.title;
    document.getElementById('itinerary-dates').textContent = `${new Date(voyage.start_date).toLocaleDateString()} - ${new Date(voyage.end_date).toLocaleDateString()}`;
}

async function selectVoyage(voyage) {
    currentVoyage = voyage;
    document.getElementById('voyage-list').classList.add('hidden');
    document.getElementById('itinerary-view').classList.remove('hidden');
    document.querySelector('.sidebar-actions').classList.add('hidden');

    updateItineraryHeader(voyage);

    // Load Stops
    try {
        currentStops = await API.getStops(voyage.id);
        renderItinerary();
        renderMapStops();

        if (map) {
            if (currentStops.length > 0) {
                const bounds = new mapboxgl.LngLatBounds();
                currentStops.forEach(stop => bounds.extend([stop.longitude, stop.latitude]));
                if (voyage.latitude != null && voyage.longitude != null) {
                    bounds.extend([voyage.longitude, voyage.latitude]);
                }
                map.fitBounds(bounds, { padding: 50, maxZoom: 12 });
            } else if (voyage.latitude != null && voyage.longitude != null) {
                map.flyTo({ center: [voyage.longitude, voyage.latitude], zoom: 9 });
            }
        }
    } catch (err) {
        console.error(err);
        alert('Failed to load stops');
    }
}

function renderItinerary() {
    const list = document.getElementById('itinerary-list');
    list.innerHTML = '';
    
    let currentDate = new Date(currentVoyage.start_date);
    const endDate = new Date(currentVoyage.end_date);

    while (currentDate <= endDate) {
        const dateStr = currentDate.toISOString().split('T')[0];
        const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
        
        const el = document.createElement('div');
        el.className = `day-item ${selectedDate === dateStr ? 'selected' : ''}`;
        
        // Day Info
        let html = `
            <div style="flex:1" class="day-info">
                <span class="day-date">${currentDate.toLocaleDateString(undefined, {month:'short', day:'numeric'})}</span>
                <span class="day-location ${stop ? 'set' : ''}">${stop ? stop.location_name : 'No destination'}</span>
            </div>
        `;
        
        // Research Action
        if (stop) {
            html += `
                <div class="day-actions">
                    <button class="btn-icon research" title="Research">
                        <span class="material-symbols-outlined">science</span>
                    </button>
                    <button class="btn-icon delete-stop" title="Delete Stop">
                        <span class="material-symbols-outlined">delete</span>
                    </button>
                </div>
            `;
        }
        
        el.innerHTML = html;
        
        // Handlers
        el.querySelector('.day-info').addEventListener('click', () => selectDate(dateStr));
        
        if (stop) {
            const btnResearch = el.querySelector('.research');
            btnResearch.addEventListener('click', (e) => {
                e.stopPropagation();
                handleResearchClick(stop, btnResearch);
            });

            const btnDelete = el.querySelector('.delete-stop');
            btnDelete.addEventListener('click', async (e) => {
                e.stopPropagation();
                if (confirm(`Remove stop at ${stop.location_name}?`)) {
                    try {
                        await API.deleteStop(stop.id);
                        currentStops = currentStops.filter(s => s.id !== stop.id);
                        renderItinerary();
                        renderMapStops();
                    } catch (err) {
                        console.error(err);
                        alert('Failed to delete stop');
                    }
                }
            });
        }

        list.appendChild(el);
        
        currentDate.setUTCDate(currentDate.getUTCDate() + 1);
    }
}

async function handleResearchClick(stop, button) {
    const originalContent = button.innerHTML;
    
    try {
        const existing = await API.getBriefing(stop.id);
        if (existing) {
            showBriefing(existing);
            return;
        }

        // Show Modal Immediately with loading state
        const modal = document.getElementById('modal-briefing');
        const content = document.getElementById('briefing-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = `
            <div style="text-align:center; padding:3rem; color: #666;">
                <span class="material-symbols-outlined spin" style="font-size: 3rem; margin-bottom: 1rem;">sync</span>
                <p><strong>Agent is researching...</strong></p>
                <p style="font-size: 0.9em;">Checking weather, tides, and local charts.</p>
            </div>
        `;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        await API.triggerResearch(stop.id);
        
        // Poll
        const poll = setInterval(async () => {
            try {
                const b = await API.getBriefing(stop.id);
                if (b) {
                    clearInterval(poll);
                    button.innerHTML = originalContent;
                    showBriefing(b); // Updates the already-open modal with data
                    renderMapStops();
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);
        
    } catch (err) {
        console.error(err);
        button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
        setTimeout(() => button.innerHTML = originalContent, 2000);
    }
}

function renderTideChart(canvasId, tideData, targetDateStr) {
    if (!tideData || !tideData.events) return;

    // Target Date Midnight (UTC)
    const targetDate = new Date(targetDateStr);
    const targetStart = new Date(targetDate).setUTCHours(0,0,0,0);

    // Parse Events
    // Data: { time: "YYYY-MM-DD HH:MM", height_ft: 1.2 }
    const points = [];
    tideData.events.forEach(e => {
        // The API returns GMT time in "YYYY-MM-DD HH:MM" format.
        // We append 'Z' to treat it as UTC.
        let timeStr = e.time;
        // Check if it has a timezone (Z or +HH:MM or -HH:MM)
        const hasTimezone = timeStr.endsWith('Z') || /[+-]\d{2}:?\d{2}$/.test(timeStr);
        if (!hasTimezone) {
             timeStr = timeStr.replace(' ', 'T') + 'Z';
        }

        let d = new Date(timeStr);
        
        if (!isNaN(d.getTime())) {
            // Calculate relative hour (-24 to +48 range is fine)
            // But for chart logic, simple float hours relative to midnight is best
            const diffMs = d.getTime() - targetStart;
            const floatHours = diffMs / (1000 * 60 * 60);
            points.push({ x: floatHours, y: e.height_ft });
        }
    });

    points.sort((a, b) => a.x - b.x);

    const ctx = document.getElementById(canvasId).getContext('2d');
    
    new Chart(ctx, {
        type: 'line',
        data: {
            datasets: [{
                label: 'Tide Height (ft)',
                data: points,
                borderColor: '#0077be',
                backgroundColor: 'rgba(0, 119, 190, 0.2)',
                borderWidth: 2,
                tension: 0.4, // Smooth Bezier
                pointRadius: 4,
                pointHoverRadius: 6,
                fill: 'start'
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    callbacks: {
                        title: (context) => {
                            const val = context[0].parsed.x;
                            const h = Math.floor(val);
                            const m = Math.round((val - h) * 60);
                            // Handle day overflow for label
                            let label = `${h.toString().padStart(2,'0')}:${m.toString().padStart(2,'0')}`;
                            if (h < 0) label += " (Prev Day)";
                            if (h >= 24) label += " (Next Day)";
                            return `Local Time: ${label}`;
                        }
                    }
                }
            },
            scales: {
                x: {
                    type: 'linear',
                    min: 0,
                    max: 24,
                    title: { display: true, text: 'Hour (Local Time)' },
                    ticks: {
                        stepSize: 3,
                        callback: (v) => {
                            if (v < 0 || v > 24) return '';
                            return `${v}:00`;
                        }
                    }
                },
                y: {
                    title: { display: true, text: 'Feet' },
                    grid: {
                        color: (context) => {
                            if (context.tick.value === 0) {
                                return '#333'; // Darker color for zero line
                            }
                            return 'rgba(0, 0, 0, 0.1)'; // Default grid color
                        },
                        lineWidth: (context) => {
                            if (context.tick.value === 0) {
                                return 2; // Thicker line for zero
                            }
                            return 1;
                        }
                    }
                }
            }
        }
    });
}

function showBriefing(briefing) {
    const modal = document.getElementById('modal-briefing');
    const content = document.getElementById('briefing-content');
    const btnClose = document.getElementById('btn-close-briefing');
    const btnRedo = document.getElementById('btn-redo-briefing');
    const modalOverlay = document.getElementById('modal-overlay');

    const isInvalid = (v) => {
        if (!v) return true;
        const sv = String(v).toLowerCase().trim();
        return sv === 'n/a' || sv === 'unknown' || sv === 'not specified';
    };

    const renderReferences = (refs) => {
        if (!refs || refs.length === 0) return '';
        return `<div style="font-size: 0.8em; margin-top: 0.3rem; color: #666;">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" style="margin-right:0.3rem">[${i+1}]</a>`).join('')}
        </div>`;
    };

    // Determine Target Date for Filtering & Charting
    const stop = currentStops.find(s => s.id === briefing.stop_id);
    const targetDateFull = stop ? stop.target_date : new Date().toISOString();
    const targetDateYMD = targetDateFull.split('T')[0]; // "YYYY-MM-DD"

    // Weather
    const weather = briefing.weather_summary || {};
    const weatherHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">${getIconForWeather(weather.condition)}</span>
                Weather
            </h3>
            <div class="weather-box">
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Summary</th>
                        <td class="briefing-td">${isInvalid(weather.summary) ? 'N/A' : weather.summary}</td>
                    </tr>
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Conditions</th>
                        <td class="briefing-td briefing-td-icon">
                            <span class="material-symbols-outlined" style="font-size: 1.2rem;">${getIconForWeather(weather.condition)}</span>
                            ${isInvalid(weather.condition) ? 'N/A' : weather.condition}
                        </td>
                    </tr>
                    ${(weather.temp_max_f || weather.temp_min_f) ? `
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Temp</th>
                        <td class="briefing-td">High: ${Math.round(weather.temp_max_f)}°F &nbsp;|&nbsp; Low: ${Math.round(weather.temp_min_f)}°F</td>
                    </tr>
                    ` : ''}
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Wind</th>
                        <td class="briefing-td">${isInvalid(weather.wind_direction) ? 'N/A' : weather.wind_direction} ${weather.wind_speed_kt || '0'} kt</td>
                    </tr>
                    ${weather.wave_height_ft > 0 ? `
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Waves</th>
                        <td class="briefing-td">${weather.wave_height_ft} ft</td>
                    </tr>
                    ` : ''}
                </table>
            </div>
        </div>
    `;

    // Sun Phase
    const sun = briefing.sun_phase || {};
    let sunHtml = '';
    if (sun.sunrise || sun.sunset) {
        // Format times to be more readable if they are full date strings
        const formatTime = (t) => {
            if (!t) return 'N/A';
            try {
                const d = new Date(t);
                if (isNaN(d.getTime())) return t;
                return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
            } catch (e) {
                return t;
            }
        };

        sunHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">wb_twilight</span>
                Sun Phase
            </h3>
            <div class="weather-box"> <!-- Reuse weather box style -->
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Sunrise</th>
                        <td class="briefing-td">${formatTime(sun.sunrise)}</td>
                    </tr>
                    <tr>
                        <th class="briefing-th briefing-table-label-width">Sunset</th>
                        <td class="briefing-td">${formatTime(sun.sunset)}</td>
                    </tr>
                </table>
            </div>
        </div>
        `;
    }

    // Tides
    // Filter events to only show the target date in the LIST
    const tides = briefing.tides || {};
    const allEvents = tides.events || [];
    const displayEvents = allEvents.filter(e => e.time.startsWith(targetDateYMD));
    
    // Fix: Define displayDateHeader
    const [y, m, d] = targetDateYMD.split('-');
    const displayDateHeader = `${m}/${d}/${y}`;

    const tideEventsHtml = displayEvents.map(e => {
        let timeStr = e.time;
        try {
            const d = new Date(e.time.replace(' ', 'T'));
            if (!isNaN(d.getTime())) {
                let hours = d.getHours();
                const minutes = String(d.getMinutes()).padStart(2, '0');
                const ampm = hours >= 12 ? 'pm' : 'am';
                hours = hours % 12;
                hours = hours ? hours : 12;
                timeStr = `${hours}:${minutes} ${ampm}`;
            }
        } catch (ignore) {}

        return `<tr>
            <td class="briefing-td">${timeStr}</td>
            <td class="briefing-td">${e.type}</td>
            <td class="briefing-td">${e.height_ft} ft</td>
        </tr>`;
    }).join('');
    
    const tidesHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">waves</span>
                Tides (${tides.station_name || 'Unknown Station'}) - ${displayDateHeader}
            </h3>
            <div class="tide-box" style="margin-bottom:1rem;">
                <div style="height:200px; width:100%; position:relative;">
                    <canvas id="tideChartModal"></canvas>
                </div>
                <table class="briefing-table">
                    <thead>
                        <tr>
                            <th class="briefing-th">Time</th>
                            <th class="briefing-th">Type</th>
                            <th class="briefing-th">Height</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${tideEventsHtml || '<tr><td colspan="3" class="briefing-no-data">No tide data for this date</td></tr>'}
                    </tbody>
                </table>
            </div>
        </div>
    `;

    // Facilities
    const facilities = briefing.facilities || [];
    const facilHtml = `
        <div class="briefing-section">
            <h3 class="briefing-header-icon">
                <span class="material-symbols-outlined">warehouse</span>
                Facilities
            </h3>
            <ul class="facility-list">
                ${facilities.map(f => {
                    // Icon Mapping
                    let icon = 'place';
                    const typeLower = (f.type || '').toLowerCase();
                    if (typeLower.includes('anchorage')) icon = 'anchor';
                    else if (typeLower.includes('marina')) icon = 'storefront';
                    else if (typeLower.includes('mooring')) icon = 'crisis_alert';

                    let detailsHtml = '';
                    if (typeof f.details === 'string') {
                        detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                    } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        let rows = `
                            <tr>
                                <th class="briefing-th briefing-table-label-width">Type</th>
                                <td class="briefing-td">${f.type}</td>
                            </tr>
                        `;
                        
                        rows += Object.entries(f.details)
                            .filter(([_, v]) => {
                                if (!v) return false;
                                const sv = String(v).toLowerCase().trim();
                                return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                            })
                            .map(([k, v]) => `
                                <tr>
                                    <th class="briefing-th briefing-table-label-width" style="text-transform:capitalize;">${k.replace(/_/g, ' ')}</th>
                                    <td class="briefing-td">${v}</td>
                                </tr>
                            `).join('');
                        
                        detailsHtml = `<table class="briefing-table" style="margin-top:0;">${rows}</table>`;
                    }

                    let locHtml = '';
                    if (f.latitude && f.longitude) {
                        const googleMapsUrl = `https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}`;
                        locHtml = `
                            <p style="margin: 0.2rem 0; color: #666; font-size: 0.9em;">
                                <span class="material-symbols-outlined" style="font-size: 1rem; vertical-align: text-bottom;">my_location</span>
                                ${f.latitude.toFixed(4)}, ${f.longitude.toFixed(4)}
                                <a href="${googleMapsUrl}" target="_blank" style="margin-left:5px;">(Open Map)</a>
                            </p>
                        `;
                    }

                    return `
                        <li class="facility-item">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined" style="font-size: 1.2rem;">${icon}</span>
                                ${f.name}
                            </h4>
                            ${locHtml}
                            ${detailsHtml}
                            ${renderReferences(f.references)}
                        </li>
                    `;
                }).join('') || '<li>No facilities found</li>'}
            </ul>
        </div>
    `;

    content.innerHTML = weatherHtml + sunHtml + tidesHtml + facilHtml;
    
    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoBriefing(briefing, btnRedo);
    }

    // Show Modal
    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');

    // Render Chart (must happen after modal is visible for size calc)
    // Pass ALL events to chart for smooth interpolation
    if (allEvents.length > 0) {
        renderTideChart('tideChartModal', tides, targetDateFull);
    }

    const hide = () => {
        modal.classList.add('hidden');
        modalOverlay.classList.add('hidden');
    };

    btnClose.onclick = hide;
    modalOverlay.onclick = hide; 
}

async function redoBriefing(oldBriefing, btn) {
    const content = document.getElementById('briefing-content');
    content.innerHTML = `
        <div style="text-align:center; padding:3rem; color: #666;">
            <span class="material-symbols-outlined spin" style="font-size: 3rem; margin-bottom: 1rem;">sync</span>
            <p><strong>Agent is researching...</strong></p>
            <p style="font-size: 0.9em;">Checking weather, tides, and local charts.</p>
        </div>
    `;
    btn.disabled = true;

    try {
        await API.triggerResearch(oldBriefing.stop_id);
        
        const oldTime = new Date(oldBriefing.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 45000; // 45 seconds
        
        // Poll
        const poll = setInterval(async () => {
            if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                content.innerHTML = '<div style="text-align:center; padding:2rem; color: #d9534f;"><p><strong>Research timed out.</strong></p><p>The agent is taking too long or encountered an error.</p></div>';
                btn.disabled = false;
                return;
            }

            try {
                const b = await API.getBriefing(oldBriefing.stop_id);
                if (b) {
                    const newTime = new Date(b.created_at).getTime();
                    // Wait for newer timestamp
                    if (newTime > oldTime) {
                        clearInterval(poll);
                        btn.disabled = false;
                        showBriefing(b); // Re-render with new data
                        renderMapStops();
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        content.innerHTML = '<div style="text-align:center; padding:2rem; color: red;"><p>Failed to redo research.</p></div>';
        btn.disabled = false;
    }
}


function selectDate(dateStr) {
    selectedDate = dateStr;
    renderItinerary(); // Re-render to show selection highlight
    
    // Zoom to existing stop if present
    const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
    if (stop && map) {
        map.flyTo({ center: [stop.longitude, stop.latitude], zoom: 10 });
    }
}

function initMap() {
  if (!MAPBOX_TOKEN) {
    console.error('Mapbox token is missing. Please set NAVALPLAN_MB_TOKEN environment variable during build.');
    return;
  }

  mapboxgl.accessToken = MAPBOX_TOKEN;

  map = new mapboxgl.Map({
    container: 'map-container',
    style: __MAPBOX_STYLE__,
    center: [-98.5795, 39.8283], // Center of USA
    zoom: 3,
    preserveDrawingBuffer: true
  });

  map.on('load', () => {
    console.log('NavalPlan: Map Loaded Successfully');
  });

  map.addControl(new mapboxgl.NavigationControl());

  map.on('click', async (e) => {
    if (!currentVoyage || !selectedDate) return;

    const {lng, lat} = e.lngLat;
    const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));

    // Get features at click point
    const features = map.queryRenderedFeatures(e.point);
    console.log('Clicked Features:', features);
    
    // Attempt to find a label
    let locationName = `Location ${lat.toFixed(3)}, ${lng.toFixed(3)}`;
    const labelFeature = features.find(f => f.properties && (f.properties.name || f.properties.name_en));
    
    if (labelFeature) {
        locationName = labelFeature.properties.name || labelFeature.properties.name_en;
        console.log('Found Label:', locationName);
    }

    // Create or Update
    const stopData = {
        target_date: selectedDate + 'T00:00:00Z',
        location_name: locationName,
        latitude: lat,
        longitude: lng,
        search_radius: 5,
        search_radius_unit: 'nm',
        notes: ''
    };

    try {
        if (stop) {
            const updated = await API.updateStop(stop.id, stopData);
            const idx = currentStops.findIndex(s => s.id === stop.id);
            currentStops[idx] = updated;
        } else {
            const created = await API.createStop(currentVoyage.id, stopData);
            currentStops.push(created);
        }
        renderItinerary();
        renderMapStops();
    } catch (err) {
        console.error(err);
        alert('Failed to save stop');
    }
  });
}

async function renderMapStops() {
    clearMap();
    if (!map) return;

    // Sort stops by date
    const sortedStops = [...currentStops].sort((a, b) => 
        new Date(a.target_date) - new Date(b.target_date)
    );

    // Add Markers
    sortedStops.forEach((stop, index) => {
        const el = document.createElement('div');
        el.className = 'marker';
        el.innerHTML = `<span><b>${index + 1}</b></span>`;

        const marker = new mapboxgl.Marker(el)
            .setLngLat([stop.longitude, stop.latitude])
            .setPopup(new mapboxgl.Popup({ offset: 25 }).setText(`${stop.location_name} (Day ${index + 1})`))
            .addTo(map);
        markers.push(marker);
    });

    // Fetch and Draw Facilities
    // We do this async but don't block the line drawing
    (async () => {
        const features = [];
        
        for (const stop of sortedStops) {
            try {
                const b = await API.getBriefing(stop.id);
                if (b && b.facilities) {
                    b.facilities.forEach(f => {
                         if (f.latitude && f.longitude) {
                             let icon = 'marker-15';
                             const type = (f.type || '').toLowerCase();
                             if (type.includes('anchorage')) icon = 'harbor-15';
                             else if (type.includes('marina')) icon = 'warehouse-15';
                             
                             features.push({
                                 type: 'Feature',
                                 geometry: {
                                     type: 'Point',
                                     coordinates: [f.longitude, f.latitude]
                                 },
                                 properties: {
                                     title: f.name,
                                     icon: icon,
                                     description: f.type,
                                     lat: f.latitude,
                                     lng: f.longitude
                                 }
                             });
                         }
                    });
                }
            } catch (err) {
               // Ignore errors fetching briefings for map
            }
        }
        
        if (features.length > 0) {
            if (map.getSource('facilities')) {
                map.getSource('facilities').setData({
                    type: 'FeatureCollection',
                    features: features
                });
            } else {
                map.addSource('facilities', {
                    type: 'geojson',
                    data: {
                        type: 'FeatureCollection',
                        features: features
                    }
                });

                map.addLayer({
                    id: 'facilities-circles',
                    type: 'circle',
                    source: 'facilities',
                    paint: {
                        'circle-radius': 15,
                        'circle-opacity': 1,
                        'circle-color': '#000',
                        'circle-stroke-width': 1,
                        'circle-stroke-color': '#314c3b'
                    }
                });
                
                map.addLayer({
                    id: 'facilities',
                    type: 'symbol',
                    source: 'facilities',
                    layout: {
                        'icon-image': ['get', 'icon'],
                        'icon-size': 1.0,
                        'icon-allow-overlap': true
                    }
                });

                // Click event for facilities
                map.on('click', 'facilities', (e) => {
                    const coords = e.features[0].geometry.coordinates.slice();
                    const props = e.features[0].properties;
                    
                    new mapboxgl.Popup()
                        .setLngLat(coords)
                        .setHTML(`
                            <strong>${props.title}</strong><br>
                            ${props.description}<br>
                            <a href="https://www.google.com/maps/search/?api=1&query=${props.lat},${props.lng}" target="_blank">View on Google Maps</a>
                        `)
                        .addTo(map);
                });
                
                // Cursor style
                map.on('mouseenter', 'facilities', () => {
                    map.getCanvas().style.cursor = 'pointer';
                });
                map.on('mouseleave', 'facilities', () => {
                    map.getCanvas().style.cursor = '';
                });
            }
        }
    })();

    // Draw Line
    const coords = sortedStops.map(s => [s.longitude, s.latitude]);
    
    if (map.getSource('route')) {
        map.getSource('route').setData({
            type: 'Feature',
            properties: {},
            geometry: {
                type: 'LineString',
                coordinates: coords
            }
        });
    } else {
        map.addSource('route', {
            type: 'geojson',
            data: {
                type: 'Feature',
                properties: {},
                geometry: {
                    type: 'LineString',
                    coordinates: coords
                }
            }
        });

        map.addLayer({
            id: 'route',
            type: 'line',
            source: 'route',
            layout: {
                'line-join': 'round',
                'line-cap': 'round'
            },
            paint: {
                'line-color': '#314c3b', // Brand Green
                'line-width': 4,
                'line-dasharray': [2, 1]
            }
        });
    }
}

function clearMap() {
    markers.forEach(m => m.remove());
    markers = [];
    if (map && map.getSource('route')) {
        map.getSource('route').setData({
            type: 'Feature',
            properties: {},
            geometry: {
                type: 'LineString',
                coordinates: []
            }
        });
    }
    if (map && map.getSource('facilities')) {
        map.getSource('facilities').setData({
            type: 'FeatureCollection',
            features: []
        });
    }
}

    async function handleShowReport() {
    if (!currentVoyage) return;
    
    const btn = document.getElementById('btn-export-voyage');
    const originalContent = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

    try {
        // 1. Fetch all data
        // For simplicity, we re-use the currentStops we have, but we need briefings
        // Sort stops
        const sortedStops = [...currentStops].sort((a, b) => 
            new Date(a.target_date) - new Date(b.target_date)
        );
        
        // Fetch Briefings in parallel
        const briefingPromises = sortedStops.map(s => API.getBriefing(s.id).catch(() => null));
        // Also fetch the Voyage Guide
        const guidePromise = API.getVoyageGuide(currentVoyage.id).catch(() => null);
        
        const [briefings, guide] = await Promise.all([
            Promise.all(briefingPromises),
            guidePromise
        ]);

        const renderReferences = (refs) => {
            if (!refs || refs.length === 0) return '';
            return `<div style="font-size: 0.8em; margin-top: 0.3rem; color: #666;">
                <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" style="margin-right:0.3rem">[${i+1}]</a>`).join('')}
            </div>`;
        };
        
        // 2. Build HTML
        let html = `
            <h1 style="text-align:center; border-bottom: 2px solid #333; padding-bottom: 0.5rem;">${currentVoyage.title}</h1>
            <p style="text-align:center; font-style:italic;">
                ${new Date(currentVoyage.start_date).toLocaleDateString()} - ${new Date(currentVoyage.end_date).toLocaleDateString()}
            </p>
            <hr />
        `;

        // --- Add Destination Guide Section ---
        if (guide) {
            html += `
                <div style="margin-bottom: 2rem; page-break-inside: avoid; background: rgba(0,0,0,0.03); padding: 1rem; border-radius: 8px;">
                    <h2 class="report-day-header" style="border-left-color: var(--brand-green-dark);">Destination Guide</h2>
                    
                    ${guide.map_url ? `
                    <div style="margin-bottom: 1.5rem; text-align: center;">
                        <img src="${guide.map_url}" alt="Voyage Map" style="max-width: 100%; border-radius: 4px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);" />
                    </div>
                    ` : ''}

                    <h3>Summary</h3>
                    <p>${guide.summary || 'N/A'}</p>
                    
                    ${guide.sailing_season ? `
                    <div style="margin-top:1rem;">
                        <h3>Sailing Season</h3>
                        <ul style="margin-top:0.5rem;">
                            <li><strong>Best Months:</strong> ${(guide.sailing_season.primary_season_months || []).join(', ')}</li>
                            <li><strong>Storm Season:</strong> ${(guide.sailing_season.storm_season_months || []).join(', ')}</li>
                            <li><strong>Notes:</strong> ${guide.sailing_season.notes || ''} ${renderReferences(guide.sailing_season.references)}</li>
                        </ul>
                    </div>
                    ` : ''}

                    ${(guide.hazards && guide.hazards.length > 0) ? `
                    <div style="margin-top:1rem;">
                        <h3>Hazards</h3>
                        <ul style="margin-top:0.5rem;">
                            ${guide.hazards.map(h => {
                                const link = h.url ? ` <a href="${h.url}" target="_blank" style="font-size:0.8rem;">(Info)</a>` : '';
                                return `<li><h4>${h.title}${link}</h4> <p>${h.description} ${renderReferences(h.references)}</p></li>`;
                            }).join('')}
                        </ul>
                    </div>
                    ` : ''}
                    
                     ${(guide.hubs && guide.hubs.length > 0) ? `
                    <div style="margin-top:1rem;">
                        <h3>Major Hubs</h3>
                        <ul style="margin-top:0.5rem;">
                            ${guide.hubs.map(h => {
                                const link = h.url ? ` <a href="${h.url}" target="_blank" style="font-size:0.8rem;">(Website)</a>` : '';
                                return `<li><h4>${h.name}${link}</h4> <p>${h.description} ${renderReferences(h.references)}</p></li>`;
                            }).join('')}
                        </ul>
                    </div>
                    ` : ''}

                    ${guide.charter_info ? `
                    <div style="margin-top:1rem;">
                         <h3>Charter Info</h3>
                         <p style="margin:0.5rem 0 0.5rem 1rem;"><strong>Available:</strong> ${guide.charter_info.is_charter_destination ? 'Yes' : 'No'}</p>
                         <div style="margin-left:1rem;">
                            <strong>Companies:</strong>
                            ${(guide.charter_info.companies && guide.charter_info.companies.length > 0) ? 
                                `<ul style="margin-top:0.2rem;">${guide.charter_info.companies.map(comp => {
                                    if (typeof comp === 'string') return `<li>${comp}</li>`;
                                    const nameLink = comp.url ? `<a href="${comp.url}" target="_blank">${comp.name}</a>` : comp.name;
                                    return `<li><h4>${nameLink}</h4> ${renderReferences(comp.references)}</li>`;
                                }).join('')}</ul>` : 'None listed'}
                         </div>
                    </div>
                    ` : ''}

                    ${(guide.country_info || guide.currencies) ? `
                    <div style="margin-top:1rem;">
                        <h3>Country & Culture</h3>
                        <ul style="margin-top:0.5rem;">
                             ${guide.country_info ? `
                                <li><strong>Country:</strong> ${guide.country_info.name || 'N/A'}</li>
                                <li><strong>Language:</strong> ${(guide.country_info.languages || []).join(', ') || 'N/A'}</li>
                                <li><strong>Timezone:</strong> ${guide.country_info.timezone || 'N/A'}</li>
                                <li><strong>Emergency:</strong> ${guide.country_info.emergency_numbers ? Object.entries(guide.country_info.emergency_numbers).map(([k,v]) => `${k}: ${v}`).join(', ') : 'N/A'}</li>
                             ` : ''}
                             ${(guide.currencies && guide.currencies.length > 0) ? `
                                <li><strong>Currency:</strong> ${guide.currencies.map(c => `${c.name} (${c.code}) - ${c.symbol || ''}`).join(', ')}</li>
                             ` : ''}
                        </ul>
                    </div>
                    ` : ''}

                    ${(guide.airports && guide.airports.length > 0) ? `
                    <div style="margin-top:1rem;">
                        <h3>Nearest Airports</h3>
                        <ul style="margin-top:0.5rem;">
                            ${guide.airports.map(a => `
                                <li>
                                    <h4>${a.name} (${a.iata_code || 'N/A'})</h4>
                                    <p><strong>Type:</strong> ${a.type || 'Unknown'}, ${a.distance_km ? a.distance_km + ' km' : 'Unknown distance'}</p>
                                    ${renderReferences(a.references)}
                                </li>
                            `).join('')}
                        </ul>
                    </div>
                    ` : ''}

                    ${(guide.points_of_interest && guide.points_of_interest.length > 0) ? `
                    <div style="margin-top:1rem;">
                        <h3>Points of Interest</h3>
                        <ul style="margin-top:0.5rem;">
                            ${guide.points_of_interest.map(poi => `
                                <li>
                                    <h4>${poi.name}</h4>
                                    <p>${poi.description} ${renderReferences(poi.references)}</p>
                                </li>
                            `).join('')}
                        </ul>
                    </div>
                    ` : ''}
                </div>
                <hr />
            `;
        }

        sortedStops.forEach((stop, idx) => {
            const b = briefings[idx];
            html += `
                <div style="margin-bottom: 3rem; page-break-inside: avoid;">
                    <h2 class="report-day-header">Day ${idx + 1}: ${stop.location_name}</h2>
                    <p class="report-day-date"><strong>Date:</strong> ${new Date(stop.target_date).toLocaleDateString()}</p>
            `;
            
            if (b) {
                 const isInvalid = (v) => {
                    if (!v) return true;
                    const sv = String(v).toLowerCase().trim();
                    return sv === 'n/a' || sv === 'unknown' || sv === 'not specified';
                 };

                 // Weather
                if (b.weather_summary) {
                    const w = b.weather_summary;
                    html += `
                        <div class="briefing-section">
                            <h3 class="briefing-header-icon">
                                <span class="material-symbols-outlined">${getIconForWeather(w.condition)}</span>
                                Weather
                            </h3>
                            <div class="weather-box">
                                <table class="briefing-table">
                                    <tr>
                                        <th class="briefing-th briefing-table-label-width">Summary</th>
                                        <td class="briefing-td">${isInvalid(w.summary) ? 'N/A' : w.summary}</td>
                                    </tr>
                                    <tr>
                                        <th class="briefing-th briefing-table-label-width">Conditions</th>
                                        <td class="briefing-td briefing-td-icon">
                                            <span class="material-symbols-outlined" style="font-size: 1.2rem;">${getIconForWeather(w.condition)}</span>
                                            ${isInvalid(w.condition) ? 'N/A' : w.condition}
                                        </td>
                                    </tr>
                                    ${(w.temp_max_f || w.temp_min_f) ? `
                                    <tr>
                                        <th class="briefing-th briefing-table-label-width">Temp</th>
                                        <td class="briefing-td">High: ${Math.round(w.temp_max_f)}°F &nbsp;|&nbsp; Low: ${Math.round(w.temp_min_f)}°F</td>
                                    </tr>
                                    ` : ''}
                                    <tr>
                                        <th class="briefing-th briefing-table-label-width">Wind</th>
                                        <td class="briefing-td">${isInvalid(w.wind_direction) ? 'N/A' : w.wind_direction} ${w.wind_speed_kt || '0'} kt</td>
                                    </tr>
                                    ${w.wave_height_ft > 0 ? `
                                    <tr>
                                        <th class="briefing-th briefing-table-label-width">Waves</th>
                                        <td class="briefing-td">${w.wave_height_ft} ft</td>
                                    </tr>
                                    ` : ''}
                                </table>
                            </div>
                        </div>
                    `;
                }

                // Sun Phase
                if (b.sun_phase && (b.sun_phase.sunrise || b.sun_phase.sunset)) {
                    const sun = b.sun_phase;
                    const formatTime = (t) => {
                        if (!t) return 'N/A';
                        try {
                            const d = new Date(t);
                            if (isNaN(d.getTime())) return t;
                            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
                        } catch (e) {
                            return t;
                        }
                    };

                    html += `
                    <div class="briefing-section">
                        <h3 class="briefing-header-icon">
                            <span class="material-symbols-outlined">wb_twilight</span>
                            Sun Phase
                        </h3>
                        <div class="weather-box">
                            <table class="briefing-table">
                                <tr>
                                    <th class="briefing-th briefing-table-label-width">Sunrise</th>
                                    <td class="briefing-td">${formatTime(sun.sunrise)}</td>
                                </tr>
                                <tr>
                                    <th class="briefing-th briefing-table-label-width">Sunset</th>
                                    <td class="briefing-td">${formatTime(sun.sunset)}</td>
                                </tr>
                            </table>
                        </div>
                    </div>
                    `;
                }
                
                // Tides
                if (b.tides && b.tides.events) {
                    const canvasId = `tideChart_${idx}`;
                    const targetDateYMD = stop.target_date.split('T')[0];
                    const displayEvents = b.tides.events.filter(e => e.time.startsWith(targetDateYMD));
                    
                    const tideEventsHtml = displayEvents.map(e => {
                        let timeStr = e.time;
                        try {
                            const d = new Date(e.time.replace(' ', 'T'));
                            if (!isNaN(d.getTime())) {
                                let hours = d.getHours();
                                const minutes = String(d.getMinutes()).padStart(2, '0');
                                const ampm = hours >= 12 ? 'pm' : 'am';
                                hours = hours % 12;
                                hours = hours ? hours : 12;
                                timeStr = `${hours}:${minutes} ${ampm}`;
                            }
                        } catch (ignore) {}

                        return `<tr>
                            <td class="briefing-td">${timeStr}</td>
                            <td class="briefing-td">${e.type}</td>
                            <td class="briefing-td">${e.height_ft} ft</td>
                        </tr>`;
                    }).join('');

                    const [y, m, d] = targetDateYMD.split('-');
                    const displayDateHeader = `${m}/${d}/${y}`;

                    html += `
                        <div class="briefing-section">
                            <h3 class="briefing-header-icon">
                                <span class="material-symbols-outlined">waves</span>
                                Tides (${b.tides.station_name || 'Station Unknown'}) - ${displayDateHeader}
                            </h3>
                            <div class="tide-box" style="margin-bottom:1rem;">
                                <div style="height:200px; width:100%; position:relative;">
                                    <canvas id="${canvasId}"></canvas>
                                </div>
                                <table class="briefing-table">
                                    <thead>
                                        <tr>
                                            <th class="briefing-th">Time</th>
                                            <th class="briefing-th">Type</th>
                                            <th class="briefing-th">Height</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${tideEventsHtml || '<tr><td colspan="3" class="briefing-no-data">No tide data for this date</td></tr>'}
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    `;
                }
                
                // Facilities
                if (b.facilities && b.facilities.length > 0) {
                     html += `
                        <div class="briefing-section">
                            <h3 class="briefing-header-icon">
                                <span class="material-symbols-outlined">warehouse</span>
                                Facilities
                            </h3>
                            <ul class="facility-list">
                                ${b.facilities.map(f => {
                         // Icon Mapping
                         let icon = 'place';
                         const typeLower = (f.type || '').toLowerCase();
                         if (typeLower.includes('anchorage')) icon = 'anchor';
                         else if (typeLower.includes('marina')) icon = 'storefront';
                         else if (typeLower.includes('mooring')) icon = 'crisis_alert';

                         let detailsHtml = '';
                         if (typeof f.details === 'string') {
                             detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                         } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        let rows = `
                            <tr>
                                <th class="briefing-th briefing-table-label-width">Type</th>
                                <td class="briefing-td">${f.type}</td>
                            </tr>
                        `;
                        
                        rows += Object.entries(f.details)
                            .filter(([_, v]) => {
                                if (!v) return false;
                                const sv = String(v).toLowerCase().trim();
                                return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                            })
                            .map(([k, v]) => `
                                <tr>
                                    <th class="briefing-th briefing-table-label-width" style="text-transform:capitalize;">${k.replace(/_/g, ' ')}</th>
                                    <td class="briefing-td">${v}</td>
                                </tr>
                            `).join('');
                        
                        detailsHtml = `<table class="briefing-table" style="margin-top:0;">${rows}</table>`;
                    }

                    let locHtml = '';
                    if (f.latitude && f.longitude) {
                        const googleMapsUrl = `https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}`;
                        locHtml = `
                            <p style="margin: 0.2rem 0; color: #666; font-size: 0.9em;">
                                <span class="material-symbols-outlined" style="font-size: 1rem; vertical-align: text-bottom;">my_location</span>
                                ${f.latitude.toFixed(4)}, ${f.longitude.toFixed(4)}
                                <a href="${googleMapsUrl}" target="_blank" style="margin-left:5px;">(Open Map)</a>
                            </p>
                        `;
                    }

                    return `
                        <li class="facility-item">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined" style="font-size: 1.2rem;">${icon}</span>
                                ${f.name}
                            </h4>
                            ${locHtml}
                            ${detailsHtml}
                            ${renderReferences(f.references)}
                        </li>
                    `;
                }).join('') || '<li>No facilities found</li>'}
            </ul>
        </div>
    `;
                }
            } else {
                html += `<p style="color: #888; font-style: italic;">No briefing data generated yet.</p>`;
            }
            
            html += `</div>`;
        });
        
        // 3. Show Modal
        const modal = document.getElementById('modal-report');
        const content = document.getElementById('report-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = html;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        // Render all charts
        sortedStops.forEach((stop, idx) => {
            const b = briefings[idx];
            if (b && b.tides && b.tides.events) {
                renderTideChart(`tideChart_${idx}`, b.tides, stop.target_date);
            }
        });

    } catch (err) {
        console.error(err);
        alert('Failed to generate report.');
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalContent;
    }
}

function getIconForWeather(description) {
    const d = (description || '').toLowerCase();
    if (d.includes('clear')) return 'clear_day';
    if (d.includes('partly cloudy')) return 'partly_cloudy_day';
    if (d.includes('overcast')) return 'cloud';
    if (d.includes('drizzle')) return 'weather_mix';
    if (d.includes('rain')) return 'rainy';
    return 'cloud';
}


async function handleGuideClick(voyage, button) {
    const originalContent = button.innerHTML;
    
    try {
        const existing = await API.getVoyageGuide(voyage.id);
        if (existing) {
            showVoyageGuide(existing);
            return;
        }

        // Show Modal Immediately with loading state
        const modal = document.getElementById('modal-guide');
        const content = document.getElementById('guide-content');
        const modalOverlay = document.getElementById('modal-overlay');
        
        content.innerHTML = `
            <div style="text-align:center; padding:3rem; color: #666;">
                <span class="material-symbols-outlined spin" style="font-size: 3rem; margin-bottom: 1rem;">sync</span>
                <p><strong>Agent is researching...</strong></p>
                <p style="font-size: 0.9em;">Gathering local knowledge, seasonal data, and regional hazards.</p>
            </div>
        `;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        button.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

        // Trigger
        await API.triggerVoyageGuideResearch(voyage.id);
        
        // Poll
        const poll = setInterval(async () => {
            try {
                const g = await API.getVoyageGuide(voyage.id);
                if (g) {
                    clearInterval(poll);
                    button.innerHTML = originalContent;
                    showVoyageGuide(g); // Updates the already-open modal with data
                }
            } catch (ignore) { /* keep polling */ }
        }, 3000);
        
    } catch (err) {
        console.error(err);
        button.innerHTML = '<span class="material-symbols-outlined error">error</span>';
        setTimeout(() => button.innerHTML = originalContent, 2000);
    }
}

function showVoyageGuide(guide) {
    const modal = document.getElementById('modal-guide');
    const content = document.getElementById('guide-content');
    const btnRedo = document.getElementById('btn-redo-guide');
    const modalOverlay = document.getElementById('modal-overlay');

    const renderReferences = (refs) => {
        if (!refs || refs.length === 0) return '';
        return `<div style="font-size: 0.8em; margin-top: 0.3rem; color: #666;">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" style="margin-right:0.3rem">[${i+1}]</a>`).join('')}
        </div>`;
    };

    let html = `
        <div class="briefing-section">
            <h3>Overview</h3>
            <p>${guide.summary || 'No summary available.'}</p>
        </div>
    `;

    // Sailing Season
    if (guide.sailing_season) {
        const s = guide.sailing_season;
        html += `
            <div class="briefing-section">
                <h3>Sailing Season</h3>
                <table class="briefing-table">
                    <tr><th class="briefing-th">Best Months</th><td class="briefing-td">${(s.primary_season_months || []).join(', ') || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Storm Season</th><td class="briefing-td">${(s.storm_season_months || []).join(', ') || 'N/A'} (${s.storm_risk_level || 'Unknown Risk'})</td></tr>
                    <tr><th class="briefing-th">Notes</th><td class="briefing-td">${s.notes || ''} ${renderReferences(s.references)}</td></tr>
                </table>
            </div>
        `;
    }

    // Hazards
    if (guide.hazards && guide.hazards.length > 0) {
        html += `<div class="briefing-section"><h3>Regional Hazards</h3><ul class="facility-list">`;
        guide.hazards.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" style="font-size:0.8rem; margin-left:0.5rem;">(Info)</a>` : '';
            html += `<li class="facility-item">
                <h4>${h.title}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Hubs
    if (guide.hubs && guide.hubs.length > 0) {
        html += `<div class="briefing-section"><h3>Major Hubs</h3><ul class="facility-list">`;
        guide.hubs.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" style="font-size:0.8rem; margin-left:0.5rem;">(Website)</a>` : '';
            html += `<li class="facility-item">
                <h4>${h.name}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }
    
    // Charter Info
    if (guide.charter_info) {
        const c = guide.charter_info;
        
        let companiesHtml = 'None listed';
        if (c.companies && c.companies.length > 0) {
             companiesHtml = '<ul style="padding-left: 1.2rem; margin: 0.5rem 0;">' + 
             c.companies.map(comp => {
                if (typeof comp === 'string') return `<li>${comp}</li>`;
                const nameLink = comp.url ? `<a href="${comp.url}" target="_blank">${comp.name}</a>` : comp.name;
                return `<li>${nameLink} ${renderReferences(comp.references)}</li>`;
             }).join('') + 
             '</ul>';
        }

        html += `
            <div class="briefing-section">
                <h3>Charter Info</h3>
                <p><strong>Available:</strong> ${c.is_charter_destination ? 'Yes' : 'No'}</p>
                <div style="margin-top:0.5rem"><strong>Companies:</strong> ${companiesHtml}</div>
            </div>
        `;
    }

    // Country Info & Currency
    if (guide.country_info || guide.currencies) {
        const c = guide.country_info || {};
        const curs = guide.currencies || [];
        
        let currencyHtml = 'N/A';
        if (curs.length > 0) {
            currencyHtml = curs.map(cur => `${cur.name} (${cur.code}) - ${cur.symbol || ''}`).join(', ');
        }

        html += `
            <div class="briefing-section">
                <h3>Country & Culture</h3>
                <table class="briefing-table">
                    <tr><th class="briefing-th">Country</th><td class="briefing-td">${c.name || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Language</th><td class="briefing-td">${c.languages ? c.languages.join(', ') : 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Timezone</th><td class="briefing-td">${c.timezone || 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Emergency</th><td class="briefing-td">${c.emergency_numbers ? Object.entries(c.emergency_numbers).map(([k,v]) => `${k}: ${v}`).join(', ') : 'N/A'}</td></tr>
                    <tr><th class="briefing-th">Currency</th><td class="briefing-td">${currencyHtml}</td></tr>
                </table>
            </div>
        `;
    }

    // Airports
    if (guide.airports && guide.airports.length > 0) {
        html += `<div class="briefing-section"><h3>Nearest Airports</h3><ul class="facility-list">`;
        guide.airports.forEach(a => {
            html += `<li class="facility-item">
                <h4>${a.name} (${a.iata_code || 'N/A'})</h4>
                <p><strong>Type:</strong> ${a.type || 'Unknown'}</p>
                <p><strong>Distance:</strong> ${a.distance_km ? a.distance_km + ' km' : 'Unknown'}</p>
                ${renderReferences(a.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Points of Interest
    if (guide.points_of_interest && guide.points_of_interest.length > 0) {
        html += `<div class="briefing-section"><h3>Points of Interest</h3><ul class="facility-list">`;
        guide.points_of_interest.forEach(poi => {
            html += `<li class="facility-item">
                <h4>${poi.name}</h4>
                <p>${poi.description}</p>
                ${renderReferences(poi.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    content.innerHTML = html;

    // Redo Handler
    if (btnRedo) {
        btnRedo.onclick = () => redoGuide(guide, btnRedo);
    }

    modal.classList.remove('hidden');
    modalOverlay.classList.remove('hidden');
}

async function redoGuide(oldGuide, btn) {
    const content = document.getElementById('guide-content');
    content.innerHTML = `
        <div style="text-align:center; padding:3rem; color: #666;">
            <span class="material-symbols-outlined spin" style="font-size: 3rem; margin-bottom: 1rem;">sync</span>
            <p><strong>Agent is researching...</strong></p>
        </div>
    `;
    btn.disabled = true;

    try {
        await API.triggerVoyageGuideResearch(oldGuide.voyage_id);
        const oldTime = new Date(oldGuide.created_at).getTime();
        const startTime = Date.now();
        const TIMEOUT_MS = 60000; 
        
        const poll = setInterval(async () => {
             if (Date.now() - startTime > TIMEOUT_MS) {
                clearInterval(poll);
                content.innerHTML = '<div style="text-align:center; padding:2rem; color: #d9534f;"><p><strong>Research timed out.</strong></p></div>';
                btn.disabled = false;
                return;
            }
            try {
                const g = await API.getVoyageGuide(oldGuide.voyage_id);
                if (g) {
                    const newTime = new Date(g.created_at).getTime();
                    if (newTime > oldTime) {
                        clearInterval(poll);
                        btn.disabled = false;
                        showVoyageGuide(g);
                    }
                }
            } catch (ignore) { }
        }, 3000);
    } catch (err) {
        console.error(err);
        btn.disabled = false;
    }
}

function showNotification(title, message) {
    const modal = document.getElementById('modal-notification');
    const modalOverlay = document.getElementById('modal-overlay');
    const titleEl = document.getElementById('notification-title');
    const msgEl = document.getElementById('notification-message');

    if (modal && titleEl && msgEl) {
        titleEl.textContent = title;
        msgEl.textContent = message;
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');
    } else {
        alert(`${title}\n\n${message}`);
    }
}
