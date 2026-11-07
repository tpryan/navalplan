/**
 * Maps arbitrary or LLM-suggested icon names into valid Material Symbols Outlined icon ligatures.
 * @param {string} icon
 * @returns {string} valid Material Symbol icon name
 */
export function normalizeLookoutIcon(icon) {
  if (!icon) return 'warning';
  const clean = String(icon).trim().toLowerCase().replace(/[- ]/g, '_');
  const iconMap = {
    // Bridges, clearances, overhead
    'bridge': 'height',
    'bridges': 'height',
    'clearance': 'height',
    'vertical_clearance': 'height',
    'overhead': 'height',
    'mast': 'height',
    'lock': 'lock',
    'canal_lock': 'lock',

    // Marine & Navigation
    'boat': 'directions_boat',
    'vessel': 'directions_boat',
    'ship': 'directions_boat',
    'directions_boat': 'directions_boat',
    'sailing': 'sailing',
    'sail': 'sailing',
    'anchor': 'anchor',
    'anchorage': 'anchor',
    'compass': 'explore',
    'explore': 'explore',
    'navigation': 'explore',
    'route': 'alt_route',
    'alt_route': 'alt_route',
    'passage': 'alt_route',
    'transit': 'alt_route',
    'straighten': 'straighten',
    'speed': 'speed',
    'speed_limit': 'speed',

    // Water, currents, tides
    'water': 'water',
    'current': 'water',
    'currents': 'water',
    'cross_current': 'water',
    'tide': 'waves',
    'tides': 'waves',
    'rip': 'waves',
    'rips': 'waves',
    'tidal_rip': 'waves',
    'waves': 'waves',
    'rough_seas': 'waves',
    'tsunami': 'tsunami',
    'shoal': 'warning',
    'shallow': 'warning',
    'depth': 'straighten',
    'channel': 'straighten',
    'rock': 'warning',
    'reef': 'warning',

    // Weather & Wind
    'wind': 'air',
    'gust': 'air',
    'gale': 'air',
    'air': 'air',
    'storm': 'storm',
    'thunderstorm': 'thunderstorm',
    'lightning': 'bolt',
    'rain': 'rainy',
    'rainy': 'rainy',
    'snow': 'weather_snowy',
    'fog': 'foggy',
    'foggy': 'foggy',
    'thermostat': 'thermostat',
    'temperature': 'thermostat',

    // Sun & Timing
    'sun': 'light_mode',
    'light_mode': 'light_mode',
    'daylight': 'light_mode',
    'sunset': 'wb_twilight',
    'sunrise': 'wb_twilight',
    'twilight': 'wb_twilight',
    'wb_twilight': 'wb_twilight',
    'time': 'schedule',
    'schedule': 'schedule',
    'clock': 'schedule',

    // Trends & Warnings
    'trending_up': 'trending_up',
    'trending_down': 'trending_down',
    'warning': 'warning',
    'danger': 'dangerous',
    'dangerous': 'dangerous',
    'alert': 'warning',
    'caution': 'warning',
    'info': 'info',
    'notice': 'info',
    'visibility': 'visibility',
  };

  return iconMap[clean] || clean;
}

const LNM_STOP_WORDS = new Set([
  'the', 'and', 'for', 'with', 'from', 'through', 'into', 'under', 'are', 'all',
  'lnm', 'uscg', 'local', 'notice', 'mariners', 'verify', 'current', 'status',
  'latest', 'before', 'transiting', 'reported', 'operations', 'san', 'francisco',
  'bay', 'california', 'united', 'states', 'aton', 'discrepancies',
]);

function extractAlertTokens(text) {
  const words = String(text || '').toLowerCase().split(/[^a-z0-9]+/);
  const seen = new Set();
  const out = [];
  for (const w of words) {
    if (!w) continue;
    const hasDigit = /\d/.test(w);
    if ((!hasDigit && w.length < 4) || LNM_STOP_WORDS.has(w) || seen.has(w)) continue;
    seen.add(w);
    out.push(w);
  }
  return out;
}

function alertTokensOverlap(tokensA, tokensB) {
  if (!tokensA.length || !tokensB.length) return false;
  const setB = new Set(tokensB);
  let shared = 0;
  for (const t of tokensA) {
    if (setB.has(t)) shared++;
  }
  if (shared >= 4) return true;
  const minLen = Math.min(tokensA.length, tokensB.length);
  return minLen > 0 && shared / minLen >= 0.5;
}

/**
 * Deduplicates Safety Lookout alerts (collapsing paraphrased/raw LNM duplicates).
 * @param {Array} alerts
 * @returns {Array}
 */
export function deduplicateLookoutAlerts(alerts) {
  if (!Array.isArray(alerts) || alerts.length <= 1) return alerts || [];
  const out = [];
  const outTokens = [];
  for (const a of alerts) {
    if (!a) continue;
    const msg = String(a.message || '').trim();
    const act = String(a.action || '').trim();
    const lowerMsg = msg.toLowerCase();
    const isLNM = lowerMsg.startsWith('lnm:') || lowerMsg.includes('local notice to mariners');
    const tokens = extractAlertTokens(`${msg} ${act}`);
    let dupIdx = -1;
    for (let i = 0; i < out.length; i++) {
      const exMsg = String(out[i].message || '').trim().toLowerCase();
      if (exMsg === lowerMsg) {
        dupIdx = i;
        break;
      }
      const exIsLNM = exMsg.startsWith('lnm:') || exMsg.includes('local notice to mariners');
      if (isLNM && exIsLNM && alertTokensOverlap(tokens, outTokens[i])) {
        dupIdx = i;
        break;
      }
    }
    if (dupIdx >= 0) {
      const exAct = String(out[dupIdx].action || '').toLowerCase();
      if (
        exAct.includes('verify current status in the latest uscg local notice') &&
        !act.toLowerCase().includes('verify current status in the latest uscg local notice')
      ) {
        out[dupIdx] = a;
        outTokens[dupIdx] = tokens;
      }
      continue;
    }
    out.push(a);
    outTokens.push(tokens);
  }
  return out;
}

/**
 * LookoutBox — renders a list of maritime safety alerts from the Lookout agent.
 * @param {Array} alerts - array of {severity, category, message, action, icon}
 * @returns {HTMLElement|null} section element, or null if no alerts
 */
export function LookoutBox(alerts) {
  const deduped = deduplicateLookoutAlerts(alerts);
  if (!deduped || deduped.length === 0) return null;

  const section = document.createElement('section');
  section.className = 'lookout-box';
  section.setAttribute('aria-labelledby', 'lookout-title');

  const header = document.createElement('div');
  header.className = 'lookout-header';
  header.innerHTML = `<span class="material-symbols-outlined" aria-hidden="true">visibility</span>
    <h3 id="lookout-title">Safety Lookout</h3>`;
  section.appendChild(header);

  const list = document.createElement('div');
  list.className = 'lookout-alert-list';

  deduped.forEach(alert => {
    const item = document.createElement('div');
    const hasTravelTable = Array.isArray(alert.travel_table) && alert.travel_table.length > 0;
    item.className = `lookout-alert ${hasTravelTable ? 'lookout-alert--travel' : `lookout-alert--${alert.severity || 'info'}`}`;

    const icon = document.createElement('span');
    icon.className = 'material-symbols-outlined lookout-alert__icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.textContent = normalizeLookoutIcon(alert.icon);

    const text = document.createElement('div');
    text.className = 'lookout-alert__text';

    const msg = document.createElement('span');
    msg.className = 'lookout-alert__msg';
    msg.textContent = alert.message || '';
    text.appendChild(msg);

    if (Array.isArray(alert.travel_table) && alert.travel_table.length > 0) {
      const tbl = document.createElement('table');
      tbl.className = 'lookout-travel-table';
      const hasDeparture = alert.travel_table.some(r => r.depart_by);
      const head = tbl.createTHead();
      const hrow = head.insertRow();
      ['Speed', 'Travel time', ...(hasDeparture ? ['Depart by'] : [])].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        hrow.appendChild(th);
      });
      const body = tbl.createTBody();
      alert.travel_table.forEach(row => {
        const tr = body.insertRow();
        [
          `${row.speed_kt} kt`,
          row.travel_time,
          ...(hasDeparture ? [row.depart_by || '—'] : []),
        ].forEach(val => {
          const td = tr.insertCell();
          td.textContent = val;
        });
      });
      text.appendChild(tbl);
    }

    if (alert.action) {
      const action = document.createElement('span');
      action.className = 'lookout-alert__action';
      action.textContent = alert.action;
      text.appendChild(action);
    }

    item.appendChild(icon);
    item.appendChild(text);
    list.appendChild(item);
  });

  section.appendChild(list);
  return section;
}
