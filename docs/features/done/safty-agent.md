This implementation document is designed for an AI-powered coding assistant to execute the "Lookout" Safety Auditor feature for **navalplan**. It covers the agent's definition, database schema changes, backend service logic, and the frontend UI component.

---

# Implementation Plan: "Lookout" Safety Auditor

## 1. Agent Definition (Researcher Service)
**File Path:** `code/services/researcher/prompts/lookout.md`

Create the system prompt for the new Lookout agent. Its goal is to analyze stop data and return structured safety alerts.

```markdown
You are the **Lookout**, a maritime safety auditor. Your task is to analyze voyage data for a single stop and identify potential "red flags."

### Inputs to Analyze:
- **Date & Location**: Identify seasonal risks (e.g., hurricane season).
- **Weather**: Analyze wind speeds, wave heights, and conditions.
- **Sun Phases**: Evaluate arrival/departure times against sunrise/sunset.
- **Tides**: Check for extreme ranges (>10ft/3m) or low water hazards.
- **Distance**: Estimate travel time at 5kts and check for night arrivals.

### Safety Rules:
- **Danger**: Wind > 33kts, Waves > 4m (13ft), or predicted arrival > 1 hour after Sunset.
- **Warning**: Wind > 18kts, Waves > 2m (7ft), or Tidal Range > 3m (10ft).
- **Info**: Minor seasonal notes or sunrise/sunset reminders.

### Output Format:
Return a JSON array of alert objects. Use Material Symbol names for icons.
```json
[
  {
    "severity": "danger | warning | info",
    "category": "weather | tides | navigation | sun",
    "message": "Direct explanation of the hazard.",
    "action": "Suggested precaution.",
    "icon": "storm | tsunami | explore | light_mode"
  }
]
```

## 2. Database & Models
**File Path:** `code/app/db/migrations/000016_add_safety_alerts.up.sql`
```sql
ALTER TABLE briefing ADD COLUMN safety_alerts JSONB;
```

**File Path:** `code/app/backend/models/models.go`
Update the `Briefing` struct to include the new field.
```go
type Briefing struct {
    // ... existing fields
    SafetyAlerts RawJSON `json:"safety_alerts" db:"safety_alerts"`
}
```

## 3. Backend Service Logic
**File Path:** `code/app/backend/server/handlers/admin.go` (or a new `lookout.go`)

Implement the daily audit logic that scans voyages occurring in the next 14 days.

```go
func (h *Handler) RunLookoutAudit(ctx context.Context) error {
    // 1. Fetch stops occurring between [NOW] and [NOW + 14 Days]
    // 2. For each stop:
    //    a. Fetch associated Briefing (Weather, Tides, Sun).
    //    b. Calculate distance from previous stop (using geometry.go).
    //    c. Payload = { weather: b.Weather, tides: b.Tides, sun: b.Sun, distance: dist }
    //    d. Call AgentRunner.Run(ctx, "lookout", payload)
    //    e. DB: Upsert SafetyAlerts into the briefing table.
    return nil
}
```

## 4. Automation Setup (GCP)
To run this daily, expose an administrative endpoint.

**File Path:** `code/app/backend/server/routes.go`
```go
{http.MethodPost, "/api/admin/lookout/audit", s.Handler.RunLookoutAuditEndpoint, 2}, // AuthLevel 2 (Admin)
```

**Deployment Note:**
Configure a **Cloud Scheduler** job to trigger this endpoint once every 24 hours.

## 5. Frontend UI Component
**File Path:** `code/app/frontend/js/ui/LookoutBox.js`

This component uses **Material Symbols Outlined** and renders in the center of the report.

```javascript
export class LookoutBox {
    constructor(alerts) {
        this.alerts = alerts || [];
    }

    render() {
        if (this.alerts.length === 0) return '';

        return `
            <section class="lookout-box" aria-labelledby="lookout-title">
                <div class="lookout-header">
                    <span class="material-symbols-outlined">visibility</span>
                    <h2 id="lookout-title">Safety Lookout</h2>
                </div>
                <div class="alert-list">
                    ${this.alerts.map(alert => `
                        <div class="alert-item alert-${alert.severity}">
                            <span class="material-symbols-outlined alert-icon">
                                ${alert.icon || 'warning'}
                            </span>
                            <div class="alert-text">
                                <span class="alert-msg">${alert.message}</span>
                                <span class="alert-action">${alert.action}</span>
                            </div>
                        </div>
                    `).join('')}
                </div>
            </section>
        `;
    }
}
```

## 6. Styles
**File Path:** `code/app/frontend/css/components.css`

```css
.lookout-box {
    margin: 2rem auto;
    max-width: 800px;
    padding: 1.5rem;
    background: var(--surface-2);
    border-radius: var(--radius-md);
    border: 1px solid var(--outline-variant);
}

.lookout-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 1rem;
    color: var(--primary);
}

.alert-item {
    display: flex;
    gap: 1rem;
    padding: 1rem;
    margin-bottom: 0.75rem;
    border-radius: var(--radius-sm);
    border-left: 5px solid transparent;
}

.alert-danger { background: var(--error-container); color: var(--on-error-container); border-left-color: var(--error); }
.alert-warning { background: #fff8e1; color: #856404; border-left-color: #ffc107; }
.alert-info { background: var(--secondary-container); color: var(--on-secondary-container); border-left-color: var(--secondary); }

.alert-msg { font-weight: 700; display: block; }
.alert-action { font-size: 0.9rem; }
```

---

**Next Steps for Gemini CLI:**
1. Generate the `lookout.md` prompt.
2. Apply the SQL migration.
3. Update `models.go` and `routes.go`.
4. Create the `LookoutBox.js` component and import it into the stop report view logic.