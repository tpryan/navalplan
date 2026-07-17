# Engineering Specification & Implementation Plan: Day Trips

## Objective

Today NavalPlan's UX implicitly nudges every voyage toward spanning multiple
days (auto-advancing the end date, rendering a multi-day "Day 1 / Day 2 / ..."
itinerary timeline, showing date ranges everywhere). Add first-class support
for **day trips** — voyages that start and end on the same calendar date, with
a single stop — via a dedicated, simplified planning mode, without disturbing
the existing multi-day voyage flow.

**Decisions locked in for this spec** (confirmed with product owner):

* A day trip has exactly **one stop**. No schema change to relax the
  `UNIQUE(voyage_id, target_date)` constraint on `stop` — that's out of scope.
* Day trips get a **dedicated UI mode** (explicit entry point + simplified
  single-day view), not just a bare-minimum "allow equal dates" tweak.

## Current State (from investigation)

The data layer is already more permissive than the UI lets on:

* `models.Voyage` (`code/app/backend/models/models.go`) has `StartDate` and
  `EndDate` as `*time.Time` — already optional, no struct-level validation.
* Migration `000001_initial_schema.up.sql` has no CHECK constraint requiring
  `end_date > start_date`. Migration `000011_optional_voyage_dates.up.sql`
  already dropped `NOT NULL` from both columns.
* `CreateVoyage`/`UpdateVoyage` handlers (`server/handlers/voyages.go`)
  decode dates straight from JSON with no range validation — `start == end`
  already round-trips successfully today.
* `ExtendVoyage` (`voyages.go:373`) only rejects `newEnd.Before(start)` —
  equal dates pass.
* Frontend day-math is inclusive everywhere it matters
  (`main.js:2549-2553`, `1818-1821`, `5604-5605`), so `start === end` already
  evaluates to exactly one day / one iteration without special-casing.

So the backend needs **no changes** for the core "allow same-day voyages"
mechanic. The work is entirely: (1) an explicit UI entry point and mode,
(2) fixing UI copy/labels that read oddly for a single-day range, and
(3) tuning the researcher agent's destination-discovery bias away from
"overnight anchorage" queries when there's no overnight involved.

---

## 1. Frontend: Entry Point & Modal Changes (`code/app/frontend/`)

### 1.1 New Voyage modal (`index.html`, `js/main.js`)

* Add a mode toggle to `#modal-new-voyage` (`index.html:161-199`), e.g. two
  pill/tab buttons "Multi-Day Voyage" / "Day Trip" above the title field.
  Default to "Multi-Day Voyage" to preserve existing behavior.
* When "Day Trip" is selected:
  * Reveal `#voyage-date-fields` immediately (skip the "Discovery First"
    hidden-dates behavior at `main.js:742`) with a single date picker
    labeled "Trip Date" instead of separate Start/End fields. On submit,
    set both `start_date` and `end_date` to that same value.
  * Set `document.getElementById('modal-voyage-pill').textContent` to
    `'DAY TRIP'` instead of `'NEW VOYAGE'` (mirrors `main.js:738`).
  * Skip the auto-advance-end-date listener (`main.js:771-777`) entirely
    for this mode — it currently pushes `end = start + 1 day`, which is
    exactly the behavior a day trip must avoid.
  * Consider defaulting the date input to today (`new Date().toISOString()`)
    since day trips are more likely to be planned close to the date.
* `Edit Voyage` modal (`openEditModal`, `main.js:1400`) should detect
  `start_date === end_date` on an existing voyage and open in "Day Trip"
  mode (single date field, `DAY TRIP` pill) rather than the two-field range
  view, so editing stays consistent with how it was created.
* Form submit handler (`main.js:812-827`): no payload shape change needed —
  just ensure both fields get the same value when in Day Trip mode.

### 1.2 Voyage list / cards & report header

* `dateDisplay` construction (`main.js:5594-5599`, and the similar blocks at
  `1269-1288` and `1466-1473`) currently always renders `${start} – ${end}`.
  Add a same-day branch: if `start_date === end_date`, render a single
  formatted date (e.g. `"Jul 16, 2026"`) instead of a dash-range.
* Stat tile labeled "Days" (`main.js:5641-5644`, value from `dayCount` at
  `5604-5605`) will correctly compute `1` for a day trip, but the tile is
  redundant/confusing for a 1-stop outing. For day trips, consider swapping
  this tile for something more useful (e.g. departure time or duration in
  hours if that data exists) or simply omit the "Days" tile when
  `dayCount === 1`.

### 1.3 Itinerary / workspace view (`renderItinerary`, `main.js:2404-2570`)

* The day loop already produces exactly one "Day 1" card for a day trip —
  no arithmetic changes needed. For the dedicated mode, replace the
  "Day 1" framing with a simpler single-stop layout:
  * Drop the day-number header/chip (`dayNum`/date chip at `2558-2559`)
    since there's only ever one.
  * Rename section heading from "Itinerary" to "Trip Details" (or similar)
    when `currentVoyage.start_date === currentVoyage.end_date`.
* `checkItineraryFullness()` (`main.js:1800-1830`): `diffDays` evaluates to
  `1` for a day trip, and `isFull = allStops.length >= diffDays` already
  becomes "full" after the single stop is added — this is correct
  out-of-the-box, no change required.

### 1.4 Weather/tide presentation for the single stop

* Since a day trip has one stop and one date, default to the **hourly**
  forecast/tide window instead of the daily summary — the user cares about
  hour-by-hour conditions for departure/return timing on that single day.
  Locate the existing hourly-track rendering used in stop briefings and
  make it the default-expanded view when in day-trip mode; this is a
  display-only change, no new data source needed.

### 1.5 Voyage report / print view

* `dateDisplay` and the "Days" stat tile in the report generator
  (`main.js:5594-5644`) need the same same-day formatting fix as §1.2.
* Spot-check the print/PDF voyage report template for any other hardcoded
  "Day 1 of N" or range-based headers and apply the same single-date
  formatting.

---

## 2. Backend (`code/app/backend/`)

No functional changes required — dates are already optional and unvalidated
for range. Two small polish items:

* `ExtendVoyage` (`server/handlers/voyages.go:309-398`) and
  `InterpolatePassagePoints` (`server/handlers/passages.go:37-107`) are
  already day-trip-safe: passage-point interpolation requires 2+ landfalls
  and `total > 1` day (`passages.go:61-64`), so it's a no-op for a 1-stop,
  1-day voyage. Add an explicit unit test asserting this stays a no-op
  (see §5).
* Optional: add a lightweight `IsDayTrip()` helper on `models.Voyage`
  (`StartDate != nil && EndDate != nil && StartDate.Equal(*EndDate)`) if any
  backend-side branching ends up needed (e.g. for the researcher-agent
  prompt tuning in §3). Prefer deriving this rather than adding a stored
  column/flag, to avoid a second source of truth that could drift from the
  actual dates.

---

## 3. Researcher Agent (`code/services/researcher/`)

The agent tooling is already single-date shaped (`harbourmaster.md` calls
weather/tides/sunrise for one location + one date), so no structural change
is needed there. One prompt-bias adjustment:

* `prompts/specialist.md:20` currently biases destination discovery toward
  `"... overnight anchorage [Location/Area]"` queries (70-80% of results
  per the distribution priority at lines 34-38). For a day trip there's no
  overnight stay, so this skews results toward irrelevant options (moorings
  meant for multi-night stays vs. day-use anchorages, lunch stops,
  fuel/provisioning near a day's sailing radius).
  * When the backend invokes the researcher for a day-trip stop, pass a
    flag/context field (e.g. `trip_type: "day_trip"`) through the A2A
    request payload, and branch the specialist query template to search for
    `"day anchorage"`, `"lunch stop"`, `"day mooring"` instead of
    `"overnight anchorage"` when that flag is set.
* `prompts/lookout.md` ("position in voyage", travel vs. non-travel days,
  seasonal patterns "only on stop 1 of N") is already N-agnostic and
  tolerates N=1 via its existing "last stop has no distance field" handling
  — no change needed, but add an eval fixture (see §5) to confirm.

---

## 4. Terminology note

Frontend copy already says "Trip Title" in the New Voyage form
(`index.html:164`) despite the domain model being called "Voyage"
everywhere else. Keep "Day Trip" as the user-facing label (matches the
request) while continuing to use `Voyage`/`voyage_id` internally — no
renaming of models, tables, or API routes.

---

## 5. Testing

* **Backend**: Go unit test asserting `CreateVoyage`/`UpdateVoyage` accept
  `start_date == end_date`, and that `InterpolatePassagePoints` is a no-op
  for a single-landfall, same-day voyage.
* **Frontend** (Jasmine): test the same-day `dateDisplay` formatting branch,
  and that `checkItineraryFullness` reports "full" after one stop when
  `start_date === end_date`.
* **Researcher eval**: add a `day_trip` fixture under
  `code/services/researcher/eval/specialist/` exercising the new query bias,
  and one under `eval/lookout/` (currently no eval directory exists for
  `lookout` — create one) covering a single-stop voyage to confirm the
  "position in voyage" / seasonal-pattern logic still behaves with N=1.
* **Manual**: create a day trip end-to-end through the new modal mode, run
  full research, and check the voyage report/print view renders sane
  single-date copy throughout.

---

## Execution Workflow for Claude

1. Add the Day Trip / Multi-Day toggle and single-date field to
   `index.html` + `main.js` modal listeners (§1.1).
2. Fix same-day `dateDisplay` formatting and the "Days" stat tile across
   voyage cards, workspace header, and report (§1.2, §1.5).
3. Simplify the itinerary render path for the single-stop case (§1.3) and
   default to the hourly forecast view (§1.4).
4. Add the `trip_type`/day-trip context flag to the backend→researcher A2A
   call and branch `specialist.md`'s query template (§3).
5. Write backend unit tests, frontend Jasmine tests, and researcher eval
   fixtures (§5).
6. Manually verify end-to-end, then move this doc to `docs/features/done/`.
