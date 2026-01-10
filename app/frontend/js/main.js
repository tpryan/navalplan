import DOMPurify from 'dompurify';
import { API } from './api.js';
import { checkSession, currentUser } from './auth.js';
import { importLibrary, setOptions } from "@googlemaps/js-api-loader";

const GOOGLE_MAPS_API_KEY = __GOOGLE_MAPS_API_KEY__;
setOptions({
  key: GOOGLE_MAPS_API_KEY,
  version: "weekly",
});

// Dynamic library loading
let googleMapsLib = null;
async function loadGoogleMaps() {
    if (googleMapsLib) return googleMapsLib;
    googleMapsLib = await importLibrary("maps");
    return googleMapsLib;
}

let ChartLib = null;
async function loadChart() {
    if (ChartLib) return ChartLib;
    ChartLib = (await import('chart.js/auto')).default;
    return ChartLib;
}

// Start loading large libraries immediately
loadGoogleMaps();
loadChart();

// Configuration

// State
let voyages = [];
let currentVoyage = null;
let currentStops = [];
let selectedDate = null;
let map = null;
let markers = [];
let routePolyline = null;
let facilityMarkers = [];
let editingVoyageId = null;
let currentMode = 'planner'; // 'planner' or 'discovery'
let discoveryRegions = [];

// Pagination
let currentVoyagePage = 1;
const VOYAGE_PAGE_LIMIT = 20;
let currentStopPage = 1;
const STOP_PAGE_LIMIT = 50;

/**
 * Strips Plus Codes (e.g. "82GQ+6Q ") from location names for cleaner UI display
 */
function displayLocationName(name) {
    if (!name) return "";
    // Regular expression to match Plus Codes at the start of the string
    // Matches 4-8 alphanumeric chars + '+' + 2-3 alphanumeric chars followed by space
    return name.replace(/^[A-Z0-9]{4,8}\+[A-Z0-9]{2,3}\s*/i, "").trim();
}

/**
 * Smoothing algorithm for polygons (Chaikin's)
 */
function smoothPolygon(coordinates, iterations = 2) {
    if (!coordinates || coordinates.length < 3) return coordinates;
    
    let result = coordinates;
    for (let i = 0; i < iterations; i++) {
        result = chaikin(result);
    }
    return result;
}

function chaikin(coords) {
    const newCoords = [];
    // Handle the closed loop: if last point == first point, we smooth across it
    const isClosed = coords[0][0] === coords[coords.length-1][0] && coords[0][1] === coords[coords.length-1][1];
    
    for (let i = 0; i < coords.length - 1; i++) {
        const p0 = coords[i];
        const p1 = coords[i + 1];
        
        const q = [
            0.75 * p0[0] + 0.25 * p1[0],
            0.75 * p0[1] + 0.25 * p1[1]
        ];
        const r = [
            0.25 * p0[0] + 0.75 * p1[0],
            0.25 * p0[1] + 0.75 * p1[1]
        ];
        
        newCoords.push(q);
        newCoords.push(r);
    }
    
    if (isClosed) {
        newCoords.push(newCoords[0]); // Re-close
    } else {
        // If not closed, keep endpoints (less ideal for smoothing)
        newCoords.unshift(coords[0]);
        newCoords.push(coords[coords.length-1]);
    }
    
    return newCoords;
}

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  
  // Shared/Public View Handler
  if (window.location.pathname.startsWith('/shared/')) {
      const token = window.location.pathname.replace('/shared/', '');
      if (token) {
          initSharedMode(token);
          return;
      }
  }

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
  const btnDiscover = document.getElementById('btn-discover');
  const btnCloseDiscovery = document.getElementById('btn-close-discovery');
  const monthSlider = document.getElementById('month-slider');

  // Discovery Toggle
  if (btnDiscover) {
      btnDiscover.addEventListener('click', () => toggleDiscoveryMode(true));
  }
  if (btnCloseDiscovery) {
      btnCloseDiscovery.addEventListener('click', () => toggleDiscoveryMode(false));
  }
  
  const btnCloseDiscoveryIntro = document.getElementById('btn-close-discovery-intro');
  if (btnCloseDiscoveryIntro) {
      btnCloseDiscoveryIntro.addEventListener('click', () => {
          document.getElementById('modal-discovery-intro').classList.add('hidden');
          document.getElementById('modal-overlay').classList.add('hidden');
      });
  }

  // Month Slider
  if (monthSlider) {
      monthSlider.addEventListener('input', (e) => {
          const months = [
              'January', 'February', 'March', 'April', 'May', 'June',
              'July', 'August', 'September', 'October', 'November', 'December'
          ];
          const month = parseInt(e.target.value);
          document.getElementById('month-display').textContent = months[month - 1];
          loadDiscoveryRegions(month);
      });
  }

  // Mobile Menu Logic
  const appContainer = document.getElementById('app');
  const btnMobileMenu = document.getElementById('btn-mobile-menu');
  const btnCloseSidebar = document.getElementById('btn-close-sidebar');

  if (btnMobileMenu) {
      btnMobileMenu.addEventListener('click', () => {
          appContainer.classList.add('menu-open');
      });
  }

  if (btnCloseSidebar) {
      btnCloseSidebar.addEventListener('click', () => {
          appContainer.classList.remove('menu-open');
      });
  }

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
      const lat = center.lat();
      const lng = center.lng();
      inputLat.value = lat;
      inputLng.value = lng;
      displayCoords.textContent = `Lat: ${lat.toFixed(4)}, Lng: ${lng.toFixed(4)}`;
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
                  // Only spin if not already done/spinning
                  if (!btn.querySelector('.spin') && !btn.querySelector('.material-symbols-outlined').textContent.includes('check_circle')) {
                     btn.dataset.originalContent = btn.innerHTML;
                     btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';
                     btn.disabled = true;
                  }
              });

              try {
                  // 1b. Snapshot timestamps to distinguish new data from old
                  const initialTimestamps = {};
                  try {
                      const existingGuide = await API.getVoyageGuide(currentVoyage.id);
                      if (existingGuide) initialTimestamps['guide'] = new Date(existingGuide.created_at).getTime();
                      
                      // Fetch existing briefings just for the list we have
                      const existingBriefings = await Promise.all(
                          currentStops.map(s => API.getBriefing(s.id).catch(()=>null))
                      );
                      existingBriefings.forEach(b => {
                          if (b) initialTimestamps['stop_' + b.stop_id] = new Date(b.created_at).getTime();
                      });
                  } catch (e) { console.warn("Failed to snapshot timestamps", e); }

                  // Helper to check freshness
                  const isNewData = (item, type, id) => {
                      const key = type + (id ? '_' + id : '');
                      const prevTime = initialTimestamps[key];
                      if (!prevTime) return true; // No previous data, so this must be new
                      const newTime = new Date(item.created_at).getTime();
                      return newTime > prevTime;
                  };

                  await API.triggerFullResearch(currentVoyage.id);
                  showNotification('Research Started', 'Full voyage research has started. Individual stops will update as they complete.');
                  
                  // 2. Poll for completion
                  const startTime = Date.now();
                  const TIMEOUT_MS = 300000; // 5 minutes timeout (research all is heavy)
                  
                  const pendingStops = [...currentStops]; 
                  let guideComplete = false;
                  
                  const poll = setInterval(async () => {
                      // Check timeout
                      if (Date.now() - startTime > TIMEOUT_MS) {
                          clearInterval(poll);
                          btnResearchAll.innerHTML = originalContent;
                          btnResearchAll.disabled = false;
                          // Revert stuck spinners
                          const stuckBtns = document.querySelectorAll('.day-actions .research:disabled');
                          stuckBtns.forEach(btn => {
                                 btn.innerHTML = btn.dataset.originalContent || '<span class="material-symbols-outlined">science</span>';
                                 btn.disabled = false;
                          });
                          showNotification('Research Timeout', 'Research is taking longer than expected. Some stops may still be processing.');
                          return;
                      }

                      try {
                          // Check Guide
                          if (!guideComplete) {
                              const g = await API.getVoyageGuide(currentVoyage.id);
                              if (g && isNewData(g, 'guide')) guideComplete = true;
                          }

                          // Check Stops
                          try {
                              const briefings = await API.getVoyageBriefings(currentVoyage.id);
                              
                              for (let i = pendingStops.length - 1; i >= 0; i--) {
                                  const stop = pendingStops[i];
                                  const b = briefings.find(br => br.stop_id === stop.id);
                                  
                                  if (b && isNewData(b, 'stop', stop.id)) {
                                      // 1. Mark as done in our list
                                      pendingStops.splice(i, 1);

                                      // 2. Update the specific button UI immediately
                                      const btn = document.querySelector(`.research[data-stop-id="${stop.id}"]`);
                                      if (btn) {
                                          btn.innerHTML = '<span class="material-symbols-outlined" style="color: var(--brand-green);">check_circle</span>';
                                          btn.disabled = false;
                                          btn.title = "View Briefing";
                                          btn.classList.remove('spin'); 
                                      }
                                  }
                              }
                          } catch (e) {
                              // Ignore error
                          }

                          // If all done
                          if (guideComplete && pendingStops.length === 0) {
                              clearInterval(poll);
                              btnResearchAll.innerHTML = originalContent;
                              btnResearchAll.disabled = false;
                              
                              renderMapStops(); // Refresh map with new data markers
                              
                              showNotification('Research Complete', 'All research tasks have been completed successfully.');
                          }

                      } catch (err) {
                          console.error("Polling cycle error", err);
                      }
                  }, 5000); // Poll every 5 seconds (slightly slower to be nice)

              } catch (err) {
                  console.error(err);
                  btnResearchAll.innerHTML = originalContent;
                  btnResearchAll.disabled = false;
                  // Revert spinners
                  const researchBtns = document.querySelectorAll('.day-actions .research');
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

        // 2c. Convert Overview Grid to Table
        const overviewGrid = content.querySelector('.overview-grid');
        const overviewReplacements = [];

        if (overviewGrid) {
            const table = document.createElement('table');
            table.style.width = '100%';
            table.style.borderCollapse = 'separate';
            table.style.borderSpacing = '10px';
            
            const cards = Array.from(overviewGrid.querySelectorAll('.overview-card'));
            let currentRow = null;
            
            cards.forEach((card, index) => {
                // Assuming max 4 columns based on existing logic
                if (index % 4 === 0) {
                    currentRow = document.createElement('tr');
                    table.appendChild(currentRow);
                }
                
                const td = document.createElement('td');
                td.style.border = '1px solid #ccc';
                td.style.borderRadius = '8px';
                td.style.padding = '10px';
                td.style.backgroundColor = '#fff';
                td.style.verticalAlign = 'top';
                td.style.width = '25%'; // Distribute evenly
                
                // Move card content to TD
                while (card.firstChild) {
                    td.appendChild(card.firstChild);
                }
                
                currentRow.appendChild(td);
            });
            
            // Insert table before grid
            overviewGrid.parentNode.insertBefore(table, overviewGrid);
            overviewGrid.style.display = 'none';
            
            overviewReplacements.push({
                grid: overviewGrid,
                table: table,
                originalCards: cards
            });
        }

        // 3. Strip Styles and Classes
        const allElements = content.querySelectorAll('*');
        const originalAttributes = [];
        
        allElements.forEach(el => {
            // Skip the temp images we just created
            if (tempImages.some(t => t.img === el)) return;
            // Skip the original ULs we just hid
            if (modifiedLists.some(m => m.ul === el)) return;
            // Skip the original Grid we just hid
            if (overviewReplacements.some(r => r.grid === el)) return;

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
            th.style.backgroundColor = 'rgb(227, 220, 211)'; // var(--brand-lighter)
            th.style.padding = '4px 8px';
            th.style.border = '1px solid #cccccc';
            th.style.textTransform = 'capitalize';

            // Check if it originally had the label-width class
            const attr = originalAttributes.find(a => a.el === th);
            if (attr && attr.class && attr.class.includes('briefing-table-label-width')) {
                th.style.width = '20ch';
                th.style.whiteSpace = 'nowrap';
            }
        });

        const theads = content.querySelectorAll('thead');
        theads.forEach(thead => {
            thead.style.backgroundColor = 'rgba(0,0,0,0.05)';
        });

        const tds = content.querySelectorAll('td');
        tds.forEach(td => {
            td.style.padding = '4px 8px';
            td.style.border = '1px solid #cccccc';
            td.style.verticalAlign = 'top';
        });

        const tables = content.querySelectorAll('table');
        tables.forEach(table => {
            table.style.borderCollapse = 'collapse';
            table.style.width = '100%';
            table.style.marginTop = '1rem';
            table.style.marginBottom = '1rem';
        });

        const h3s = content.querySelectorAll('h3');
        h3s.forEach(h3 => {
            h3.style.color = 'rgb(88, 61, 27)'; // var(--brand-dark)
            h3.style.marginTop = '1.5rem';
            h3.style.marginBottom = '0.5rem';
            h3.style.borderBottom = '1px solid #cccccc';
            h3.style.paddingBottom = '4px';
        });

        const h4s = content.querySelectorAll('h4');
        h4s.forEach(h4 => {
            h4.style.margin = '0.5rem 0';
            h4.style.fontSize = '1.1rem';
            h4.style.color = 'rgb(88, 61, 27)';
        });

        const imgs = content.querySelectorAll('img');
        imgs.forEach(img => {
            img.style.width = '100%';
            img.style.maxWidth = '600px';
            img.style.height = 'auto';
            img.style.display = 'block';
            img.style.margin = '1rem 0';
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
            
            // Restore Overview Grid
            overviewReplacements.forEach(({ grid, table, originalCards }) => {
                 // We need to move content back from TDs to Cards
                 const tds = table.querySelectorAll('td');
                 tds.forEach((td, i) => {
                     const card = originalCards[i];
                     if (card) {
                         while (td.firstChild) {
                             card.appendChild(td.firstChild);
                         }
                     }
                 });
                 table.remove();
                 grid.style.display = '';
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
    voyages = await API.getVoyages(currentVoyagePage, VOYAGE_PAGE_LIMIT);
    renderVoyageList();
  } catch (err) {
    console.error(err);
    if (err.message === 'Unauthorized') {
        listContainer.innerHTML = '<p class="loading-text">Login to view and plan your voyages.</p>';
    } else {
        listContainer.innerHTML = '<p class="loading-text error">Failed to load voyages.</p>';
    }
  }
}

function renderVoyageList() {
  const listContainer = document.getElementById('voyage-list');
  listContainer.innerHTML = '';

  if ((!voyages || voyages.length === 0) && currentVoyagePage === 1) {
    listContainer.innerHTML = '<p class="loading-text">No voyages yet. Plan your first trip!</p>';
    return;
  }

  voyages.forEach(voyage => {
    const el = document.createElement('div');
    el.className = 'voyage-item';
    
    const locationHtml = voyage.location_name ? `<p class="font-sm text-gray">📍 ${DOMPurify.sanitize(displayLocationName(voyage.location_name))}</p>` : '';
    
    el.innerHTML = DOMPurify.sanitize(`
      <div class="voyage-info">
        <h2>${voyage.title}</h2>
        <p>${new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'})} - ${new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}</p>
        ${locationHtml}
      </div>
      <div class="voyage-actions">
        <button class="btn-icon edit" title="Edit">
          <span class="material-symbols-outlined">edit</span>
        </button>
        <button class="btn-icon delete" title="Delete">
          <span class="material-symbols-outlined">delete</span>
        </button>
      </div>
    `);
    
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

  // Pagination Controls
  const paginationControls = document.createElement('div');
  paginationControls.className = 'flex justify-center align-center gap-md mt-md p-sm';
  
  const prevBtn = document.createElement('button');
  prevBtn.className = 'btn secondary';
  prevBtn.disabled = currentVoyagePage === 1;
  prevBtn.innerHTML = '<span class="material-symbols-outlined">chevron_left</span>';
  prevBtn.onclick = () => {
      if (currentVoyagePage > 1) {
          currentVoyagePage--;
          loadVoyages();
      }
  };

  const pageLabel = document.createElement('span');
  pageLabel.className = 'text-gray font-sm';
  pageLabel.textContent = `Page ${currentVoyagePage}`;

  const nextBtn = document.createElement('button');
  nextBtn.className = 'btn secondary';
  // If we got fewer items than limit, we are likely on the last page
  nextBtn.disabled = voyages.length < VOYAGE_PAGE_LIMIT;
  nextBtn.innerHTML = '<span class="material-symbols-outlined">chevron_right</span>';
  nextBtn.onclick = () => {
      currentVoyagePage++;
      loadVoyages();
  };

  paginationControls.appendChild(prevBtn);
  paginationControls.appendChild(pageLabel);
  paginationControls.appendChild(nextBtn);

  if (voyages.length > 0 || currentVoyagePage > 1) {
      listContainer.appendChild(paginationControls);
  }
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
    document.getElementById('itinerary-dates').textContent = `${new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'})} - ${new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}`;
}

async function selectVoyage(voyage) {
    currentVoyage = voyage;
    document.getElementById('voyage-list').classList.add('hidden');
    document.getElementById('itinerary-view').classList.remove('hidden');
    document.querySelector('.sidebar-actions').classList.add('hidden');

    updateItineraryHeader(voyage);

    // Reset pagination
    currentStopPage = 1;
    loadStops();
}

async function loadStops() {
    const list = document.getElementById('itinerary-list');
    list.innerHTML = '<div class="loading-state"><p>Loading stops...</p></div>';

    try {
        currentStops = await API.getStops(currentVoyage.id, currentStopPage, STOP_PAGE_LIMIT);
        renderItinerary();
        renderMapStops();

        if (map) {
            if (currentStops.length > 0) {
                const { LatLngBounds } = await importLibrary("core");
                const bounds = new LatLngBounds();
                currentStops.forEach(stop => bounds.extend({ lat: stop.latitude, lng: stop.longitude }));
                if (currentVoyage.latitude != null && currentVoyage.longitude != null) {
                    bounds.extend({ lat: currentVoyage.latitude, lng: currentVoyage.longitude });
                }
                map.fitBounds(bounds, 50);
            } else if (currentVoyage.latitude != null && currentVoyage.longitude != null) {
                map.panTo({ lat: currentVoyage.latitude, lng: currentVoyage.longitude });
                map.setZoom(9);
            }
        }
    } catch (err) {
        console.error(err);
        alert('Failed to load stops');
        list.innerHTML = '<div class="error-state"><p>Failed to load stops.</p></div>';
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
            <div class="day-info flex-1">
                <span class="day-date">${currentDate.toLocaleDateString(undefined, {month:'short', day:'numeric', timeZone: 'UTC'})}</span>
                <span class="day-location ${stop ? 'set' : ''}">${stop ? displayLocationName(stop.location_name) : 'No destination'}</span>
            </div>
        `;
        
        // Research Action
        if (stop) {
            html += `
                <div class="day-actions">
                    <button class="btn-icon research" title="Research" data-stop-id="${stop.id}">
                        <span class="material-symbols-outlined">science</span>
                    </button>
                    <button class="btn-icon delete-stop" title="Delete Stop">
                        <span class="material-symbols-outlined">delete</span>
                    </button>
                </div>
            `;
        }
        
        el.innerHTML = DOMPurify.sanitize(html);
        
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
                if (confirm(`Remove stop at ${displayLocationName(stop.location_name)}?`)) {
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

    // Pagination Controls
    const paginationControls = document.createElement('div');
    paginationControls.className = 'flex justify-center align-center gap-md mt-md p-sm';
    
    const prevBtn = document.createElement('button');
    prevBtn.className = 'btn secondary';
    prevBtn.disabled = currentStopPage === 1;
    prevBtn.innerHTML = '<span class="material-symbols-outlined">chevron_left</span>';
    prevBtn.onclick = () => {
        if (currentStopPage > 1) {
            currentStopPage--;
            loadStops();
        }
    };

    const pageLabel = document.createElement('span');
    pageLabel.className = 'text-gray font-sm';
    pageLabel.textContent = `Stops Page ${currentStopPage}`;

    const nextBtn = document.createElement('button');
    nextBtn.className = 'btn secondary';
    nextBtn.disabled = currentStops.length < STOP_PAGE_LIMIT;
    nextBtn.innerHTML = '<span class="material-symbols-outlined">chevron_right</span>';
    nextBtn.onclick = () => {
        currentStopPage++;
        loadStops();
    };

    paginationControls.appendChild(prevBtn);
    paginationControls.appendChild(pageLabel);
    paginationControls.appendChild(nextBtn);

    if (currentStops.length > 0 || currentStopPage > 1) {
        list.appendChild(paginationControls);
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
            <div class="loading-state">
                <span class="material-symbols-outlined spin loading-icon">sync</span>
                <p class="font-xs">Checking weather, tides, and local charts.</p>
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

async function renderTideChart(canvasId, tideData, targetDateStr) {
    if (!tideData || !tideData.events) return;

    // Helper to parse strings as wall-clock time (browser local)
    const parseLocal = (s) => {
        if (!s) return new Date(NaN);
        // Strip 'Z' if present, replace space with 'T'
        const clean = s.replace('Z', '').replace(' ', 'T');
        // If it's just a date (YYYY-MM-DD), append T00:00:00 to avoid UTC parsing
        const final = clean.length === 10 ? clean + 'T00:00:00' : clean;
        return new Date(final);
    };

    // Target Date Midnight (Wall-clock)
    const targetStart = parseLocal(targetDateStr).getTime();

    // Parse Events
    const points = [];
    tideData.events.forEach(e => {
        const d = parseLocal(e.time);
        
        if (!isNaN(d.getTime())) {
            const diffMs = d.getTime() - targetStart;
            const floatHours = diffMs / (1000 * 60 * 60);
            points.push({ x: floatHours, y: e.height_ft });
        }
    });

    points.sort((a, b) => a.x - b.x);

    const Chart = await loadChart();
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

async function showBriefing(briefing) {
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
        return `<div class="ref-link">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
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
                            <span class="material-symbols-outlined icon-lg">${getIconForWeather(weather.condition)}</span>
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
            <div class="tide-box mb-md">
                <div class="tide-chart-container">
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
                    else if (typeLower.includes('bar')) icon = 'local_bar';
                    else if (typeLower.includes('restaurant')) icon = 'restaurant';

                    let detailsHtml = '';
                    if (typeof f.details === 'string') {
                        detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                    } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        let rows = `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Type</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${f.type}</td>
                            </tr>
                        `;
                        
                        rows += Object.entries(f.details)
                            .filter(([_, v]) => {
                                if (!v) return false;
                                const sv = String(v).toLowerCase().trim();
                                return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                            })
                            .map(([k, v]) => `
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width capitalize" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">${k.replace(/_/g, ' ')}</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${v}</td>
                                </tr>
                            `).join('');
                        
                        detailsHtml = `<table class="briefing-table mt-0" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em; margin-top: 0.5rem;">${rows}</table>`;
                    }

                    let locHtml = '';
                    if (f.latitude && f.longitude) {
                        const googleMapsUrl = `https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}`;
                        locHtml = `
                            <p class="map-link-p">
                                <span class="material-symbols-outlined icon-md icon-bottom">my_location</span>
                                ${f.latitude.toFixed(4)}, ${f.longitude.toFixed(4)}
                                <a href="${googleMapsUrl}" target="_blank" class="map-link-a">(Open Map)</a>
                            </p>
                        `;
                    }

                    return `
                        <li class="facility-item">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined icon-lg">${icon}</span>
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

    content.innerHTML = DOMPurify.sanitize(weatherHtml + sunHtml + tidesHtml + facilHtml);
    
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
        await renderTideChart('tideChartModal', tides, targetDateFull);
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
        <div class="loading-state">
            <span class="material-symbols-outlined spin loading-icon">sync</span>
            <p class="font-xs">Checking weather, tides, and local charts.</p>
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
                content.innerHTML = '<div class="error-state"><p><strong>Research timed out.</strong></p><p>The agent is taking too long or encountered an error.</p></div>';
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
        content.innerHTML = '<div class="error-state error-text"><p>Failed to redo research.</p></div>';
        btn.disabled = false;
    }
}


function selectDate(dateStr) {
    selectedDate = dateStr;
    renderItinerary(); // Re-render to show selection highlight
    
    // Auto-close menu on mobile
    document.getElementById('app').classList.remove('menu-open');
    
    // Zoom to existing stop if present
    const stop = currentStops.find(s => s.target_date.startsWith(dateStr));
    if (stop && map) {
        map.panTo({ lat: stop.latitude, lng: stop.longitude });
        map.setZoom(10);
    }
}

async function initMap() {
  if (!GOOGLE_MAPS_API_KEY) {
    console.error('Google Maps API key is missing. Please set GOOGLE_MAPS_API_KEY environment variable during build.');
    const mapContainer = document.getElementById('map-container');
    if (mapContainer) {
        mapContainer.innerHTML = `
            <div class="flex flex-col items-center justify-center h-full text-center p-xl">
                <span class="material-symbols-outlined icon-xl text-gray mb-md">map</span>
                <h2 class="text-dark">Map Configuration Missing</h2>
                <p class="text-gray max-w-sm">The Google Maps API key is not set. Please configure <code>GOOGLE_MAPS_API_KEY</code> in your environment and rebuild the application.</p>
            </div>
        `;
    }
    return;
  }

  const { Map } = await loadGoogleMaps();
  const { Geocoder } = await importLibrary("geocoding");

  map = new Map(document.getElementById("map-container"), {
    center: { lat: 20, lng: 0 },
    zoom: 3,
    mapId: __GOOGLE_MAPS_MAP_ID__, 
    disableDefaultUI: false,
    clickableIcons: false
  });

  console.log('NavalPlan: Map Loaded Successfully');
  
  map.addListener('click', async (e) => {
      if (!currentVoyage || !selectedDate) return;
  
      const lat = e.latLng.lat();
      const lng = e.latLng.lng();
      const stop = currentStops.find(s => s.target_date.startsWith(selectedDate));
  
      // Reverse Geocoding
      const geocoder = new Geocoder();
      let locationName = `Location ${lat.toFixed(3)}, ${lng.toFixed(3)}`;
      
      try {
          const response = await geocoder.geocode({ location: e.latLng });
          if (response.results[0]) {
              locationName = response.results[0].formatted_address;
          }
      } catch (err) {
          console.error("Geocoding failed: " + err);
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

    const { AdvancedMarkerElement, PinElement } = await importLibrary("marker");
    const { InfoWindow } = await importLibrary("maps");
    const { Polyline } = await importLibrary("maps");

    // Sort stops by date
    const sortedStops = [...currentStops].sort((a, b) => 
        new Date(a.target_date) - new Date(b.target_date)
    );

    // Add Markers
    sortedStops.forEach((stop, index) => {
        const pin = new PinElement({
            glyphText: `${index + 1}`,
            glyphColor: "white",
            background: "#EA4335", // Google Maps Red
            borderColor: "#B31412",
        });

        const marker = new AdvancedMarkerElement({
            map: map,
            position: { lat: stop.latitude, lng: stop.longitude },
            content: pin.element,
            title: `${displayLocationName(stop.location_name)} (Day ${index + 1})`
        });
        
        marker.addListener('click', () => {
             const infoWindow = new InfoWindow({
                content: `<div style="color: black;"><b>${displayLocationName(stop.location_name)}</b><br>Day ${index + 1}</div>`
             });
             infoWindow.open(map, marker);
        });

        markers.push(marker);
    });

    // Fetch and Draw Facilities
    (async () => {
        for (const stop of sortedStops) {
            try {
                const b = await API.getBriefing(stop.id);
                if (b && b.facilities) {
                    b.facilities.forEach(f => {
                         if (f.latitude && f.longitude) {
                             let iconName = 'location_on'; // default
                             const type = (f.type || '').toLowerCase();
                             if (type.includes('anchorage')) iconName = 'anchor';
                             else if (type.includes('marina')) iconName = 'directions_boat';
                             else if (type.includes('bar')) iconName = 'local_bar';
                             else if (type.includes('restaurant')) iconName = 'restaurant';
                             
                             const iconDiv = document.createElement('div');
                             iconDiv.style.backgroundColor = '#000000';
                             iconDiv.style.borderRadius = '50%';
                             iconDiv.style.width = '28px';
                             iconDiv.style.height = '28px';
                             iconDiv.style.display = 'flex';
                             iconDiv.style.alignItems = 'center';
                             iconDiv.style.justifyContent = 'center';
                             iconDiv.style.border = '2px solid #ffffff';
                             iconDiv.style.boxShadow = '0 2px 5px rgba(0,0,0,0.5)';
                             iconDiv.innerHTML = `<span class="material-symbols-outlined" style="font-size: 18px; color: #ffffff;">${iconName}</span>`;

                             const fMarker = new AdvancedMarkerElement({
                                 map: map,
                                 position: { lat: f.latitude, lng: f.longitude },
                                 content: iconDiv,
                                 title: f.name
                             });

                             fMarker.addListener('click', () => {
                                 const infoWindow = new InfoWindow({
                                     content: `
                                         <div style="color: black;">
                                             <strong>${f.name}</strong><br>
                                             ${f.type}<br>
                                             <a href="https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}" target="_blank">View on Google Maps</a>
                                         </div>
                                     `
                                 });
                                 infoWindow.open(map, fMarker);
                             });

                             facilityMarkers.push(fMarker);
                         }
                    });
                }
            } catch (err) {
               // Ignore errors
            }
        }
    })();

    // Draw Line
    const coords = sortedStops.map(s => ({ lat: s.latitude, lng: s.longitude }));
    
    routePolyline = new Polyline({
      path: coords,
      geodesic: true,
      strokeColor: "#314c3b",
      strokeOpacity: 0,
      icons: [{
        icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, scale: 4 },
        offset: '0',
        repeat: '20px'
      }],
      map: map
    });
}
  
  function clearMap() {
      markers.forEach(m => m.map = null);
      markers = [];
      
      if (routePolyline) {
          routePolyline.setMap(null);
          routePolyline = null;
      }
  
      facilityMarkers.forEach(m => m.map = null);
      facilityMarkers = [];
  
      if (map && map.data) {
          map.data.forEach((feature) => {
              map.data.remove(feature);
          });
      }
  }  
async function renderMiniTideChart(canvasId, tideData, targetDateStr) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;

    if (!tideData || !tideData.events) return;

    // Helper to parse strings as wall-clock time (browser local)
    const parseLocal = (s) => {
        if (!s) return new Date(NaN);
        const clean = s.replace('Z', '').replace(' ', 'T');
        const final = clean.length === 10 ? clean + 'T00:00:00' : clean;
        return new Date(final);
    };

    const targetStart = parseLocal(targetDateStr).getTime();
    
    const points = [];
    tideData.events.forEach(e => {
        const d = parseLocal(e.time);
        if (!isNaN(d.getTime())) {
            const diffMs = d.getTime() - targetStart;
            const floatHours = diffMs / (1000 * 60 * 60);
            // Allow a buffer around the day so the line extends to edges
            if (floatHours >= -6 && floatHours <= 30) {
                 points.push({ x: floatHours, y: e.height_ft });
            }
        }
    });
    points.sort((a, b) => a.x - b.x);

    const Chart = await loadChart();
    const tideLevels = {
  
          id: 'tideLevels',
  
          afterDraw: (chart) => {
  
              const { ctx, scales: { x, y } } = chart;
  
              const yZero = y.getPixelForValue(0);
  
              
  
              // 1. Draw Zero Line (if within chart area)
  
              if (yZero >= y.top && yZero <= y.bottom) {
  
                  ctx.save();
  
                  ctx.beginPath();
  
                  ctx.setLineDash([2, 2]);
  
                  ctx.strokeStyle = 'rgba(0, 0, 0, 0.3)';
  
                  ctx.lineWidth = 1;
  
                  ctx.moveTo(x.left, yZero);
  
                  ctx.lineTo(x.right, yZero);
  
                  ctx.stroke();
  
                  ctx.restore();
  
              }
  
  
  
              // 2. Find and Label High/Low for the VISIBLE day (0 to 24)
  
              let maxPt = null;
  
              let minPt = null;
  
  
  
              points.forEach(p => {
  
                  if (p.x >= 0 && p.x <= 24) {
  
                      if (!maxPt || p.y > maxPt.y) maxPt = p;
  
                      if (!minPt || p.y < minPt.y) minPt = p;
  
                  }
  
              });
  
  
  
              const drawLabel = (pt, color, baseline) => {
  
                  if (!pt) return;
  
                  const xPos = x.getPixelForValue(pt.x);
  
                  const yPos = y.getPixelForValue(pt.y);
  
  
  
                  // Draw horizontal dash
  
                  ctx.save();
  
                  ctx.beginPath();
  
                  ctx.setLineDash([2, 2]);
  
                  ctx.strokeStyle = color;
  
                  ctx.lineWidth = 1;
  
                  // Short line around the point
  
                  ctx.moveTo(xPos - 10, yPos);
  
                  ctx.lineTo(xPos + 10, yPos);
  
                  ctx.stroke();
  
  
  
                  // Draw Text
  
                  ctx.font = 'bold 9px sans-serif';
  
                  ctx.fillStyle = color;
  
                  ctx.textAlign = 'center';
  
                  ctx.textBaseline = baseline;
  
                  
  
                  // Offset text slightly
  
                  const offset = baseline === 'bottom' ? -4 : 12;
  
                  ctx.fillText(`${pt.y.toFixed(1)}`, xPos, yPos + offset);
  
                  ctx.restore();
  
              };
  
  
  
              // Draw High
  
              if (maxPt) drawLabel(maxPt, '#d9534f', 'bottom');
  
              // Draw Low
  
              if (minPt) drawLabel(minPt, '#314c3b', 'top');
  
          }
  
      };
  
  
  
      const ctx = canvas.getContext('2d');
  
      new Chart(ctx, {
  
          type: 'line',
  
          data: {
  
              datasets: [{
  
                  data: points,
  
                  borderColor: '#0077be',
  
                  backgroundColor: 'rgba(0, 119, 190, 0.1)',
  
                  borderWidth: 2,
  
                  tension: 0.4,
  
                  pointRadius: 0,
  
                  fill: 'start'
  
              }]
  
          },
  
          options: {
  
              responsive: true,
  
              maintainAspectRatio: false,
  
              plugins: { legend: { display: false }, tooltip: { enabled: false } },
  
              scales: {
  
                  x: { type: 'linear', display: false, min: 0, max: 24 },
  
                  y: { 
  
                      display: false,
  
                      grace: '20%' // Add space for labels
  
                  }
  
              },
  
              layout: { padding: { top: 10, bottom: 10, left: 5, right: 5 } },
  
              animation: false
  
          },
  
          plugins: [tideLevels]
  
      });
  
  }

async function captureAndUploadMap(voyageId) {
    if (!currentStops || currentStops.length === 0) return false;

    const sortedStops = [...currentStops].sort((a, b) => 
        new Date(a.target_date) - new Date(b.target_date)
    );

    // Construct Static Map URL
    const baseUrl = "https://maps.googleapis.com/maps/api/staticmap";
    const size = "600x400";
    const scale = "2";
    const mapType = "roadmap";
    const key = GOOGLE_MAPS_API_KEY;

    let markersParam = "";
    const stopsToDraw = sortedStops.slice(0, 15); // Limit to avoid URL overflow
    stopsToDraw.forEach((s, i) => {
        markersParam += `&markers=color:red%7Clabel:${i+1}%7C${s.latitude},${s.longitude}`;
    });

    let pathParam = "&path=color:0x314c3bff|weight:4";
    stopsToDraw.forEach(s => {
        pathParam += `|${s.latitude},${s.longitude}`;
    });

    const url = `${baseUrl}?size=${size}&scale=${scale}&maptype=${mapType}${markersParam}${pathParam}&key=${key}`;

    try {
        const response = await fetch(url);
        if (!response.ok) throw new Error('Failed to fetch static map');
        const blob = await response.blob();
        await API.uploadVoyageMap(voyageId, blob);
        return true;
    } catch (err) {
        console.error('Failed to upload map image', err);
        return false;
    }
}

    async function handleShowReport() {
    if (!currentVoyage) return;
    
    const btn = document.getElementById('btn-export-voyage');
    const originalContent = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="material-symbols-outlined spin">sync</span>';

    try {
        // 1. Check/Capture Map
        let guide = await API.getVoyageGuide(currentVoyage.id).catch(() => null);
        
        // If guide doesn't exist or has no map, try to capture
        if (!guide || !guide.map_url) {
             const captured = await captureAndUploadMap(currentVoyage.id);
             if (captured) {
                 // Re-fetch guide to get the new map_url
                 guide = await API.getVoyageGuide(currentVoyage.id).catch(() => null);
             }
        }

        // 2. Fetch all data (Briefings)
        // Sort stops
        const sortedStops = [...currentStops].sort((a, b) => 
            new Date(a.target_date) - new Date(b.target_date)
        );
        
        // Fetch Briefings in parallel
        const briefingPromises = sortedStops.map(s => API.getBriefing(s.id).catch(() => null));
        
        const [briefings] = await Promise.all([
            Promise.all(briefingPromises)
        ]);

        const firstStopName = sortedStops.length > 0 ? displayLocationName(sortedStops[0].location_name) : null;

        const renderReferences = (refs) => {
            if (!refs || refs.length === 0) return '';
            return `<div class="ref-link">
                <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
            </div>`;
        };
        
        // 2. Build HTML
        let html = `
            <h1 class="report-title">${DOMPurify.sanitize(currentVoyage.title)}</h1>
            <p class="report-dates">
                ${new Date(currentVoyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'})} - ${new Date(currentVoyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}
            </p>
            <hr />
        `;

        // --- Consolidated View ---
        const gridCols = Math.min(sortedStops.length, 4);
        html += `<div class="report-section-wrapper">
            <h2 class="report-day-header brand-blue">Voyage Overview</h2>
            <div class="overview-grid grid-cols-${gridCols}">
        `;
            
        sortedStops.forEach((stop, idx) => {
            const briefing = briefings[idx] || {};
            const date = new Date(stop.target_date);
            const dateStr = date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });
            
            // Weather
            const w = briefing.weather_summary || {};
            const weatherIcon = getIconForWeather(w.condition);
            const temp = (w.temp_max_f && w.temp_min_f) ? `${Math.round(w.temp_max_f)}° / ${Math.round(w.temp_min_f)}°` : '--';

            // Sun
            const sun = briefing.sun_phase || {};
            const sunrise = sun.sunrise ? new Date(sun.sunrise).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'}) : '--:--';
            const sunset = sun.sunset ? new Date(sun.sunset).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'}) : '--:--';

            const canvasId = `miniTideChart_${idx}`;

            html += `
                <div class="overview-card">
                    <div class="overview-date">
                        ${dateStr}
                    </div>
                    <div class="overview-location" title="${DOMPurify.sanitize(stop.location_name)}">
                        ${DOMPurify.sanitize(displayLocationName(stop.location_name))}
                    </div>
                    
                    <div class="overview-weather">
                        <span class="material-symbols-outlined" style="font-size: 20px; color: #555;">${weatherIcon}</span>
                        <span class="overview-temp">${temp}</span>
                    </div>

                    <div class="overview-sun">
                        <div title="Sunrise"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">wb_twilight</span> ${sunrise}</div>
                        <div title="Sunset"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">bedtime</span> ${sunset}</div>
                    </div>

                    <div class="overview-chart">
                        <canvas id="${canvasId}"></canvas>
                    </div>
                </div>
            `;
        });
        html += `</div></div><hr />`;

        // --- Add Destination Guide Section ---
        if (guide) {
            html += `
                <div class="report-section-wrapper report-guide-bg">
                    <h2 class="report-day-header brand-green">Destination Guide</h2>
                    
                    ${guide.map_url ? `
                    <div class="report-map-container">
                        <img src="${guide.map_url}" alt="Voyage Map" class="report-map-img" />
                    </div>
                    ` : ''}

                    <h3>Summary</h3>
                    <p>${guide.summary || 'N/A'}</p>
                    
                    ${guide.sailing_season ? `
                    <div class="mt-md">
                        <h3>Sailing Season</h3>
                        <ul class="mt-sm">
                            <li><strong>Best Months:</strong> ${(guide.sailing_season.primary_season_months || []).join(', ')}</li>
                            <li><strong>Storm Season:</strong> ${(guide.sailing_season.storm_season_months || []).join(', ')}</li>
                            <li><strong>Notes:</strong> ${guide.sailing_season.notes || ''} ${renderReferences(guide.sailing_season.references)}</li>
                        </ul>
                    </div>
                    ` : ''}

                    ${(guide.hazards && guide.hazards.length > 0) ? `
                    <div class="mt-md">
                        <h3>Hazards</h3>
                        <ul class="mt-sm">
                            ${guide.hazards.map(h => {
                                const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm">(Info)</a>` : '';
                                return `<li><h4>${h.title}${link}</h4> <p>${h.description} ${renderReferences(h.references)}</p></li>`;
                            }).join('')}
                        </ul>
                    </div>
                    ` : ''}
                    
                     ${(guide.hubs && guide.hubs.length > 0) ? `
                    <div class="mt-md">
                        <h3>Major Hubs</h3>
                        <ul class="mt-sm">
                            ${guide.hubs.map(h => {
                                const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm">(Website)</a>` : '';
                                return `<li><h4>${h.name}${link}</h4> <p>${h.description} ${renderReferences(h.references)}</p></li>`;
                            }).join('')}
                        </ul>
                    </div>
                    ` : ''}

                    ${guide.charter_info ? `
                    <div class="mt-md">
                         <h3>Charter Info</h3>
                         <p class="mb-sm ml-md"><strong>Available:</strong> ${guide.charter_info.is_charter_destination ? 'Yes' : 'No'}</p>
                         <div class="ml-md">
                            <strong>Companies:</strong>
                            ${(guide.charter_info.companies && guide.charter_info.companies.length > 0) ? 
                                `<ul class="mt-xs">${guide.charter_info.companies.map(comp => {
                                    if (typeof comp === 'string') return `<li>${comp}</li>`;
                                    const nameLink = comp.url ? `<a href="${comp.url}" target="_blank">${comp.name}</a>` : comp.name;
                                    return `<li><h4>${nameLink}</h4> ${renderReferences(comp.references)}</li>`;
                                }).join('')}</ul>` : 'None listed'}
                         </div>
                    </div>
                    ` : ''}

                    ${(guide.country_info || guide.currencies) ? `
                    <div class="mt-md">
                        <h3>Country & Culture</h3>
                        <ul class="mt-sm">
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
                    <div class="mt-md">
                        <h3>Nearest Airports</h3>
                        <ul class="mt-sm">
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
                    <div class="mt-md">
                        <h3>Points of Interest</h3>
                        <ul class="mt-sm">
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
                <div class="report-daily-wrapper">
                    <h2 class="report-day-header">Day ${idx + 1}: ${DOMPurify.sanitize(displayLocationName(stop.location_name))}</h2>
                    <p class="report-day-date"><strong>Date:</strong> ${new Date(stop.target_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}</p>
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
                                <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em;">
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Summary</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${isInvalid(w.summary) ? 'N/A' : w.summary}</td>
                                    </tr>
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Conditions</th>
                                        <td class="briefing-td briefing-td-icon" style="border: 1px solid #ddd; padding: 8px; vertical-align: top; display: flex; align-items: center; gap: 0.5rem;">
                                            <span class="material-symbols-outlined" style="font-size: 1.2rem;">${getIconForWeather(w.condition)}</span>
                                            ${isInvalid(w.condition) ? 'N/A' : w.condition}
                                        </td>
                                    </tr>
                                    ${(w.temp_max_f || w.temp_min_f) ? `
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Temp</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">High: ${Math.round(w.temp_max_f)}°F &nbsp;|&nbsp; Low: ${Math.round(w.temp_min_f)}°F</td>
                                    </tr>
                                    ` : ''}
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Wind</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${isInvalid(w.wind_direction) ? 'N/A' : w.wind_direction} ${w.wind_speed_kt || '0'} kt</td>
                                    </tr>
                                    ${w.wave_height_ft > 0 ? `
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Waves</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${w.wave_height_ft} ft</td>
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
                            <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em;">
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Sunrise</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${formatTime(sun.sunrise)}</td>
                                </tr>
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Sunset</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${formatTime(sun.sunset)}</td>
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

                        return `<tr style="border: 1px solid #ddd;">
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${timeStr}</td>
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${e.type}</td>
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${e.height_ft} ft</td>
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
                                <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif;">
                                    <thead>
                                        <tr style="background-color: #f4f4f4;">
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Time</th>
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Type</th>
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Height</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${tideEventsHtml || '<tr><td colspan="3" class="briefing-no-data" style="border: 1px solid #ddd; padding: 8px;">No tide data for this date</td></tr>'}
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    `;
                }
                
                // Facilities
                const stopName = displayLocationName(stop.location_name);
                const isLastStopLoop = (idx === sortedStops.length - 1 && sortedStops.length > 1 && stopName === firstStopName);

                if (b.facilities && b.facilities.length > 0) {
                    if (isLastStopLoop) {
                        html += `
                        <div class="briefing-section">
                            <h3 class="briefing-header-icon">
                                <span class="material-symbols-outlined">warehouse</span>
                                Facilities
                            </h3>
                            <p class="text-gray italic">Facilities omitted as this is the return to the starting location.</p>
                        </div>`;
                    } else {
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
                         else if (typeLower.includes('bar')) icon = 'local_bar';
                         else if (typeLower.includes('restaurant')) icon = 'restaurant';

                         let detailsHtml = '';
                         if (typeof f.details === 'string') {
                             detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                         } else if (f.details && typeof f.details === 'object') {
                        // Table format for details
                        let rows = `
                            <tr style="border-bottom: 1px solid #eee;">
                                <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Type</th>
                                <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${f.type}</td>
                            </tr>
                        `;
                        
                        rows += Object.entries(f.details)
                            .filter(([_, v]) => {
                                if (!v) return false;
                                const sv = String(v).toLowerCase().trim();
                                return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                            })
                            .map(([k, v]) => `
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width capitalize" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">${k.replace(/_/g, ' ')}</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${v}</td>
                                </tr>
                            `).join('');
                        
                        detailsHtml = `<table class="briefing-table mt-0" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em; margin-top: 0.5rem;">${rows}</table>`;
                    }

                    let locHtml = '';
                    if (f.latitude && f.longitude) {
                        const googleMapsUrl = `https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}`;
                        locHtml = `
                            <p class="map-link-p">
                                <span class="material-symbols-outlined icon-md icon-bottom">my_location</span>
                                ${f.latitude.toFixed(4)}, ${f.longitude.toFixed(4)}
                                <a href="${googleMapsUrl}" target="_blank" class="map-link-a">(Open Map)</a>
                            </p>
                        `;
                    }

                    return `
                        <li class="facility-item">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined icon-lg">${icon}</span>
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
        
        content.innerHTML = DOMPurify.sanitize(html, { ADD_ATTR: ['target'] });
        modal.classList.remove('hidden');
        modalOverlay.classList.remove('hidden');

        // Render all charts (including mini ones)
        for (const [idx, stop] of sortedStops.entries()) {
            const b = briefings[idx];
            if (b && b.tides && b.tides.events) {
                // Main Chart
                await renderTideChart(`tideChart_${idx}`, b.tides, stop.target_date);
                // Mini Chart
                await renderMiniTideChart(`miniTideChart_${idx}`, b.tides, stop.target_date);
            }
        }

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
            <div class="loading-state">
                <span class="material-symbols-outlined spin loading-icon">sync</span>
                <p class="font-xs">Gathering local knowledge, seasonal data, and regional hazards.</p>
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

    // Inject Share/Snapshot controls if not present (create only once, update handler always)
    const headerControls = modal.querySelector('.modal-header-row .flex.gap-sm');
    
    let btnSnapshot = document.getElementById('btn-snapshot-guide');
    if (!btnSnapshot) {
        btnSnapshot = document.createElement('button');
        btnSnapshot.id = 'btn-snapshot-guide';
        btnSnapshot.className = 'btn secondary p-xs font-sm';
        btnSnapshot.title = 'Update Map Snapshot';
        btnSnapshot.innerHTML = '<span class="material-symbols-outlined icon-lg icon-align">camera_alt</span>';
        headerControls.insertBefore(btnSnapshot, headerControls.firstChild);
    }

    // Always update handler to use current guide context
    btnSnapshot.onclick = async () => {
            btnSnapshot.disabled = true;
            const icon = btnSnapshot.querySelector('span');
            icon.classList.add('spin');
            icon.textContent = 'sync';
            
            // We need to briefly hide the modal to capture the map if it's behind
            modal.classList.add('hidden');
            modalOverlay.classList.add('hidden');
            
            // Small delay to allow render
            await new Promise(r => setTimeout(r, 200));

            const success = await captureAndUploadMap(guide.voyage_id);
            
            modal.classList.remove('hidden');
            modalOverlay.classList.remove('hidden');
            
            icon.classList.remove('spin');
            icon.textContent = 'camera_alt';
            btnSnapshot.disabled = false;
            
            if (success) {
                showNotification('Snapshot Saved', 'The map view has been updated for the public report.');
                // Refresh image in modal if present
                const img = document.querySelector('#guide-content .report-map-img');
                if (img) {
                    // Cache bust
                    const src = img.src.split('?')[0];
                    img.src = `${src}?t=${Date.now()}`;
                }
            } else {
                alert('Failed to capture map. Ensure the map is visible.');
            }
    };

    let btnShare = document.getElementById('btn-share-guide');
    if (!btnShare) {
        btnShare = document.createElement('button');
        btnShare.id = 'btn-share-guide';
        btnShare.className = 'btn secondary p-xs font-sm ml-sm';
        btnShare.title = 'Share Guide';
        btnShare.innerHTML = '<span class="material-symbols-outlined icon-lg icon-align">share</span>';
        headerControls.insertBefore(btnShare, headerControls.firstChild);
    }
    
    // Always update handler
    btnShare.onclick = () => {
            handleShareClick(guide);
    };

    const renderReferences = (refs) => {
        if (!refs || refs.length === 0) return '';
        return `<div class="ref-link">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
        </div>`;
    };

    let html = '';
    
    // Map Snapshot
    if (guide.map_url) {
        // Cache bust
        const sep = guide.map_url.includes('?') ? '&' : '?';
        const url = `${guide.map_url}${sep}t=${Date.now()}`;
        html += `
            <div class="briefing-section">
                 <img src="${url}" alt="Voyage Map" class="report-map-img" style="width:100%; border-radius: 4px; border: 1px solid #ccc; display: block; margin-bottom: 1rem;" />
            </div>
        `;
    }

    html += `
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
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Info)</a>` : '';
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
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Website)</a>` : '';
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
             companiesHtml = '<ul class="charter-list">' + 
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
                <div class="mt-sm"><strong>Companies:</strong> ${companiesHtml}</div>
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

    content.innerHTML = DOMPurify.sanitize(html);

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
        <div class="loading-state">
            <span class="material-symbols-outlined spin loading-icon">sync</span>
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
                content.innerHTML = '<div class="error-state"><p><strong>Research timed out.</strong></p></div>';
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

async function toggleDiscoveryMode(active) {
    currentMode = active ? 'discovery' : 'planner';
    const discoveryControls = document.getElementById('discovery-controls');
    const sidebar = document.getElementById('sidebar');
    
    if (active) {
        discoveryControls.classList.remove('hidden');
        sidebar.classList.add('hidden');
        const currentMonth = new Date().getMonth() + 1;
        document.getElementById('month-slider').value = currentMonth;
        const months = [
            'January', 'February', 'March', 'April', 'May', 'June',
            'July', 'August', 'September', 'October', 'November', 'December'
        ];
        document.getElementById('month-display').textContent = months[currentMonth - 1];
        
        clearMap(); // Clear existing markers/routes
        loadDiscoveryRegions(currentMonth);
        
        // Zoom out to world view
        if (map) {
             map.panTo({ lat: 20, lng: 0 });
             map.setZoom(3);
        }

        // Show Intro Modal if first time
        if (!localStorage.getItem('seenDiscoveryIntro')) {
            const introModal = document.getElementById('modal-discovery-intro');
            const overlay = document.getElementById('modal-overlay');
            if (introModal && overlay) {
                introModal.classList.remove('hidden');
                overlay.classList.remove('hidden');
                localStorage.setItem('seenDiscoveryIntro', 'true');
            }
        }
    } else {
        discoveryControls.classList.add('hidden');
        sidebar.classList.remove('hidden');
        
        // Remove discovery layers
        if (map && map.data) {
             map.data.forEach((feature) => {
                map.data.remove(feature);
            });
        }
        
        if (currentVoyage) {
            selectVoyage(currentVoyage); // Restore voyage view
        } else {
            showVoyageList();
        }
    }
}

async function loadDiscoveryRegions(month) {
    try {
        discoveryRegions = await API.getDiscoveryRegions(month);
        renderDiscoveryLayer();
    } catch (err) {
        console.error('Failed to load discovery regions:', err);
    }
}

async function renderDiscoveryLayer() {
    if (!map) return;

    // Clear existing data
    map.data.forEach((feature) => {
        map.data.remove(feature);
    });

    // Convert to GeoJSON
    const geojson = {
        type: 'FeatureCollection',
        features: discoveryRegions.map(r => ({
            type: 'Feature',
            geometry: {
                type: 'Polygon',
                coordinates: r.geometry.coordinates.map(ring => smoothPolygon(ring, 3))
            },
            properties: {
                id: r.id,
                name: r.name,
                tier: r.tier,
                suitability_score: r.suitability_score,
                is_hidden_gem: r.is_hidden_gem,
                summary: r.summary,
                avg_wind_speed_knots: r.avg_wind_speed_knots,
                avg_temp_c: r.avg_temp_c,
                deep_cut_reasoning: r.deep_cut_reasoning
            }
        }))
    };

    map.data.addGeoJson(geojson);

    // Styling
    map.data.setStyle((feature) => {
        const tier = feature.getProperty('tier');
        let color = '#0077be'; // Standard Blue
        let strokeColor = '#005fa3';

        if (tier === 'Hidden Gem') {
            color = '#9c27b0'; // Purple
            strokeColor = '#6a1b9a';
        } else if (tier === 'Regional Favorite') {
            color = '#ff9800'; // Orange
            strokeColor = '#ef6c00';
        } else if (tier === 'Challenging') {
            color = '#d32f2f'; // Red
            strokeColor = '#b71c1c';
        }

        return {
            fillColor: color,
            fillOpacity: 0.6,
            strokeColor: strokeColor,
            strokeWeight: 2
        };
    });

    // Click handler
    map.data.addListener('click', (event) => {
        const props = {
            id: event.feature.getProperty('id'),
            name: event.feature.getProperty('name'),
            tier: event.feature.getProperty('tier'),
            suitability_score: event.feature.getProperty('suitability_score'),
            is_hidden_gem: event.feature.getProperty('is_hidden_gem'),
            summary: event.feature.getProperty('summary'),
            avg_wind_speed_knots: event.feature.getProperty('avg_wind_speed_knots'),
            avg_temp_c: event.feature.getProperty('avg_temp_c'),
            deep_cut_reasoning: event.feature.getProperty('deep_cut_reasoning')
        };
        const month = document.getElementById('month-slider').value;
        showRegionBriefing(props, month);
    });

    // Hover effect
    map.data.addListener('mouseover', () => {
        map.setOptions({ draggableCursor: 'pointer' });
    });
    map.data.addListener('mouseout', () => {
        map.setOptions({ draggableCursor: '' });
    });
}

function calculateGeometryArea(geometry) {
    if (!geometry) return 0;
    if (geometry.type === 'Polygon') {
        return calculatePolygonArea(geometry.coordinates);
    } else if (geometry.type === 'MultiPolygon') {
        return geometry.coordinates.reduce((sum, polygonCoords) => sum + calculatePolygonArea(polygonCoords), 0);
    }
    return 0;
}

function calculatePolygonArea(coordinates) {
    let area = 0;
    if (coordinates && coordinates.length > 0) {
        // Outer ring is the first element
        const ring = coordinates[0]; 
        for (let i = 0; i < ring.length - 1; i++) {
            area += ring[i][0] * ring[i+1][1] - ring[i+1][0] * ring[i][1];
        }
    }
    return Math.abs(area / 2);
}


async function showRegionBriefing(props, month) {
    const modal = document.getElementById('modal-region-briefing');
    const title = document.getElementById('region-title');
    const content = document.getElementById('region-briefing-content');
    const overlay = document.getElementById('modal-overlay');

    // For now, use the data we already have from the list
    // In a full implementation, we might fetch detailed stats
    const region = discoveryRegions.find(r => r.id === props.id);
    
    title.textContent = props.name;
    
    content.innerHTML = DOMPurify.sanitize(`
        <div class="briefing-section">
            <div class="flex justify-between align-center mb-md">
                <span class="badge ${props.tier === 'Hidden Gem' ? 'badge-gem' : props.tier === 'Regional Favorite' ? 'badge-regional' : props.tier === 'Challenging' ? 'badge-challenging' : 'badge-standard'}">
                    ${props.tier || (props.is_hidden_gem ? 'Hidden Gem' : 'Standard Destination')}
                </span>
                <span class="font-sm text-gray">Suitability: <strong>${props.suitability_score}/100</strong></span>
            </div>
            
            <p class="mb-lg"><strong>Summary:</strong> ${props.summary}</p>
            
            ${region && region.deep_cut_reasoning ? `
                <div class="report-guide-bg p-md border-radius">
                    <h4 class="mt-0">The Deep Cut Factor</h4>
                    <p class="mb-0">${region.deep_cut_reasoning}</p>
                </div>
            ` : ''}

            <div class="weather-box mt-lg">
                <table class="briefing-table">
                    <tr>
                        <th class="briefing-th">Typical Wind</th>
                        <td class="briefing-td">${region?.avg_wind_speed_knots || '??'} knots</td>
                    </tr>
                    <tr>
                        <th class="briefing-th">Avg Temp</th>
                        <td class="briefing-td">${region?.avg_temp_c || '??'}°C (${Math.round((region?.avg_temp_c || 0) * 9/5 + 32)}°F)</td>
                    </tr>
                </table>
            </div>

            ${(currentUser && currentUser.is_admin) ? `
            <div class="mt-xl flex justify-end">
                <button id="btn-delete-region-seasonality" class="btn-text btn-danger font-sm">
                    <span class="material-symbols-outlined font-md">delete</span>
                    Remove for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}
                </button>
            </div>` : ''}
        </div>
    `);

    modal.classList.remove('hidden');
    overlay.classList.remove('hidden');

    const hide = () => {
        modal.classList.add('hidden');
        overlay.classList.add('hidden');
    };

    document.getElementById('btn-close-region-briefing').onclick = hide;
    overlay.onclick = hide;

    const btnDelete = document.getElementById('btn-delete-region-seasonality');
    if (btnDelete) {
        btnDelete.onclick = async () => {
            if (!confirm(`Are you sure you want to remove ${props.name} from the discovery list for ${new Date(2000, month - 1).toLocaleString('default', { month: 'long' })}?`)) {
                return;
            }

            try {
                await API.deleteDiscoverySeasonality(props.id, month);
                hide();
                // Refresh discovery regions
                loadDiscoveryRegions(month);
            } catch (err) {
                console.error('Failed to delete region seasonality:', err);
                alert('Failed to remove region. Please try again.');
            }
        };
    }
}

async function initSharedMode(token) {
    document.body.classList.add('shared-view');
    const app = document.getElementById('app');
    // Clear existing UI
    app.innerHTML = "<div class=\"loading-state\"><span class=\"material-symbols-outlined spin loading-icon\">sync</span><p>Loading Captain's Report...</p></div>";

    try {
        const resp = await API.getPublicVoyageGuide(token);
        renderSharedReport(resp, app);
    } catch (err) {
        console.error(err);
        app.innerHTML = '<div class="error-state text-center p-xl"><h2 class="text-dark">Report Not Found</h2><p>This link may have expired or is invalid.</p></div>';
    }
}

function renderSharedReport(data, container) {
    const guide = data.guide || {};
    const voyage = data.voyage || {};
    const stops = data.stops || [];
    const briefings = data.briefings || [];
    const mapUrl = data.map_url;

    // Sort stops
    stops.sort((a, b) => new Date(a.target_date) - new Date(b.target_date));

    const firstStopName = stops.length > 0 ? displayLocationName(stops[0].location_name) : null;

    const renderReferences = (refs) => {
        if (!refs || refs.length === 0) return '';
        return `<div class="ref-link">
            <strong>Refs:</strong> ${refs.map((r, i) => `<a href="${r}" target="_blank" class="ref-anchor">[${i+1}]</a>`).join('')}
        </div>`;
    };

    const isInvalid = (v) => {
        if (!v) return true;
        const sv = String(v).toLowerCase().trim();
        return sv === 'n/a' || sv === 'unknown' || sv === 'not specified';
    };

    let html = `
        <div class="shared-container max-w-4xl mx-auto p-md">
            <header class="mb-xl text-center">
                <h1 class="brand-font text-xxl brand-blue mb-sm">NavalPlan</h1>
                <h2 class="text-dark">${DOMPurify.sanitize(voyage.title || "Captain's Report")}</h2>
                <p class="text-gray italic">
                    ${new Date(voyage.start_date).toLocaleDateString(undefined, {timeZone: 'UTC'})} - ${new Date(voyage.end_date).toLocaleDateString(undefined, {timeZone: 'UTC'})}
                </p>
            </header>

            ${mapUrl ? `
            <div class="report-map-container mb-xl shadow-lg border-radius overflow-hidden">
                <img src="${mapUrl}" alt="Voyage Map" class="w-full block" />
            </div>
            ` : ''}

            <div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mb-xl">
                <h3 class="brand-blue mt-0 mb-md">Voyage Overview</h3>
                <table class="overview-table" style="width: 100%; border-collapse: separate; border-spacing: 10px; font-family: sans-serif;">
                    ${(() => {
                        let tableHtml = '';
                        stops.forEach((stop, idx) => {
                            if (idx % 4 === 0) tableHtml += '<tr>';
                            
                            const b = briefings.find(br => br.stop_id === stop.id) || {};
                            const date = new Date(stop.target_date);
                            const dateStr = date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });
                            
                            // Weather
                            const w = b.weather_summary || {};
                            const weatherIcon = getIconForWeather(w.condition);
                            const temp = (w.temp_max_f && w.temp_min_f) ? `${Math.round(w.temp_max_f)}° / ${Math.round(w.temp_min_f)}°` : '--';

                            // Sun
                            const sun = b.sun_phase || {};
                            const sunrise = sun.sunrise ? new Date(sun.sunrise).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'}) : '--:--';
                            const sunset = sun.sunset ? new Date(sun.sunset).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'}) : '--:--';

                            const canvasId = `sharedMiniTideChart_${idx}`;

                            tableHtml += `
                                <td class="overview-card" style="border: 1px solid #ccc; border-radius: 8px; padding: 10px; background: #fff; vertical-align: top; width: 25%; min-width: 150px;">
                                    <div class="overview-date" style="font-weight: bold; border-bottom: 1px solid #eee; padding-bottom: 5px; margin-bottom: 5px; text-align: center; font-size: 0.9rem;">${dateStr}</div>
                                    <div class="overview-location" title="${DOMPurify.sanitize(stop.location_name)}" style="font-size: 0.8rem; text-align: center; margin-bottom: 5px; color: #555; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
                                        ${DOMPurify.sanitize(displayLocationName(stop.location_name))}
                                    </div>
                                    <div class="overview-weather" style="display: flex; align-items: center; justify-content: center; gap: 8px; margin-bottom: 5px;">
                                        <span class="material-symbols-outlined" style="font-size: 20px; color: #555;">${weatherIcon}</span>
                                        <span class="overview-temp" style="font-size: 1rem; font-weight: bold;">${temp}</span>
                                    </div>
                                    <div class="overview-sun" style="display: flex; justify-content: space-around; font-size: 0.75rem; color: #666; margin-bottom: 5px;">
                                        <div title="Sunrise"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">wb_twilight</span> ${sunrise}</div>
                                        <div title="Sunset"><span class="material-symbols-outlined" style="font-size: 12px; vertical-align: middle;">bedtime</span> ${sunset}</div>
                                    </div>
                                    <div class="overview-chart" style="position: relative; height: 120px; width: 100%;">
                                        <canvas id="${canvasId}" data-tide-json='${JSON.stringify(b.tides || {}).replace(/'/g, "&apos;")}' data-date="${stop.target_date}"></canvas>
                                    </div>
                                </td>
                            `;

                            if (idx % 4 === 3 || idx === stops.length - 1) tableHtml += '</tr>';
                        });
                        return tableHtml;
                    })()}
                </table>
            </div>

            <div class="report-section-wrapper bg-white p-lg shadow-sm border-radius">
                <h3 class="brand-green mt-0 mb-md">Voyage Summary</h3>
                <p>${DOMPurify.sanitize(guide.summary || 'No summary available.')}</p>
            </div>
    `;

    // Sailing Season
    if (guide.sailing_season) {
        const s = guide.sailing_season;
        html += `
            <div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mt-lg">
                <h3 class="brand-green mt-0 mb-md">Sailing Season</h3>
                <ul class="facility-list">
                    <li class="facility-item"><strong>Best Months:</strong> ${(s.primary_season_months || []).join(', ') || 'N/A'}</li>
                    <li class="facility-item"><strong>Storm Season:</strong> ${(s.storm_season_months || []).join(', ') || 'N/A'} (${s.storm_risk_level || 'Unknown Risk'})</li>
                    <li class="facility-item"><strong>Notes:</strong> ${s.notes || ''} ${renderReferences(s.references)}</li>
                </ul>
            </div>
        `;
    }

    // Hazards
    if (guide.hazards && guide.hazards.length > 0) {
        html += `<div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mt-lg">
            <h3 class="brand-red mt-0 mb-md">⚠️ Hazards</h3>
            <ul class="facility-list">`;
        guide.hazards.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Info)</a>` : '';
            html += `<li class="facility-item">
                <h4 class="brand-red">${h.title}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Hubs
    if (guide.hubs && guide.hubs.length > 0) {
        html += `<div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mt-lg">
            <h3 class="brand-green mt-0 mb-md">Major Hubs</h3>
            <ul class="facility-list">`;
        guide.hubs.forEach(h => {
            const link = h.url ? ` <a href="${h.url}" target="_blank" class="font-sm ml-sm">(Website)</a>` : '';
            html += `<li class="facility-item">
                <h4>${h.name}${link}</h4>
                <p>${h.description}</p>
                ${renderReferences(h.references)}
            </li>`;
        });
        html += `</ul></div>`;
    }

    // Country Info
    if (guide.country_info || guide.currencies) {
        const c = guide.country_info || {};
        const curs = guide.currencies || [];
        
        let currencyHtml = 'N/A';
        if (curs.length > 0) {
            currencyHtml = curs.map(cur => `${cur.name} (${cur.code}) - ${cur.symbol || ''}`).join(', ');
        }

        html += `
            <div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mt-lg">
                <h3 class="brand-green mt-0 mb-md">Country & Culture</h3>
                <ul class="facility-list">
                    <li class="facility-item"><strong>Country:</strong> ${c.name || 'N/A'}</li>
                    <li class="facility-item"><strong>Language:</strong> ${c.languages ? c.languages.join(', ') : 'N/A'}</li>
                    <li class="facility-item"><strong>Timezone:</strong> ${c.timezone || 'N/A'}</li>
                    <li class="facility-item"><strong>Emergency:</strong> ${c.emergency_numbers ? Object.entries(c.emergency_numbers).map(([k,v]) => `${k}: ${v}`).join(', ') : 'N/A'}</li>
                    <li class="facility-item"><strong>Currency:</strong> ${currencyHtml}</li>
                </ul>
            </div>
        `;
    }

    // STOPS & BRIEFINGS
    if (stops.length > 0) {
        html += `<div class="mt-xl"><h2 class="text-center brand-blue mb-lg">Daily Itinerary</h2>`;
        
        stops.forEach((stop, idx) => {
            const b = briefings.find(br => br.stop_id === stop.id);
            const dateStr = new Date(stop.target_date).toLocaleDateString(undefined, {timeZone: 'UTC', weekday: 'long', month: 'long', day: 'numeric'});
            
            html += `
                <div class="report-section-wrapper bg-white p-lg shadow-sm border-radius mt-lg">
                    <h3 class="brand-green mt-0 mb-xs">Day ${idx + 1}: ${DOMPurify.sanitize(displayLocationName(stop.location_name))}</h3>
                    <p class="text-gray mb-md font-sm"><strong>Date:</strong> ${dateStr}</p>
            `;

            if (b) {
                 // Weather
                if (b.weather_summary) {
                    const w = b.weather_summary;
                    html += `
                        <div class="briefing-section">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined">${getIconForWeather(w.condition)}</span>
                                Weather
                            </h4>
                            <div class="weather-box">
                                <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em;">
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Summary</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${isInvalid(w.summary) ? 'N/A' : w.summary}</td>
                                    </tr>
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Conditions</th>
                                        <td class="briefing-td briefing-td-icon" style="border: 1px solid #ddd; padding: 8px; vertical-align: top; display: flex; align-items: center; gap: 0.5rem;">
                                            <span class="material-symbols-outlined" style="font-size: 1.2rem;">${getIconForWeather(w.condition)}</span>
                                            ${isInvalid(w.condition) ? 'N/A' : w.condition}
                                        </td>
                                    </tr>
                                    ${(w.temp_max_f || w.temp_min_f) ? `
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Temp</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">High: ${Math.round(w.temp_max_f)}°F &nbsp;|&nbsp; Low: ${Math.round(w.temp_min_f)}°F</td>
                                    </tr>
                                    ` : ''}
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Wind</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${isInvalid(w.wind_direction) ? 'N/A' : w.wind_direction} ${w.wind_speed_kt || '0'} kt</td>
                                    </tr>
                                    ${w.wave_height_ft > 0 ? `
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Waves</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${w.wave_height_ft} ft</td>
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
                        <h4 class="briefing-header-icon">
                            <span class="material-symbols-outlined">wb_twilight</span>
                            Sun Phase
                        </h4>
                        <div class="weather-box">
                            <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em;">
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Sunrise</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${formatTime(sun.sunrise)}</td>
                                </tr>
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">Sunset</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${formatTime(sun.sunset)}</td>
                                </tr>
                            </table>
                        </div>
                    </div>
                    `;
                }

                // Tides
                if (b.tides && b.tides.events) {
                    const canvasId = `sharedTideChart_${idx}`;
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

                        return `<tr style="border: 1px solid #ddd;">
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${timeStr}</td>
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${e.type}</td>
                            <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${e.height_ft} ft</td>
                        </tr>`;
                    }).join('');

                    const [y, m, d] = targetDateYMD.split('-');
                    const displayDateHeader = `${m}/${d}/${y}`;

                    html += `
                        <div class="briefing-section">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined">waves</span>
                                Tides (${b.tides.station_name || 'Station Unknown'}) - ${displayDateHeader}
                            </h4>
                            <div class="tide-box" style="margin-bottom:1rem;">
                                <div style="height:200px; width:100%; position:relative;">
                                    <canvas id="${canvasId}" data-tide-json='${JSON.stringify(b.tides).replace(/'/g, "&apos;")}' data-date="${stop.target_date}"></canvas>
                                </div>
                                <table class="briefing-table" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif;">
                                    <thead>
                                        <tr style="background-color: #f4f4f4;">
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Time</th>
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Type</th>
                                            <th class="briefing-th" style="border: 1px solid #ddd; padding: 8px; text-align: left; font-weight: bold;">Height</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${tideEventsHtml || '<tr><td colspan="3" class="briefing-no-data" style="border: 1px solid #ddd; padding: 8px;">No tide data for this date</td></tr>'}
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    `;
                }

                // Facilities
                const stopName = displayLocationName(stop.location_name);
                const isLastStopLoop = (idx === stops.length - 1 && stops.length > 1 && stopName === firstStopName);

                if (b.facilities && b.facilities.length > 0) {
                    if (isLastStopLoop) {
                        html += `
                        <div class="briefing-section">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined">warehouse</span>
                                Facilities
                            </h4>
                            <p class="text-gray italic">Facilities omitted as this is the return to the starting location.</p>
                        </div>`;
                    } else {
                     html += `
                        <div class="briefing-section">
                            <h4 class="briefing-header-icon">
                                <span class="material-symbols-outlined">warehouse</span>
                                Facilities
                            </h4>
                            <ul class="facility-list">
                                ${b.facilities.map(f => {
                         let icon = 'place';
                         const typeLower = (f.type || '').toLowerCase();
                         if (typeLower.includes('anchorage')) icon = 'anchor';
                         else if (typeLower.includes('marina')) icon = 'storefront';
                         else if (typeLower.includes('mooring')) icon = 'crisis_alert';
                         else if (typeLower.includes('bar')) icon = 'local_bar';
                         else if (typeLower.includes('restaurant')) icon = 'restaurant';

                         let detailsHtml = '';
                         if (typeof f.details === 'string') {
                             detailsHtml = `<p><strong>Type:</strong> ${f.type}</p><p>${f.details}</p>`;
                         } else if (f.details && typeof f.details === 'object') {
                            let rows = `
                                <tr style="border-bottom: 1px solid #eee;">
                                    <th class="briefing-th briefing-table-label-width" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9; width: 120px;">Type</th>
                                    <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${f.type}</td>
                                </tr>
                            `;
                            rows += Object.entries(f.details)
                                .filter(([_, v]) => {
                                    if (!v) return false;
                                    const sv = String(v).toLowerCase().trim();
                                    return sv !== 'n/a' && sv !== '' && sv !== 'unknown' && sv !== 'not specified';
                                })
                                .map(([k, v]) => `
                                    <tr style="border-bottom: 1px solid #eee;">
                                        <th class="briefing-th briefing-table-label-width capitalize" style="border: 1px solid #ddd; padding: 8px; text-align: left; background-color: #f9f9f9;">${k.replace(/_/g, ' ')}</th>
                                        <td class="briefing-td" style="border: 1px solid #ddd; padding: 8px; vertical-align: top;">${v}</td>
                                    </tr>
                                `).join('');
                            detailsHtml = `<table class="briefing-table mt-0" style="width: 100%; border-collapse: collapse; border: 1px solid #ddd; font-family: sans-serif; font-size: 0.9em; margin-top: 0.5rem;">${rows}</table>`;
                        }

                        let locHtml = '';
                        if (f.latitude && f.longitude) {
                            const googleMapsUrl = `https://www.google.com/maps/search/?api=1&query=${f.latitude},${f.longitude}`;
                            locHtml = `
                                <p class="map-link-p">
                                    <span class="material-symbols-outlined icon-md icon-bottom">my_location</span>
                                    ${f.latitude.toFixed(4)}, ${f.longitude.toFixed(4)}
                                    <a href="${googleMapsUrl}" target="_blank" class="map-link-a">(Open Map)</a>
                                </p>
                            `;
                        }

                        return `
                            <li class="facility-item">
                                <h4 class="briefing-header-icon">
                                    <span class="material-symbols-outlined icon-lg">${icon}</span>
                                    ${f.name}
                                </h4>
                                ${locHtml}
                                ${detailsHtml}
                                ${renderReferences(f.references)}
                            </li>
                        `;
                    }).join('')}
                    </ul></div>`;
                    }
                }
            } else {
                 html += `<p class="text-gray italic">No briefing data available.</p>`;
            }
            html += `</div>`; // End of report-section-wrapper
        });
        html += `</div>`; // End of Stops Wrapper
    }

    html += `
        <footer class="mt-xl text-center text-gray font-sm p-lg">
            <p>Generated by NavalPlan</p>
        </footer>
        </div>
    `;

    container.innerHTML = DOMPurify.sanitize(html);

    // Render Charts
    setTimeout(() => {
        // Main Tide Charts
        const charts = container.querySelectorAll('canvas[id^="sharedTideChart_"]');
        charts.forEach(canvas => {
            try {
                const tideJson = canvas.getAttribute('data-tide-json');
                const date = canvas.getAttribute('data-date');
                if (tideJson && date) {
                    const tideData = JSON.parse(tideJson);
                    renderTideChart(canvas.id, tideData, date);
                }
            } catch (e) {
                console.error("Failed to render shared tide chart", e);
            }
        });

        // Mini Tide Charts
        const miniCharts = container.querySelectorAll('canvas[id^="sharedMiniTideChart_"]');
        miniCharts.forEach(canvas => {
            try {
                const tideJson = canvas.getAttribute('data-tide-json');
                const date = canvas.getAttribute('data-date');
                if (tideJson && date) {
                    const tideData = JSON.parse(tideJson);
                    renderMiniTideChart(canvas.id, tideData, date);
                }
            } catch (e) {
                console.error("Failed to render shared mini tide chart", e);
            }
        });
    }, 100);
}

async function handleShareClick(guide) {
    if (!currentVoyage || currentVoyage.id !== guide.voyage_id) {
         alert('Error: Voyage context lost.');
         return;
    }

    const content = `
        <div class="text-left">
            <h3 class="mt-0">Public Sharing</h3>
            <p class="text-gray mb-md">Share this guide with friends and crew.</p>
            
            <div class="form-group">
                <label class="flex align-center gap-sm" style="cursor:pointer;">
                    <input type="checkbox" id="chk-share-public">
                    <strong>Enable Public Link</strong>
                </label>
            </div>

            <div id="share-link-container" class="hidden mt-md">
                <label>Public Link</label>
                <div class="flex gap-sm">
                    <input type="text" id="share-link-input" readonly value="" class="w-full p-sm border-radius border">
                    <button id="btn-copy-share" class="btn secondary">Copy</button>
                </div>
            </div>
            
            <div class="mt-xl text-right">
                <button id="btn-close-share" class="btn primary">Done</button>
            </div>
        </div>
    `;
    
    let shareModal = document.getElementById('modal-share-dynamic');
    if (!shareModal) {
        shareModal = document.createElement('div');
        shareModal.id = 'modal-share-dynamic';
        shareModal.className = 'modal hidden';
        document.body.appendChild(shareModal);
    }
    
    shareModal.innerHTML = DOMPurify.sanitize(content);
    
    const chk = shareModal.querySelector('#chk-share-public');
    const linkInput = shareModal.querySelector('#share-link-input');
    const linkContainer = shareModal.querySelector('#share-link-container');

    // Init State
    chk.checked = currentVoyage.is_public;
    if (currentVoyage.is_public) {
        linkInput.value = `${window.location.origin}/shared/${currentVoyage.share_token}`;
        linkContainer.classList.remove('hidden');
    }
    
    document.getElementById('modal-overlay').classList.remove('hidden');
    shareModal.classList.remove('hidden');
    
    chk.onchange = async () => {
        try {
            if (chk.checked) {
                const res = await API.enableSharing(currentVoyage.id);
                currentVoyage.is_public = true;
                currentVoyage.share_token = res.share_token;
                
                linkInput.value = `${window.location.origin}/shared/${res.share_token}`;
                linkContainer.classList.remove('hidden');
            } else {
                await API.disableSharing(currentVoyage.id);
                currentVoyage.is_public = false;
                currentVoyage.share_token = null;
                linkContainer.classList.add('hidden');
            }
        } catch (err) {
            console.error(err);
            alert('Failed to update sharing settings');
            chk.checked = !chk.checked;
        }
    };
    
    shareModal.querySelector('#btn-copy-share').onclick = () => {
        linkInput.select();
        document.execCommand('copy');
        const btn = shareModal.querySelector('#btn-copy-share');
        const orig = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => btn.textContent = orig, 2000);
    };
    
    shareModal.querySelector('#btn-close-share').onclick = () => {
        shareModal.classList.add('hidden');
        if (document.getElementById('modal-guide').classList.contains('hidden')) {
             document.getElementById('modal-overlay').classList.add('hidden');
        }
    };
}

// --- Geometry Smoothing (Chaikin's Algorithm) ---

function smoothGeoJSON(geojson) {
    if (!geojson || !geojson.features) return geojson;
    
    geojson.features.forEach(feature => {
        if (!feature.geometry) return;
        
        const type = feature.geometry.type;
        const coords = feature.geometry.coordinates;
        
        if (type === 'Polygon') {
            feature.geometry.coordinates = coords.map(ring => smoothRing(ring));
        } else if (type === 'MultiPolygon') {
            feature.geometry.coordinates = coords.map(poly => poly.map(ring => smoothRing(ring)));
        }
    });
    
    return geojson;
}

function smoothRing(ring) {
    // Chaikin's algorithm for closed paths (iterations=3 for a natural, softened look)
    let currentRing = ring;
    for (let i = 0; i < 3; i++) {
        const nextRing = [];
        const len = currentRing.length;
        if (len < 3) return currentRing; // Cannot smooth line with < 3 points

        // We assume the ring is closed (first point == last point)
        // Process segments
        for (let j = 0; j < len - 1; j++) {
            const p0 = currentRing[j];
            const p1 = currentRing[j + 1];
            
            // Q = 0.75*P0 + 0.25*P1
            const q = [
                0.75 * p0[0] + 0.25 * p1[0],
                0.75 * p0[1] + 0.25 * p1[1]
            ];
            
            // R = 0.25*P0 + 0.75*P1
            const r = [
                0.25 * p0[0] + 0.75 * p1[0],
                0.25 * p0[1] + 0.75 * p1[1]
            ];
            
            nextRing.push(q);
            nextRing.push(r);
        }
        
        // Close the ring
        nextRing.push(nextRing[0]);
        currentRing = nextRing;
    }
    return currentRing;
}
