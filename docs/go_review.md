# NavalPlan — Go code review

Scope: all non-test Go in `code/app/backend`, `code/services/researcher`, `code/jobs/nautical-sync`
(~70 files, 3 modules). Read-only; nothing in the repository was modified.

Path prefixes used below:
`be/` = `code/app/backend/internal/` · `rs/` = `code/services/researcher/internal/` ·
`sync/` = `code/jobs/nautical-sync/`

Every claim below was confirmed by opening the file. Claims from the seven per-package
worker reports that did not survive that check were dropped; where two workers disagreed,
the resolution is stated inline.

---

## 1. Verdict

This is a healthy codebase. Mechanical hygiene is genuinely good: `go vet` is clean on all
three modules, `gofmt` is clean, imports are grouped stdlib/module/third-party throughout,
and there is not a single double blank line in any file in scope — the AGENTS.md style rules
are actually being followed, which is rarer than it sounds. The package layout is sound and
occasionally exemplary (`be/safety/validator.go:1-46` names every threshold and documents why
the logic was moved out of the LLM; it is the standard the rest of the repo should copy).

The real problems are concentrated rather than diffuse. `be/server/handlers/` has silted up:
it holds the domain logic for track pairing, debrief synthesis, region dedup and passage
interpolation, plus three Google API clients, alongside its HTTP code — and that is where the
180-to-225-line functions live. The second cluster is dropped errors: roughly a dozen sites
where a write failure, a parse failure or an auth failure is logged-and-continued or discarded
entirely, several of which report success to the user afterwards. Both are tractable with
local edits; neither is architectural.

Nothing here needs a rewrite. The highest-value work is an afternoon of error-handling fixes,
deleting ~500 lines of confirmed-dead code, and one `authorizedVoyage` helper that would remove
roughly 300 lines of copy-pasted auth preamble.

---

## 2. Bugs

Genuine defects, not style. Ordered by severity.

### B1 — Unchecked array indexing panics the weather handler
`be/server/handlers/weather.go:141-144`

`weather.Daily.Temperature2mMin[0]`, `Temperature2mMax[0]`, `WindSpeed10mMax[0]` and
`WindDirection10mDominant[0]` are indexed after only `WeatherCode` was length-checked
(`:76`, `:120`). An upstream response carrying `weather_code` but a short or absent
temperature array panics. `fetchWeatherForStop` is called in a loop from
`ScheduledMaintenance` (`be/server/handlers/scheduled.go:44`), so one bad response from
Open-Meteo takes down the whole cron run, not one stop. The inconsistency is inside one
function: the marine block 35 lines later length-checks every array it touches
(`weather.go:178`, `:180`, `:183`, `:187`).

**Fix:** guard the four reads the way `:178-192` already does, or compute
`n := len(weather.Daily.Time)` once and bail if it is zero.

### B2 — Guide research reports success after the save failed
`be/server/handlers/guide.go:268-272`

```go
if err := h.DB.CreateVoyageGuide(ctx, guide); err != nil {
        slog.ErrorContext(ctx, "Failed to save voyage guide", "error", err)
}
// falls through
slog.InfoContext(..., "Voyage guide saved for voyage %d", ...)
h.broadcastProgress(sessionID, "done", "Voyage guide research complete")
```

There is no `return` after the log. The browser is told the guide was saved and the job is
complete; nothing was written.

**Fix:** `return` after the log, or route to `failGuideResearch`.

### B3 — Every MCP tool call is made with a literal `nil` context
`rs/mcp/mcp.go:191, 197, 203, 209, 215, 221, 227`

`handleToolCall(ctx context.Context, ...)` (`:176`) accepts a context — correctly threaded
in from `ServeHTTP` at `:40` and `:155` — and then never uses it. All seven dispatch arms
pass `nil`. Consequences: no deadline, no cancellation (a client disconnect leaves the
upstream NOAA/Places/RAG fetch running), and no trace propagation.

Calibration: this does **not** crash. `stop_anchor.go:21` and `:53` both begin with an
explicit `if ctx == nil` guard, so the session-state overrides (stop coordinate anchoring,
area search radius) silently no-op for every MCP call instead of panicking — a behavioural
divergence between the MCP and ADK paths that nothing documents.

**Fix:** pass `ctx` at all seven sites. Delete `Handler.Ctx` (`:28`) while you are there — see D7.

### B4 — RAG poll loop can never terminate
`sync/main.go:280-328`

`waitForRagOperation` loops forever with no deadline of its own. Both escape hatches are
unreachable in practice: `ctx` traces back to `context.Background()` (`sync/main.go:57`) so
`ctx.Done()` at `:284` never fires, and a malformed status body hits
`slog.Warn` + `continue` (`:321-322`), spinning at one request per 10 s indefinitely. A stuck
Vertex operation hangs the Cloud Run job silently.

**Fix:** `context.WithTimeout` around the import phase plus a consecutive-parse-failure cap.

### B5 — A tide parse failure is emitted as a real 0.0 ft height
`rs/tool/tides.go:171-177`

```go
val, _ := pt.ValueFloat()
events = append(events, TideEvent{Time: pt.Time, Type: pt.Type, Height: val, Unit: "ft"})
```

A parse failure yields `0.0`, which is then handed to the agent as a measured tide height,
indistinguishable from a genuine reading. This is a navigational value being fabricated by an
ignored error.

**Fix:** skip the event and log, or propagate the error.

### B6 — Sticky stream-error flag silently truncates agent responses
`be/agent/reasoning_engine.go:234-251`

An `event: error` line sets `streamErr` at `:235`. From that point every subsequent `data:`
line hits the `if streamErr != nil` branch at `:248`, overwrites the error with its own
payload and `continue`s — so all remaining content is discarded and never reaches `sb`. The
flag is never cleared. A mid-stream error that the stream recovers from costs the entire
remainder of the response.

**Fix:** clear the flag when a `data:` line parses, or abort the loop on first error instead
of limping on.

### B7 — Auth failure falls through as an unauthenticated request
`be/agent/agent.go:274-286`

`addAuthHeader` returns `void`. On token-source failure (`:276-279`) or token failure
(`:280-283`) it logs and returns; the `Authorization` header is never set and the callers at
`:108`/`:178` cannot tell. The downstream Cloud Run service then rejects with 401/403, which
surfaces as an opaque "agent returned 403" — the real cause is two log lines away in a
different function.

**Fix:** return `error` and abort the call.

### B8 — Rate limiting does not enforce the limits it declares
`be/server/middleware.go:22, 28-34`

The bucket key is `user:<id>` or `ip:<RemoteAddr>` with **no route component**, while
`rateLimit(limit, window)` is applied per route with different limits: 5/min at
`be/server/routes.go:79, 80, 84, 99, 100` and 10/min at `:96, 98`. All seven routes therefore
share one counter per user, compared against whichever route's `limit` happens to be checked.
A user who spends 5 calls on `/voyages/{id}/research` is rejected from `/stops/{id}/research`
even though that route advertises 10.

**Fix:** include the route pattern in the key.

### B9 — Rate-limit map grows without bound and barely limits by IP
`be/server/middleware.go:21-23, 33`

`rateLimits` is a package-level `sync.Map` with no reaper; every key is retained for the
process lifetime. Because `r.RemoteAddr` includes the **ephemeral port**, each new connection
from an unauthenticated client mints a fresh key — so this is simultaneously an unbounded
memory leak and the reason IP-based limiting is close to ineffective.

**Fix:** reap on the window field; reduce `RemoteAddr` to host, or read `X-Forwarded-For`
behind Cloud Run.

### B10 — Global discovery mutation is reachable by any authenticated user
`be/server/routes.go:129-130`; handlers at `be/server/handlers/discovery.go:81`, `:210`

`DiscoveryMining` and `DiscoveryPruning` are registered at `AuthLevel 1` and perform no
in-handler authorization check. `DiscoveryMining` with `?month=all` spawns a 4-hour background
job (`discovery.go:90`) that rewrites the **global** discovery region set shared by all users.
The narrower `DeleteDiscoveryRegionSeasonality` is stricter than both — it enforces
`person.IsAdmin` at `discovery.go:52`.

Related: `routes.go:131` registers an `/api/admin/discovery/...` path at `AuthLevel 1`, and it
is a byte-identical duplicate of the `/api/v1/...` route on `:132` (same handler, same level).

**Fix:** `AuthLevel 2` on `:129-130`, delete `:131`.

### B11 — Progress channel is reaped before the work it reports on can finish
`be/server/handlers/guide.go:173` vs `:189`

The progress channel TTL is `25*time.Minute`; the agent context it covers allows
`30*time.Minute`. A run taking 26-30 minutes completes successfully and then broadcasts `done`
into a channel that has already been collected, so the browser hangs forever. The sibling
subsystem has the same pair the safe way round (25 min TTL vs a 20 min context at
`be/server/handlers/recommendation.go:297`), which is exactly why the invariant needs to be
stated rather than rediscovered.

**Fix:** TTL must exceed the work timeout; derive one from the other.

### B12 — `UpdateStop` performs none of `CreateStop`'s validation
`be/server/handlers/stops.go:164-176` vs `:90-121`

`CreateStop` validates stop type (`:90-97`), latitude range (`:99-102`), longitude range
(`:103-106`), radius sign (`:107-110`) and radius unit (`:114-121`). `UpdateStop` decodes
straight into a `model.Stop` at `:164-168`, sets only `ID` and `VoyageID`, and writes. A
client can `PUT latitude: 999` or `stop_type: "banana"`, and any field omitted from the body
is zeroed.

**Fix:** extract `validateStop(*model.Stop) error` from `:90-121`, call it from both.

### B13 — Four writes whose failure is discarded entirely
- `be/server/handlers/research.go:667` — `h.DB.CreateBriefing(ctx, briefing)`, return value
  not even assigned to `_`. This is `saveEmptyBriefing`, the *error-recovery* path, so a
  failure leaves the stop with no briefing and no record of why.
- `be/server/handlers/guide.go:300` — `h.DB.CreateVoyageGuide(ctx, guide)` in `saveEmptyGuide`,
  same shape, same consequence.
- `be/server/handlers/tracks.go:365-366` and `:428-429` — `debriefBytes, _ := json.Marshal(...)`
  then `_ = h.DB.UpdateVoyageTrackDebrief(...)`. The debrief is returned to the client at
  `:369` as if it had been persisted.

**Fix:** check and log at minimum; for `tracks.go` the handler should not claim persistence
it did not achieve.

### B14 — `err` tested on the wrong statement
`be/server/handlers/guide.go:383-389`

```go
guide, err := h.DB.GetVoyageGuide(r.Context(), voyage.ID)   // :383 — err never tested
stops, err := h.DB.ListStops(r.Context(), voyage.ID, 0, 0)  // :385 — reassigns err
if err != nil { ... }
```

A guide-fetch failure is invisible; `guide` is then emitted as `null` at `:439`, so the public
report silently renders as "this voyage has no guide" when the database call actually failed.

### B15 — Semaphore acquired without `defer`
`be/server/handlers/scheduled.go:69-71`

```go
h.ResearchSem <- struct{}{}
h.performLookoutAuditLogic(&stop, briefing, allVoyageStops, sessionID)
<-h.ResearchSem
```

A panic inside the audit permanently leaks a slot from a semaphore of capacity 10
(`be/server/handlers/handlers.go:192`); enough cron runs and the whole research subsystem
deadlocks. `guide.go:183-184` does this correctly with `defer` — match it.

### B16 — Stop ID logged under `voyage_id`
`be/server/handlers/scheduled.go:62`

`slog.ErrorContext(ctx, "...Failed to list voyage stops", "voyage_id", stop.ID, ...)` — the
value is a **stop** ID; `stop.VoyageID` is what was queried on the line above (`:60`).
Actively misleading during triage.

### Claims checked and NOT carried forward

- **`rs/server/reasoning_engine.go:185-212` "nil-pointer panic".** Worker item 7 rated this
  Critical. `f, _ := w.(http.Flusher)` does discard `ok` and `f.Flush()` is called
  unconditionally at `:190` and `:212` — but the writer in the live chain is
  `rs/server/middleware.go:19`, which **does** implement `Flush()` (`:29-33`). The assertion
  therefore succeeds today and this is a latent unchecked assertion, not a live crash.
  Worth fixing (`telemetry.go:225-229` in the same module shows the correct `ok` check and a
  500 "Streaming unsupported"), but it belongs under findings, not bugs.
- **`be/gpx/parser.go:22-24` "swallowed error with silent correctness consequence."** Worker
  item 5 rated this Critical. `FilterGPX` has exactly two error paths. The unmarshal failure
  at `be/gpx/filter.go:60-62` returns the original bytes, so `ParseGPX`'s own `xml.Unmarshal`
  at `:27` fails immediately afterwards and the user gets a proper error — no silent
  corruption. Only the marshal failure at `filter.go:108-110` could degrade silently, and it
  also returns a glitch count of `0` despite having found glitches, which contradicts the
  function's contract. `xml.MarshalIndent` on that struct is effectively infallible, so this
  is latent. Downgraded to Worth doing (F8).
- **`be/server/handlers/recommendation.go:105` "Earth radii disagree, results differ."** Two
  workers said this. They differ by 0.035 NM in 3440 — one part in 100,000, or 0.0002 NM over
  a 20 NM search radius. It is a consistency problem, not a correctness problem. See C-b.

---

## 3. Safe to delete

All confirmed by reference count over non-test Go, with the route table and the ADK interface
assertions checked explicitly.

| Symbol / region | file:line | Evidence it is unreachable |
|---|---|---|
| `(*NauticalToolService).FetchFilterGPXData` | `rs/tool/adapters.go:375-377` | Zero references in src and tests. Not in the slice `AsTools()` returns (`rs/tool/service.go:142`). Not in the MCP dispatch — `handleToolCall`'s switch ends at `rs/mcp/mcp.go:227` with no GPX case, and `tools/list` does not advertise one. |
| `gpx_filter.go` — the entire file (451 lines) | `rs/tool/gpx_filter.go:1-451` | Reachable only through `FetchFilterGPXData` above. `FilterGPXDataResult`, `FilterGPXRequest`, `FilterGPXResponse` and `TrackPointInput` have exactly two referrers between them: `adapters.go:375-376` and `gpx_filter_test.go`. Production GPX filtering is the backend's own `gpx.FilterGPX` (`be/gpx/filter.go:58`), live from `be/server/handlers/tracks.go:51`, `:171` and `be/gpx/parser.go:22`. Delete `gpx_filter_test.go` with it. |
| The comment claiming the above is live | `rs/tool/service.go:133-136` | First half is correct (GPX filtering is deliberately not an LLM tool); the last sentence — "retained for the MCP server and direct use" — asserts a consumer that does not exist. Verified against the full `mcp.go` switch. |
| `formatRagContextResponse` | `rs/tool/sailing_directions.go:464-471` | Zero references in src and tests. The live path decodes inline at `:300-310` and calls `formatRagChunks` directly at `:358`. Leftover from that refactor. |
| `(*Handler).getStaticMap` | `be/server/handlers/research.go:100-132` | Only caller in the module is `research_test.go:84`. Not in the route table; the one map route is `be/server/routes.go:89` → `GetVoyageMapImage`. Superseded by the frontend, which builds the Static Maps URL itself and uploads the blob. Delete `TestGetStaticMap_MissingAPIKey` with it. Removes a live Maps API key read from the handler layer. |
| `model.VoyageMap` (type + its 3 fields) | `be/model/models.go:138-143` | No reference to the type anywhere. `SaveVoyageMap`/`GetVoyageMap` take and return raw `[]byte` against hand-written SQL (`be/store/guide.go:50`, `:61`); the struct is never materialised. |
| `voyageStops` map | `be/server/handlers/scheduled.go:35-38` | Built from every stop, then never read — the loop at `:43` ranges `stops`, and `:60` re-queries per voyage. Confirmed over the whole 87-line file. |
| `isSpatialDuplicate` branch | `be/server/handlers/discovery.go:121-123`, used `:143` | Returns true on `iou>0.40 \|\| cont>0.70`. The very next check at `:146` is `iou>0.05 \|\| cont>0.15` and returns the identical tuple, so it **strictly subsumes** it; the `:143` branch can never be the deciding factor. It has a test (`discovery_test.go:139`) so it looks alive, but inside `isRegionDuplicate` it is inert. |
| Redundant guard | `be/server/handlers/recommendation.go:433` | `if v.Latitude != nil && v.Longitude != nil` can never be false: `:322-326` returns from the same function when either is nil. |
| Two dead conditions | `be/server/handlers/progress.go:82` | `evt.Stage == "error" \|\| evt.Stage == "error_503"` are both subsumed by `strings.HasPrefix(evt.Stage, "error")` in the same expression. |
| `stream bool` parameter | `be/agent/agent.go:288` | Never referenced in the body (`:289-302`). Sole call site passes `true` (`:173`). |
| `Handler.Ctx` field | `rs/mcp/mcp.go:28`, set `:34` | No reader anywhere; `ServeHTTP` correctly uses `r.Context()` at `:40`. Storing a context in a struct is the wrong pattern regardless. |
| `Config.AgentRegistryEnabled` | `be/config/config.go:43`, written `:194` | Populated from `NAVALPLAN_USE_AGENT_REGISTRY`; zero readers. |
| `stopMatch.distNM` | `be/gpx/split.go:26`, assigned `:53` | Write-only; the leg loop (`:65-83`) uses only `stopIndex`/`pointIndex`. |
| `itoa` | `rs/tool/safety_feed.go:152-172` | A 20-line hand-rolled reimplementation of `strconv.Itoa` with no comment explaining why the stdlib is avoided. |
| `sizeRatio` return value | `be/server/handlers/discovery.go:163`, `:191`, `:194` | Computed and returned; the single caller (`:142`) discards it with `_`. |
| Four never-called `Store` methods | `be/store/discovery.go:36`, `:78`, `:124`; `be/store/person.go:68` | `GetRegionDetails`, `DeleteSeasonalityForMonth`, `GetAllRegions`, `SetAdminStatus` — each is live code plus a `DBStore` interface entry plus two mock implementations, called by nothing. |
| `model.TrackKindRecorded` | `be/model/models.go:260` | Never used; only `TrackKindPlanned` is referenced (5 sites in `tracks.go`), and always as `string(...)`, so the `TrackKind` type is never used as a type either. |

**Do NOT delete** — two near-misses worth recording:
`(*BatchSearchTool).IsLongRunning` (`rs/tool/search.go:38`) is required by the ADK `tool.Tool`
interface and is *called* at runtime from `vendor/.../llminternal/base_flow.go:983`;
`(*BatchSearchTool).ProcessRequest` (`:64`) has zero direct references but is invoked through
a `toolinternal.RequestProcessor` type assertion at `base_flow.go:720`.

**Note on the dead GPX filter:** it contains a real defect, which is an argument for deleting
rather than fixing. `rs/tool/gpx_filter.go` ingests points from routes and waypoints as
fallbacks (`:169-214`), but the re-emit at `:363-397` rebuilds only `cleanDoc.Tracks` and its
point-matching predicate at `:374` requires `SourceDoc == "trk"`. For route- or
waypoint-sourced input the returned `FilteredGPX` still contains every glitch while `Summary`
(`:409`) states they were removed. The response contradicts itself for that entire input class.

---

## 4. Top 10 findings by payoff

Excludes everything already listed under Bugs.

| # | Finding | file:line | Fix |
|---|---|---|---|
| F1 | **The auth preamble is written ~25 times.** `GetPersonFromContext` → 401, `ParseInt(PathValue("id"))` → 400, `GetVoyage` → 404, `PersonID != person.ID` → 403. | `tracks.go:21-43, 131-157, 274-301, 320-346, 374-394`; `voyages.go:14-36, 51-73, 188-211, 217-239, 259-281, 294-316, 380-403, 449-471, 526-548`; `stops.go:13-35, 59-81, 135-162, 183-210`; `research.go:183-211, 484-517, 535-557`; `guide.go:37-52, 146-163, 316-331`; `lookout.go:20-48, 83-105`; `recommendation.go:138-154, 248-264` (all `be/server/handlers/`) | One `func (h *Handler) authorizedVoyage(w, r) (*model.Voyage, bool)` plus an `authorizedStop` sibling. Removes ~300 lines and makes the authorization rule auditable in one place. Highest leverage change in the repo. |
| F2 | **The SSE/channel subsystem exists twice and has already drifted.** `ensureRecChannel` (`recommendation.go:170-190`) is a line-for-line copy of `ensureProgressChannel` (`progress.go:13-33`); `broadcastRecommendation` (`:192-201`) copies `broadcastProgress` (`:35-44`); `StreamRecommendations` (`:203-241`) copies `StreamProgress` (`:46-87`). ~90 lines. Drift already present: buffer 30 vs 100, and `StreamProgress` terminates on a `done` event while `StreamRecommendations` only terminates on channel close. | `be/server/handlers/progress.go:13-87` vs `recommendation.go:170-241` | One generic `eventHub[T any]` with `ensure`/`broadcast`/`stream`. Do this before it drifts further — B11 is a symptom of the same split. |
| F3 | **Three 180-225-line functions carrying unrelated concerns.** `generateTrackDebrief` (199 lines: arithmetic, stop-title derivation, LLM prompt construction, agent call, response parsing, rule-based fallback prose). `performStopResearchLogic` (**225** lines — worker item 2 corrected the brief's figure of 158). `performDiscoveryMining` (184 lines). | `tracks.go:859-1057`; `research.go:258-482`; `discovery.go:338-521` (all `be/server/handlers/`) | Each worker report has a concrete helper-by-line-range split; residuals land at ~35, ~40 and ~35 lines. |
| F4 | **The glitch detector is written three times inside one package.** `filterPointSlice` (`filter.go:141-218`) and `FilterTrackPoints` (`:238-319`) are line-for-line equivalent modulo accessors — compare `:159-197` against `:256-294`: identical structure, identical literals (0.5, 15.0, 5.0), identical `lastValidIdx` watermark and `glitchMap` compaction. Copy 3 is inlined at `parser.go:172-203` and re-runs a subset of the same tests on points `FilterTrackPoints` already cleaned one line earlier at `parser.go:124`. | `be/gpx/filter.go:141-218`, `:238-319`; `be/gpx/parser.go:172-203` | One `isSpeedGlitch(prev, cur speedSample) bool` with an adapter per point type. Removes ~150 lines and forces resolution of the one place the copies diverge: `FilterTrackPoints` returns the original slice when everything was filtered (`:315-317`), `filterPointSlice` does not (`:218`) and `FilterGPX` compensates externally at `:83-85`. |
| F5 | **Values interpolated into log messages with `fmt.Sprintf` instead of passed as slog attrs.** Per AGENTS.md production forces `slog.NewJSONHandler`, so month, stop ID, region name, durations and byte counts all end up as unqueryable prose inside `message`. | `research.go:261, 358, 437, 443, 480, 586, 593, 620, 654`; `discovery.go:83, 108, 212, 250, 346, 352, 354, 371, 376, 381, 385, 391, 395, 399, 426, 449, 453, 458, 477, 504, 517, 520`; `recommendation.go:299, 452`; `guide.go:191, 271` | Mechanical conversion to key/value attrs. `lookout.go` and `tracks.go` already do this correctly. |
| F6 | **Three Google API clients living in HTTP handler files**, each constructing its own `http.Client` and reading `os.Getenv` directly, bypassing the dependency injection `Handler` exists to provide. None can be tested without a network stub. | `research.go:100-132` (`getStaticMap`, also dead), `:134-181` (`geocodeFacility`); `passages.go:145-199` (`isWaterPoint` / Geocoding) | Move behind an injected interface on `Handler`, in an `internal/maps` package. |
| F7 | **Three unexplained leg-matching tolerances and one unnamed planning speed, all driving user-visible numbers.** `plannedDist / 5.5 * 60` (`tracks.go:883`) is an assumed 5.5 kt planning speed that silently produces the debrief duration-variance percentage. Six proximity thresholds for the same "does this track match this leg?" question: 15.0 NM (`tracks.go:607`), −5.0 same-day score bonus (`:611`), 10.0 NM (`:652`), 2.0 NM (`:657`), 8.0 NM (`:771`), 3.0 NM (`lookout.go:433`). | `be/server/handlers/tracks.go:607, 611, 652, 657, 771, 883`; `lookout.go:433` | Named constants with one line each saying what they protect against. Three different tolerances (3/8/15 NM) for one concept need a stated reason. |
| F8 | **`FilterGPX` returns a glitch count of 0 together with an error after it has already found glitches.** `be/gpx/filter.go:108-110` returns `(data, 0, err)` on marshal failure even though `totalGlitches > 0`, and the sole caller discards the error (`parser.go:22-24`). Contract contradiction; latent rather than live (see §2 "not carried forward"). Separately, the godoc at `filter.go:56` says it "removes glitches and returns clean XML" while the comment at `:91-93` correctly says routes and waypoints are deliberately untouched. | `be/gpx/filter.go:56`, `:108-110`; `be/gpx/parser.go:22-24` | Return the real count; `slog.WarnContext` at the call site; fix the godoc. |
| F9 | **One physical constant spelled two ways, applied to the same data in the same request.** `1.943844` (`filter.go:132`) vs `1.94384` (`parser.go:71`). `FilterGPX` runs first (`parser.go:22`), then `ParseGPX` re-reads the same XML — so a device-reported speed is classified for glitch detection against one constant and reported to the user against the other. | `be/gpx/filter.go:132`; `be/gpx/parser.go:71` | One named `MPS2Knots` constant alongside the others at `parser.go:13-17`. The researcher module already names it. |
| F10 | **`repairMathInJSON` is not JSON-aware and rewrites prose.** It regex-scans the entire agent response for `number ± number` and evaluates the arithmetic, because the LLM sometimes emits `"latitude": 37.8 + 0.05`. A description string like `"Berth 3 - 5"` or `"open 9 - 5"` is silently rewritten to `"Berth -2.0000000"`. Completely uncommented. | `be/server/handlers/recommendation.go:77-99`, called `:406` | Restrict to numeric value positions, and say in a comment what it is for. |

---

## 5. Findings by package

### `be/server/handlers/` — the problem area

| Finding | file:line | Rating |
|---|---|---|
| Inline session-cookie authentication inside a handler, reading `navalplan_session` and calling `h.DB.GetSession` directly with the session error discarded at `:117`. Every other handler gets identity from middleware via `GetPersonFromContext`. An auth path that bypasses the middleware it should be part of. | `guide.go:115-120` | Critical |
| Business logic embedded in the transport layer: track pairing and debrief synthesis (`tracks.go:442-1057`), region dedup/prune arbitration (`discovery.go:121-336`), facility ring filtering (`research.go:45-98`), passage interpolation (`passages.go:28-96`), weather fetch called as a library by cron with no HTTP involvement (`weather.go:27`). The repo already demonstrates the right pattern with `be/gpx` and `be/safety` — note `lookout.go:607` correctly delegates to `safety.Evaluate`. | as listed | Critical |
| `lookout.go` is two files: HTTP triggers and audit orchestration (`:20-310`), then a general-purpose geo/format library (`:312-788` — `extractCoordinates`, `bearingToCardinal`, `calculateBearing`, `lookoutHaversineNM`, `extractWaypointsSummary`, `buildTravelTable`, `normalizeLookoutAlertIcon`). The seam is already crossed: `tracks.go:459` consumes `extractCoordinates`. | `lookout.go:312-788` | Critical |
| `tracks.go` is three files: handlers (`:20-440`), a duration parse/format library (`:476-571`, which already has its own `duration_internal_test.go`), and the debrief engine (`:442-1057`). | `tracks.go` | Critical |
| `handlers.go` is three files: `DBStore` + `Handler` + `New` (`:22-197`), job registry and agent health (`:199-273`), and LLM output-repair utilities `bulletKeyRegex`/`cleanJSON`/`repairTruncatedJSONArray` (`:275-359`). The JSON salvage block has nothing to do with handler wiring; `repairMathInJSON` (`recommendation.go:77`) is the same concern stranded in a feature file. | `handlers.go:22-365` | Worth doing |
| Character-for-character identical debrief-hydration block in two handlers: unmarshal `t.Debrief`, backfill `TrackName`/`TrackID`/`VoyageStopID`, call `ResolveDebriefStartStop`, append. | `voyages.go:491-512` ≡ `guide.go:408-429` | Worth doing |
| Two search-radius validation rules for one field: `CreateVoyage` rejects only negatives and defaults 0→20, accepting 5000; `UpdateVoyageConfig` enforces 1..300. A voyage can be created in a state its own config endpoint rejects. | `voyages.go:161-175` vs `:414-435` | Worth doing |
| Round-trip detection (`const threshold = 0.001`) implemented twice in one file, and the briefing-clone literal at `:281-288` duplicated verbatim at `:631-638`. | `research.go:264-300`, `:599-609` | Worth doing |
| Six copies of "collect tracks where `Kind == TrackKindPlanned`". | `tracks.go:728-733, 789-792, 821-824`; `lookout.go:374-378, 382-386, 389-394` | Worth doing |
| Two copies of "find stop by ID, title it `A to B`, else `A`, else `stops[0] to stops[1]`", in slightly different shapes. | `tracks.go:706-724` vs `:898-915` | Worth doing |
| The fetch-weather / upsert / count loop appears three times. | `weather.go:237-250`, `:277-290`; `scheduled.go:44-53` | Worth doing |
| `InviteUser` is a strict subset of `BulkInviteUsers`, and they **disagree**: bulk lowercases and de-dupes via `splitEmails` (`:162-184`), single-invite stores the raw string, so a mixed-case single invite will not match a later lowercase lookup. | `admin.go:129-158` vs `:186-233` | Worth doing |
| Two concurrency models for one operation, no comment on why: `TriggerVoyageLookout` fans out with a `WaitGroup` and per-stop goroutines; `RunLookoutAuditEndpoint` runs identical per-stop work strictly sequentially. Both claim the same `lookout:%d` job keys and both gate on `ResearchSem`. | `lookout.go:130-157` vs `:170-220` | Worth doing |
| A region is marked "replaced" by setting `parsedActive[i].Name = ""` as a tombstone, read back as the `ex.Name == ""` skip at `:432`. Invisible at the read site. | `discovery.go:456` | Worth doing |
| Error classification by substring: `strings.Contains(err.Error(), "503") \|\| strings.Contains(err.Error(), "high demand")`. `"503"` matches any error text containing those digits. Same ladder again at `recommendation.go:386-394`. | `research.go:390` | Worth doing |
| `errors` collapsed into silent success: `discovery.go:244-255` logs per-month failures then `return totalPruned, nil`, so an all-months run in which every month failed returns HTTP 200. `discovery.go:397-400` logs a `ListRegionsByMonth` failure and continues with `activeRegions == nil`, disabling all dedup for that run and writing duplicates. `lookout.go:368-371` converts a store failure into "no planned tracks exist", degrading the safety audit silently. | as listed | Worth doing |
| `writeError(w, 404, err.Error())` leaks the raw datastore error string to the client at 8 sites, while `voyages.go:465` and every other handler in the package use a fixed `"Voyage not found"`. | `voyages.go:30, 67, 204, 233, 275, 310, 397, 542` | Worth doing |
| Bounding-box IoU/containment computed in raw degrees², so a 5°×5° box near the Arctic has a fraction of the real area of one at the equator, and the antimeridian is unhandled — a region spanning ±180° gets a nonsense bound. For a *global* region deduper that needs stating. | `discovery.go:163-194` | Worth doing |
| Two waypoint-sampling densities in the same prompt path: `numPts > 8 → step = numPts/6` and `len(coords) > 10 → step = len/10`. Both feed the same lookout prompt. | `lookout.go:497-498` vs `:539-540` | Worth doing |
| Model types declared in handler files: `AgentOutput` and `Facility` (`research.go:22-43`) — `Facility` is persisted inside `Briefing.Facilities` and re-parsed in three places — and the exported, JSON-tagged response type `TravelTableRow` declared mid-file between two helpers (`lookout.go:685-689`). | as listed | Worth doing |
| `performStopResearchLogic` calls `performLookoutAuditLogic` inline at three points, synchronously, inside its own timeout. A workflow decision buried in a leaf function. | `research.go:293, 477, 645` | Worth doing |
| Undocumented clamps that look like DB column limits (`999999.99`, `999.99`) but say so nowhere. | `tracks.go:1124-1141` | Worth doing |
| `len(*track.RawGPX) < 100000` gates inlining raw GPX into the LLM prompt — an unexplained token-budget guard. | `tracks.go:990` | Worth doing |
| Genuine seamanship rules with zero explanation: a 30-minute pre-sunset arrival margin (`lookout.go:720`) and the hull-speed ladder `{4,5,6,7,8}` the whole travel table is built on (`lookout.go:692`). | as listed | Worth doing |
| Naming: `…Logic` suffix on three wrapper/body pairs with three different conventions (`research.go:229`/`:258`, `lookout.go:223`, `guide.go:181`/`:188`); `performStopResearch` is a 5-line semaphore wrapper differing from its body only by that word. Three shapes for one lookout concept: `TriggerLookoutAudit` / `TriggerVoyageLookout` / `RunLookoutAuditEndpoint` (the third stutters — `Endpoint` on a method already registered as an endpoint). `recommendation.go:285` takes both `sessionID` and `progressSessionID` 15 lines apart, while `guide.go:196` already uses the better name `agentSessionID` for the same concept. `hasValidFacilities` vs `hasReusableFacilities` (`research.go:236`, `:247`) differ only by a count threshold; "valid" means "non-empty". | as listed | Worth doing |
| `recommendation.go:32` `count := int(math.Round(nm * 1.0))` — the `* 1.0` is a no-op reading as if a conversion factor was intended and lost. | `recommendation.go:32` | Worth doing |
| `27.0` NM prompt-engineering threshold with no derivation; `const threshold = 0.001` (≈110 m round-trip test) declared twice and explained neither time. | `recommendation.go:71-72`; `research.go:270`, `:605` | Worth doing |
| Failure state encoded as the magic string prefix `"Error:"` in `Summary`, written in one function and tested in another. | `guide.go:279`, `:288` | Worth doing |
| Stale numbered comment: `// 4. Check existing StartStopID…` where step 4 was already used at `:649`. The sequence runs 1,2,3,4,4,5. | `tracks.go:672` | Worth doing |
| Re-queries stops already fetched at `:385`, shadows the outer slice, hard-codes a `100` limit the outer call does not have, and discards the error. | `guide.go:410` | Worth doing |
| Collapses a real DB error and a legitimate "no briefing yet" into one silent skip. | `scheduled.go:55-58` | Worth doing |
| `ctx := context.Background()` discards the request context, so the scheduled run cannot be cancelled and loses the trace ID that `GetTraceFromContext` (`context.go:39`) exists to propagate. | `scheduled.go:16` | Worth doing |

### `be/server/` core

| Finding | file:line | Rating |
|---|---|---|
| `auth.go` imports `github.com/charmbracelet/log` directly and calls package-level `log.Error`/`log.Info` at 13 sites, bypassing `slog` entirely — so in production these never reach the JSON handler and are invisible to the log index. | `auth.go:16`, calls at `:22, 34, 41, 53, 60, 69, 73, 80, 88, 95, 102, 109, 115` | Critical |
| SSE flushing depends on an undocumented path substring: `strings.Contains(r.URL.Path, "/stream")` exempts a response from gzip. `gzipResponseWriter` (`:86-93`) implements neither `Flush()` nor `Unwrap()`, so `http.NewResponseController(w).Flush()` — used by `handlers/progress.go:61` and `handlers/recommendation.go:218` — returns `ErrNotSupported` for anything that does get wrapped. Every SSE route happens to contain `/stream` today; the next one that does not will hang with no error. | `middleware.go:102` | Critical |
| DB failures in `requireAuth` become 401s with no log, making a database outage indistinguishable from a bad session. | `middleware.go:160-170` | Critical |
| `oauthstate` and logout cookies omit `Secure` and `SameSite`; the set and clear paths duplicate the field list and **disagree** on exactly those two fields. | `auth.go:120-128` vs `:138-144` | Critical |
| The system-API-key bypass is written twice in different shapes — `requireAuth` reads the header into a local and compares, `requireAdmin` inlines it. Two copies of one security check that must stay identical. | `middleware.go:145-150` vs `:185-188` | Worth doing |
| `corsMiddleware` sets an origin allowlist plus `Access-Control-Allow-Credentials: true`, and both SSE handlers then overwrite `Access-Control-Allow-Origin` with `"*"`. Browsers reject `*` with credentials, so those two handlers are likely broken cross-origin *and* duplicate logic the middleware owns. A third copy lives at `rs/telemetry/telemetry.go:222`. | `middleware.go:258-281` vs `handlers/progress.go:57`, `handlers/recommendation.go:214` | Worth doing |
| `gzipMiddleware` sets `Content-Encoding: gzip` but never sets `Vary: Accept-Encoding` and never clears a pre-existing `Content-Length`. Combined with `staticCache`'s `public, max-age=31536000, immutable` on `/assets/` (`:78`), an intermediary can cache a gzipped asset and serve it to a client that did not advertise gzip. | `middleware.go:113-118` | Worth doing |
| Seven singular/plural route aliases (`/track` and `/tracks`, …), fourteen lines carrying seven routes. This is API surface debt: both spellings are now public and must be supported forever. | `routes.go:59/60, 103/104, 105/106, 107/108, 109/110, 111/112, 113/114` | Worth doing |
| `New` returns `(*Server, error)` and never returns a non-nil error — the only return is `s, nil` at `:61`. Every caller branches on something that cannot happen. | `server.go:29-62` | Worth doing |
| Twelve config values logged as twelve separate records all sharing the message `"config"`, with **PascalCase** keys (`Env`, `Port`, `DatabaseDSN`, …) where every other key in the codebase is lowercase. | `main.go:78-89` | Worth doing |
| Stale comment: `// Middleware wraps the handler with standard middleware (Logging, CORS, Recovery)` — the chain is six deep and also includes `otelhttp`, `traceMiddleware` and `sanitizePathMiddleware`. A reader trusting it will not know path sanitization or tracing happen here. | `server.go:64` | Worth doing |
| `rateLimitEntry.lastSeen` is the window *start*, not the last sighting — it is only reassigned when the window rolls (`:46`), never on an in-window hit. The name also implies an eviction reaper that does not exist. | `middleware.go:18, 44-46` | Worth doing |
| `CloudLoggingHandler` is misleading: it is the handler in *both* environments — `main.go:58` wraps the charmbracelet dev logger in it too. Its actual job is trace injection and duration styling. | `log_handler.go:14` | Worth doing |
| `len(trimmed) > 5` is `len("/api/")`, guarding against rewriting `/api/` itself. Unexplained. | `middleware.go:286` | Worth doing |
| `Server` exposes `DB`, `Handler`, `SystemAPIKey`, `GoogleConfig` and `Mux` as public fields, and middleware reaches through `s.DB` directly (`:160`, `:166`) rather than through `Handler` — two parallel paths to one store. `main.go` needs only `Mux`, `Routes`, `Middleware`. | `server.go:17-26` | Nitpick |

### `be/gpx/`, `be/agent/`, `be/store/`, `be/config/`, `be/model/`, `be/telemetry/`, `be/safety/`

| Finding | file:line | Rating |
|---|---|---|
| `simplify.go`'s entire distance model is undocumented. `perpendicularDistanceNM` is an **equirectangular** approximation, not the haversine used everywhere else; the bare `60.0` at eight sites is NM-per-degree-of-latitude and `cosLat` is the longitude convergence correction. A reader will reasonably read `60.0` as a minutes-per-degree unit bug. Nothing states why flat-earth is acceptable here (RDP operates on segments far shorter than the curvature error). | `be/gpx/simplify.go:42-76` | Critical |
| `buildParsedTrack` is 146 lines doing six separable jobs with glitch state threaded through a metrics accumulator. | `be/gpx/parser.go:123-268` | Critical |
| `RunStreaming` ×2: 116 and 119 lines. The first forks on reasoning-engine vs HTTP, sniffs the first byte to choose JSON-array vs line parsing, and inlines two character-identical event-extraction loops (`:209-217` ≡ `:257-263`). The second holds span setup, input marshalling, gRPC receive, a hand-rolled line re-assembler, SSE framing and three separate fallback-to-`RunSync` paths. | `be/agent/agent.go:157-272`; `be/agent/reasoning_engine.go:165-283` | Worth doing |
| `config.New` is 154 lines running four phases inline: required-var validation, defaulting, DSN assembly with a socket/TCP fork, struct population. | `be/config/config.go:47-200` | Worth doing |
| Stale godoc that would actively mislead: `// ObscureString replaces the input string with asterisks of the same length.` It replaces occurrences of `input[1]` inside `input[0]`, and returns `input[0]` unchanged when `input[1]` is empty. It is also a strictly 2-argument function dressed as variadic (`input ...string`, reads `[0]` and `[1]`, ignores the rest) — so `ObscureString(a, b, c)` silently drops `c`. | `be/config/config.go:202-219` | Worth doing |
| Two parallel XML type hierarchies in one package with no comment explaining why: `types.go`'s `GPX`/`Trk`/`TrkPt` (time as `*time.Time`) vs `filter.go`'s `filterGPXDoc`/`filterTrack`/`filterPoint` (time as `string`). The real reason — filter must round-trip unparseable timestamps byte-for-byte on re-marshal, which `*time.Time` cannot do — is a good justification that is nowhere written down. | `be/gpx/types.go:9`; `be/gpx/filter.go:11-12` | Worth doing |
| `Lon` on the XML types vs `Lng` on the domain type, in one package. Every conversion site has to remember which side it is on, and the two haversine call sites in one file read `pts[...].Lon` and `points[...].Lng` 97 lines apart. | `be/gpx/types.go:29, 47, 69, 80`; `be/gpx/filter.go:47` vs `Point.Lng` | Worth doing |
| `ListStopsInWindow` and `ListAllFutureStops` are the same 17-line query differing only by one date clause — and **both `JOIN voyage v ON s.voyage_id = v.id` without ever referencing `v`** (`:99`, `:117`). If that join is standing in for a dropped `v.person_id` filter, a briefing sweep silently covering all users' stops is a different thing from one scoped to a user. | `be/store/briefings.go:93-126` | Worth doing |
| `resolveProjectID` runs the same four-key ladder twice, gated by `isNumeric` the first time and ungated the second. The reason — a GCP project *number* is numeric and is not a valid project *ID*, so prefer a real ID but fall back to anything — is nowhere stated, and the two-pass shape reads as a copy-paste bug without it. | `be/telemetry/telemetry.go:22-74` | Worth doing |
| Stutter: `gpx.ParseGPX`, `gpx.FilterGPX`, `gpx.GPX`, `gpx.parseGPXTime`; `agent.AgentRunner` and `agent.AgentEvent` (while the sibling in the same package is correctly `ReasoningEngineRunner`). | `be/gpx/parser.go:21`, `filter.go:58`, `types.go:9`; `be/agent/agent.go:39, 69` | Nitpick |
| `store.DB` is not a database handle — it embeds `*sqlx.DB` and hangs ~60 domain methods off it, so every call reads `db.ListVoyageBriefings(...)`. `store.Store` with a `db *sqlx.DB` field would name the thing it is and stop leaking the connection as public surface. | `be/store/db.go:11-13` | Nitpick |
| `RegionWithSeasonality` repeats seven fields of `RegionSeasonality` verbatim. It cannot embed it because of the `ID`/`CreatedAt` collision — but that deserves the one-line comment it does not have, since the two lists will drift. | `be/model/models.go:159-178` | Nitpick |
| Reasoning-engine span setup duplicated verbatim, including the same `gen_ai.operation.name = "reasoning_engine_query"` for both a unary query and a stream. | `be/agent/reasoning_engine.go:41-48` ≡ `:166-173` | Nitpick |
| **`be/safety/` is clean.** No findings. `validator.go:1-7` explains why the logic moved out of the LLM, `:37-46` names every threshold, `:6-7` and `:60` call out the non-obvious exclusions. This is the model the rest of the repo should follow. | — | — |

### `rs/tool/`

| Finding | file:line | Rating |
|---|---|---|
| **Three tide datums merged into one undocumented field.** NOAA predictions are requested on MLLW (`:157`), NIWA on LAT (`:398`), and the UK ADMIRALTY events path returns Chart Datum (`:243`, `:259`). All three land in `TideEvent.Height` with `Unit: "ft"` (`:176`, `:344`, `:456-457`) and are presented to the agent interchangeably, with no datum recorded anywhere in `TideResult`. Three different zero-references, one field, zero comments. | `rs/tool/tides.go:157, 243, 259, 398` | Critical |
| **Fabricated climatology contradicts the project's own documented stance.** `weather.go:131-176` returns invented values — 12 kt NW wind, 72 °F, 3 ft waves at 8 s, 290° — labelled `"Seasonal Average"`, location-independent and distinguishable from real data only via `ForecastType`. Two sibling subsystems state the opposite policy explicitly: `safety_feed.go:20-22` ("degrades gracefully to 'no data' … never blocked or seeded with fabricated hazards") and `adapters.go:287-289` ("returns an explicit 'no data' response rather than fabricated hazards, so the safety audit is never seeded with mock data"). | `rs/tool/weather.go:131-176` | Critical |
| Three oversized functions: `FilterGPXDataResult` (309 lines — argument validation through four point-ingestion paths, three glitch rules, XML re-emit and prose summary), `QuerySailingDirections` (237), `FindPlaces` (195). Each worker report carries a concrete helper split; residuals ≈30/50/35 lines. | `gpx_filter.go:116-424`; `sailing_directions.go:138-374`; `places.go:120-314` | Worth doing |
| `tides.go` is four files — `NOAAProvider` `:70-180`, `UKProvider` `:182-364`, `NIWAProvider` `:366-469`, `TideManager` `:471-529` — and the author's own `// --- X ---` banners at `:70`, `:182`, `:366`, `:471` mark the split points. `adapters.go` is two: 10 MCP DTO declarations (`:13-131`) and the service-method layer (`:133-377`). | as listed | Worth doing |
| Severity inverted: local programming errors log at `ErrorContext` (marshal `:252`, request build `:263`, decode `:302`) while actual external failures log at `WarnContext` (network `:275`, non-200 `:289`). Both produce the identical user-visible notice, so an upstream outage is logged *quieter* than a bug. | `rs/tool/sailing_directions.go:252-302` | Worth doing |
| Only 3 of 14 files log at all (`sailing_directions.go`, `adapters.go:259`, `stop_anchor.go:111`). `tides.go`, `weather.go`, `places.go`, `safety_feed.go`, `gpx_filter.go` and `search.go` emit nothing — including on every dropped error. | package-wide | Worth doing |
| Four `parseGPXTime` errors dropped at every ingestion site; the point survives with `Time == nil` and is then invisible to both the acceleration and spike rules. | `gpx_filter.go:156-158, 186-188, 208-210, 226-228` | Worth doing |
| O(n²): a full linear scan over `pts` *inside* the triple-nested track/segment/point loop, recovering an index already known from `tIdx`/`sIdx`/`pIdx`. ~10⁸ comparisons on a 10 k-point track. | `gpx_filter.go:372-378` | Worth doing |
| `UKProvider.GetTides` fetches the nearest station twice when `candidates[0]` succeeds but lacks both tide types — the candidate loop (`:242-256`) and the fallback (`:259-274`) both call `p.client.Events(…, 7)` and `filterUKEvents`. | `rs/tool/tides.go:204-275` | Worth doing |
| Leaking abstraction: `FindPlaces` calls `areaSearchRadius(ctx)` and silently enlarges the caller's requested radius from hidden session state. The coordinate half of exactly this "trust the session over the LLM" rule already lives correctly one layer up in `anchorRequest` (`adapters.go:250`); the two halves are split across two layers. | `rs/tool/places.go:132-134` | Worth doing |
| Misplaced generic helpers: `haversineDistance` and `offsetLatLng` (spherical math in a Places file, the former used by `stop_anchor.go:95`), `toFloat` (a generic `any`→`float64` coercion in an anchor file), `DegreesToDirection` (compass math in a weather file, consumed by `heading.go:37`). | `places.go:23`, `:107`; `stop_anchor.go:78`; `weather.go:333` | Worth doing |
| Wrong godoc on two no-op `Close` methods — both say "closes the underlying client connection" above a body that is `return nil`. (`places.go:80-83` is correct and really does close.) | `weather.go:60-63`; `sunrise.go:41-44` | Worth doing |
| Stale unit in a comment: "if under 100, assume miles/NM passed and convert to meters" and "Convert NM/miles to meters" — the factor is `1852`, nautical miles only; a statute mile is 1609.34. | `places.go:123`, `:128` | Worth doing |
| Stale threshold in a comment: the search-strategy block describes two thresholds; the code uses three (`<= 10000` Nearby `:180`, `> 10000` RELEVANCE `:201`, `> 15000` sector fan-out `:222`). | `places.go:173-177` | Worth doing |
| Bare `recover()` ×2 with no explanation of what panics, under what conditions, or whether it is an ADK quirk being worked around; the panic value is discarded. | `stop_anchor.go:24-28`, `:56-60` | Worth doing |
| Error-as-flag: an error value is manufactured purely to make `processResults` skip the marine branch. | `weather.go:184` | Worth doing |
| Exported tuning constants with no doc comments — and the backend twins at `be/gpx/parser.go:13-15` **do** carry the rationale. The copy lost it. | `gpx_filter.go:12-15` | Worth doing |
| Four unexplained glitch constants: `dtSec <= 60.0` (`:291`), `deltaV > 0.5` (`:297`), `dtPrev/dtNext <= 15.0` and `> 5.0` kt (`:309-310`). | `gpx_filter.go:291-310` | Worth doing |
| The same 6-line corpus-append block appears three times (once per switch arm, twice inside `default`); `resolveCorpus` (`:380-392`) re-implements `formatCorpusResource` (`:457-462`), which it already calls on four other paths. | `sailing_directions.go:176-207`, `:380-392` | Worth doing |
| Raw-stats and clean-stats loops are the same max/sum/count over `pts` with a different skip predicate; the four point-ingestion blocks are near-identical. | `gpx_filter.go:250-267` ≡ `:340-358`; `:141-232` | Worth doing |
| `adapters.go` is ~150 lines of field-for-field DTO copying — `SailingDirectionsRequest/Response` duplicate `SailingDirectionsArgs/Result` exactly, and `FetchSailingDirections`/`FetchCoastPilot` exist only to copy one shape into the other and are the same shape as each other. A deliberate MCP-vs-ADK boundary, but nothing says so. | `adapters.go:300-364` | Worth doing |
| Naming: `FilterGPXDataResult` returns a `*FilterGPXResponse` and `CalculateHeadingResult` returns a `*HeadingResponse`; `Result`/`Response`/`Mcp` prefixes are applied inconsistently across one layer boundary (the `Mcp` prefix marks 2 of 7 MCP types); `TideClient` (`:54`) is NOAA-specific while its siblings are `UKTidalClient` and `NIWAClient`. | `gpx_filter.go:116`; `heading.go:34`; `tides.go:54` | Worth doing |
| Unbounded external payload into logs: the full response `body` logged at Warn on every non-200. | `sailing_directions.go:294` | Nitpick |
| **Clean files:** `errors.go`, `place_type.go`, `service.go`, `sunrise.go`. `heading.go` has one naming nitpick and otherwise is clean and well-commented. | — | — |

### `rs/` core, `sync/`

| Finding | file:line | Rating |
|---|---|---|
| Full request body logged at Info on **every POST, in all environments**. These bodies are agent prompts: voyage plans, coordinates, free-text user input, GPX telemetry. It also buffers every POST into memory ahead of the handler, and the `ReadAll` error is swallowed by `if err == nil` with no else-branch. | `rs/server/middleware.go:40-48` | Critical |
| Hardcoded 19-digit reasoning-engine ID and a hardcoded `us-central1`, applied only when `env == "production"`, with no comment on provenance, what breaks if wrong, or why it is not `cfg.VertexLocation`. Redeploying to a new project inherits a stale ID and silently mis-attributed traces. | `rs/telemetry/telemetry.go:449-451` | Critical |
| Env read in the wrong layer, contradicting config. `createAgent` runs its own `GOOGLE_GENAI_USE_VERTEXAI` / `GOOGLE_CLOUD_LOCATION` ladder with default `"global"`, while `rs/config/config.go:123-132` already resolves exactly this into `cfg.VertexLocation` with default `"us-central1"`. Two resolvers for one concept that can legitimately disagree — and `cfg.VertexLocation` is passed to the nautical service at `main.go:60`, so the RAG corpora and the model can end up in different regions. | `rs/agent/agent.go:332-339` | Critical |
| Unchecked type assertion: `f, _ := w.(http.Flusher)` then `f.Flush()` unconditionally. Latent, not live — the writer in the chain (`rs/server/middleware.go:19`) does implement `Flush()` at `:29-33`. The same module gets it right 40 lines away at `telemetry.go:225-229` (checks `ok`, returns 500 "Streaming unsupported"). | `rs/server/reasoning_engine.go:185, 190, 212` | Worth doing |
| The agent list is hardcoded next to the list that already exists: `NewMultiLoader` enumerates all six agents literally while `agent.Order` holds the same six and is used eleven lines later at `server.go:58`. Adding a seventh and forgetting `:40-47` registers it over A2A but leaves it invisible to the ADK REST loader. | `rs/server/server.go:40-47` | Worth doing |
| Package comments wrong on both top-level files. `main.go:1-2` names "Researcher, Guide, Discovery" — none of those three exist. `agent.go:1-3` lists five agents and omits **debrief** (`:36`, `:98-104`), and says each agent "shares" the Google Search sub-agent when in fact each gets its own instance (`toolsFor:222` names it `sp.name+"_search_specialist"`) and debrief and lookout get none at all (`:103`, `:162`). | `rs/main.go:1-2`; `rs/agent/agent.go:1-3` | Worth doing |
| Sniffing an ADK type by the substring `"tool"` in `fmt.Sprintf("%T", ctx)`, then blanket-`recover()`ing from `ctx.Session()`. Needs a comment naming the type that panics and the version it was seen in; as written an unrelated future panic is eaten and sessions silently lose their ID. | `rs/telemetry/telemetry.go:283-289` | Worth doing |
| Magic colour codes de-commented during the copy: `"255"/"226"/"208"/"196"` are bare here, while the file this was copied from annotates each (`be/server/log_handler.go:34-40`: `// White`, `// Yellow`, `// Orange`, `// Red`). The duplicate lost the only thing that made it readable. | `rs/telemetry/telemetry.go:135-141` | Worth doing |
| `config.New` is 125 lines of flat env ladders; `ResolveProjectID` runs the same four-key ladder twice differing only by an `!isNumeric` guard; `handle` and `handleStream` share an identical 15-line preamble. | `rs/config/config.go:29-154`; `rs/telemetry/telemetry.go:390-428`; `rs/server/reasoning_engine.go:106-120` ≡ `:161-175` | Worth doing |
| Split-brain model defaults with no rationale at the site: `gemini-3.5-flash-lite` for everything, `gemini-4-argon` for the commodore alone. The *why* lives only in the commodore's spec comment at `agent.go:125-126`. | `rs/config/config.go:40` vs `:45` | Worth doing |
| `sync/main.go` — unreachable error branch and duplicated fallback: `resolveCorpusResourceName` returns the synthesized name *and `nil`* on no-match (`:182`), so the caller's `if err != nil → warn → fallback` (`:190-194`) cannot fire for that case, and `:193` recomputes the byte-identical string `:182` already returned. | `sync/main.go:182, 190-194` | Worth doing |
| `sync/main.go` — `main()` does six jobs in 84 lines; the two RAG-import blocks (`:106-120` ≈ `:122-136`) are the same shape; three `NGAStoredPub` fields are never read (`:33`, `:34`, `:39`). | `sync/main.go:33-39, 56-139` | Worth doing |
| Browser `User-Agent` and `msi.nga.mil` `Referer` spoofing on both the list and download requests, twice, unexplained. Someone will "clean this up" and break NGA sync; one comment saying NGA rejects non-browser clients would prevent that. | `sync/main.go:438-439, 486-487` | Worth doing |
| **Clean files:** `rs/session/session.go` and `rs/prompt/prompt.go` — no findings. | — | — |

---

## 6. Cross-cutting issues

### (a) `be/gpx/filter.go` vs `rs/tool/gpx_filter.go` — how duplicated are they really?

**Verdict: substantially duplicated in the part that matters, and the recommendation is to
delete one side rather than extract a shared module.**

What is genuinely shared — confirmed line by line:

| Concern | backend | researcher | Identical? |
|---|---|---|---|
| Earth radius | `parser.go:13` `3440.065` | `gpx_filter.go:15` `3440.065` | Yes |
| Max plausible speed | `parser.go:14` `45.0` | `gpx_filter.go:13` `45.0` | Yes |
| Max acceleration | `parser.go:15` `2.0` | `gpx_filter.go:12` `2.0` | Yes |
| m/s → kt | `filter.go:132` `1.943844` | `gpx_filter.go:14` `MPS2Knots = 1.943844` | Yes (backend's own `parser.go:71` says `1.94384` — see F9) |
| Rule 1, absolute | `filter.go:160`, `:257` `spd > MaxPlausibleSpeedKts` | `gpx_filter.go:282` | Yes |
| Rule 2, acceleration | `filter.go:176` with `0 < Δt <= 60.0` | `gpx_filter.go:291-296` | Yes |
| Rule 2b, zero-elapsed | `filter.go:179` `dtSec <= 0 && deltaV > 0.5` | `gpx_filter.go:297-299` | Yes |
| Rule 3, 3-point spike | `filter.go:189-194` `dtPrev,dtNext <= 15.0` and `> 5.0` kt over both neighbours | `gpx_filter.go:304-318` | Yes, same literals |
| `lastValidIdx` baseline strategy | `filter.go:203` | `gpx_filter.go:271` | Yes |
| Haversine | `parser.go:325-343` | `gpx_filter.go:426-435` | Same formula; backend **clamps `a` to [0,1]** at `:335-339`, researcher does not |
| `parseGPXTime` | `filter.go:322-341`, 6 formats, `TrimSpace` + empty guard, returns `*time.Time` | `gpx_filter.go:437-450`, 5 formats, no trim, no empty guard, returns `time.Time` | Near-duplicate, drifted |

Where they genuinely differ — and the divergence looks intentional, not accidental:

- **Purpose.** The backend is the **ingest path**: `FilterGPX` consumes XML and emits XML, is
  called once on upload (`handlers/tracks.go:51`, `:171`) and again from `ParseGPX`
  (`parser.go:22`), and returns `(bytes, glitchCount, error)` — nothing else. The researcher
  side is a **reporting/MCP-facing** variant: it carries a `GlitchRecord` audit trail with
  per-point reasons (`gpx_filter.go:35`, `:269`), raw-vs-clean statistics (`:250-267`,
  `:340-358`), and a prose `Summary` (`:400-410`). None of those exist on the backend.
- **Input surface.** The researcher accepts structured `TrackPointInput` as an alternative to
  XML (`:19-33`, `:215-232`); the backend takes bytes only.
- **Scope of filtering.** The backend deliberately leaves `<rte>` and `<wpt>` alone, with a
  correct justification at `filter.go:91-93` (route `<time>` is waypoint creation time, not
  travel time — there is even a regression test, `parser_test.go:379`). The researcher ingests
  route and waypoint points as fallbacks (`:169-214`) and runs travel-speed glitch rules on
  them, which is arguably the wrong thing to do, and then fails to re-emit them (see §3).

**Recommendation — do not extract a shared module. Delete `rs/tool/gpx_filter.go`.**

The deciding fact is not the duplication, it is reachability: the researcher copy has zero
production callers (§3), its only entry point `FetchFilterGPXData` is absent from both
`AsTools()` and the MCP dispatch, and the comment claiming otherwise (`rs/tool/service.go:133-136`)
is wrong. A shared `nav`/`gpxmath` module would be the right call if both sides were live —
the threshold constants, the haversine, `parseGPXTime` and the three-rule predicate are
textbook shared-library material. But creating a cross-module dependency to serve one live
consumer and one dead one is strictly worse than deleting the dead one. Do the in-package
unification instead (F4): that removes ~150 lines of real duplication inside `be/gpx/`, which
is where the duplication actually costs something.

If the researcher copy is ever revived, extract then — and carry over the `a`-clamp
(`parser.go:335-339`) and the constant rationale comments (`parser.go:13-15`), both of which
the copy lost.

### (b) Repeated geodesic math — do the implementations agree numerically?

**Eleven implementations across two modules. They agree to within one part in 100,000. There
is no numeric bug here — this is duplication, not divergence.**

| Function | file:line | Radius | Units | Clamps `a`? |
|---|---|---|---|---|
| `CalculateHaversineNM` | `be/gpx/parser.go:325` | `EarthRadiusNM` = 3440.065 (`:13`) | NM | **Yes** (`:335-339`) |
| `lookoutHaversineNM` | `be/server/handlers/lookout.go:730` | `const R = 3440.065` (`:731`) | NM | No |
| `haversine(…, unit)` | `be/server/handlers/recommendation.go:101` | km 6371.0, mi 3958.8, **nm 3440.1** (`:103-105`) | per arg | No |
| `haversineDistanceNM` | `rs/tool/gpx_filter.go:426` | `EarthRadiusNM` = 3440.065 (`:15`) | NM | No |
| `haversineNM` | `rs/tool/safety_feed.go:82` | 3440.065 (`:83`) | NM | No |
| `CalculateDistanceNM` | `rs/tool/heading.go:120` | 3440.065 (`:132`) | NM | No |
| `haversineDistanceMiles` | `rs/tool/tides.go:533` | 3958.8 (`:534`) | mi | No |
| `haversineDistance` | `rs/tool/places.go:23` | 6371000 (`:24`) | **m** | No |
| `perpendicularDistanceNM` | `be/gpx/simplify.go:42` | — equirectangular, `60.0` NM/deg | NM | n/a |
| `pointDistanceNM` | `be/gpx/simplify.go:70` | — same flat-earth approximation | NM | n/a |
| `offsetLatLng` | `rs/tool/places.go:108` | 6371000.0 (`:109`) | m | n/a (forward solution) |

Numerical agreement: 3440.065 NM × 1852 m = 6,370,999 m, so the NM and metre constants are the
same sphere. 3958.8 mi × 1609.344 = 6,371,071 m — agrees to 1 part in 10⁵.

The one outlier is **`recommendation.go:105` `earthRadiusNm = 3440.1`**, which differs from the
canonical 3440.065 by 0.035 NM in 3440 — about 0.0002 NM over a 20 NM search radius. Two worker
reports called this a disagreement in results; it is a consistency defect, not a correctness
one, and the report should not overstate it. Fix it because having two spellings of one
constant is how real divergence starts, not because any user-visible number is wrong today.

Two genuine robustness divergences are worth naming: only `be/gpx/parser.go:335-339` clamps
`a` to `[0,1]` before `Asin`/`Atan2`, and `be/gpx/simplify.go` uses an equirectangular
approximation while everything else uses haversine — correct for RDP, but undocumented
(see the `simplify.go` entry in §5).

**Bearing: four implementations, all the same formula.**
`be/gpx/parser.go:346` `CalculateBearing`, `be/server/handlers/lookout.go:669`
`calculateBearing`, `be/server/handlers/passages.go:201` `initialBearing`,
`rs/tool/heading.go:103` `CalculateBearing`. All compute the initial great-circle azimuth via
`atan2(y, x)` normalised to 0-360. The backend has three copies of this in one module, two of
them with the same name differing only in case.

**Cardinal conversion: three implementations that agree on the valid domain and diverge
outside it.**

| Function | file:line | Expression |
|---|---|---|
| `bearingToCardinal` | `be/server/handlers/lookout.go:354-362` | normalises negatives (`:356-359`), then `int(math.Floor((d+11.25)/22.5)) % 16` |
| `degreesToCompass` | `be/server/handlers/weather.go:21-25` | `int(math.Round(d/22.5)) % 16` — **no negative handling** |
| `DegreesToDirection` | `rs/tool/weather.go:333-337` | `int((d+11.25)/22.5) % 16` — truncation, **no negative handling** |

`round(d/22.5)` ≡ `floor((d+11.25)/22.5)` for `d >= 0`, and `int()` truncation equals `floor`
for positive values, so all three return identical strings for any input in `[0, 360]` — which
is all Open-Meteo ever supplies. But `degreesToCompass` and `DegreesToDirection` would panic
with a negative index for `d < -11.25`, where `bearingToCardinal` returns correctly. Only one
of three is robust, and nothing marks which.

**Recommendation:** one `internal/geo` package in the backend holding haversine, bearing and
cardinal conversion, with `lookout.go`'s normalising cardinal implementation as the survivor;
`lookout.go:730` and `recommendation.go:101` then delete. In the researcher module, one
`geo.go` holding `haversineNM` plus unit converters, replacing five functions and four radius
constants. Cross-module sharing is not worth a new module (see (a)).

### (c) Repeated HTTP client construction, timeout and retry scaffolding

**Confirmed: nine independent client constructions in non-test code, no shared client, no
shared transport, and five hand-rolled retry/fallback patterns.**

| Site | Construction | Timeout |
|---|---|---|
| `be/server/handlers/research.go:116` | `&http.Client{...}` inline, per call | 10 s |
| `be/server/handlers/research.go:153` | `&http.Client{...}` inline, per call | 10 s |
| `be/server/handlers/passages.go:167` | `&http.Client{...}` inline, per call | 10 s |
| `be/server/handlers/handlers.go:142-147` | fallback when `idtoken.NewClient` fails | zero value (none) |
| `be/server/handlers/handlers.go:149` | `client.Timeout = 0` on the idtoken client | **explicitly none** |
| `be/server/handlers/handlers.go:156-160` | local path, `DisableKeepAlives: true` | none |
| `be/server/handlers/handlers.go:176-177` | `healthClient` | 5 s |
| `be/server/auth.go:172` | `http.DefaultClient` (ctx timeout at `:168`) | via context |
| `rs/tool/safety_feed.go:201` | `http.DefaultClient` (ctx timeout at `:197`) | via context |
| `rs/tool/sailing_directions.go:64` | `var client HTTPDoer = http.DefaultClient` | via context |
| `sync/main.go:407`, `:420` | `&http.Client{...}` ×2 | 5 min |

Three distinct timeout strategies coexist: a `Client.Timeout` field, a per-request context
deadline, and nothing at all. The three 10-second clients in `research.go` and `passages.go`
are constructed *inside the request handler on every call* rather than once on `Handler`,
which also defeats connection reuse. `handlers.go:149`'s `Timeout = 0` is deliberate (agent
runs last tens of minutes) but carries no comment saying so, which makes it read as a bug.

Retry and fallback are hand-rolled five times, each differently:
`rs/tool/tides.go:122-140` (expanding-radius retry), `tides.go:242-256` (candidate list retry),
`rs/tool/weather.go:92-127` (live-then-historical fallback),
`rs/tool/sailing_directions.go:233-317` (parallel fan-out with graceful degrade),
`sync/main.go:282-328` (fixed 10 s poll, unbounded — see B4). No backoff anywhere; no jitter;
no shared notion of what is retryable.

The one good pattern already exists and should be the template: `sailing_directions.go:21-24`
declares an `HTTPDoer` interface and holds it as a field, so the provider is testable without
a network. Nothing else does this.

**Recommendation:** one constructed client per module held on the struct, injected as an
interface; a single `doJSON(ctx, client, req, &out)` helper carrying the status check, the
`LimitReader` cap and the decode (that trio is currently written out at
`sailing_directions.go:275-310`, `safety_feed.go:195-212` and `sync/main.go:289-320`
separately); and one backoff helper for the retry loops.

### (d) Repeated JSON cleaning / unmarshalling helpers

**This one does not check out as a cross-cutting duplication problem, and the report should
say so plainly.**

There are exactly three LLM-output-repair helpers in the entire repository, all in one
package, none duplicated:

| Helper | file:line | Callers |
|---|---|---|
| `cleanJSON` | `be/server/handlers/handlers.go:277` | `discovery.go:387`, `lookout.go:272`, `research.go:404`, `recommendation.go:405`, `guide.go:239`, `tracks.go:1005` |
| `repairTruncatedJSONArray` | `be/server/handlers/handlers.go:311` | `recommendation.go:416` |
| `repairMathInJSON` | `be/server/handlers/recommendation.go:77` | `recommendation.go:406` |

The researcher module has none — it decodes into typed structs throughout, which is the better
pattern. The only thing resembling a JSON helper there is the `map[string]any` ↔ struct
round-trip through `encoding/json` at `rs/tool/search.go:104-111` and `:118-128`, which is a
different concern (ADK argument marshalling) and is used twice in one file.

So the real finding is **placement, not duplication**: `repairMathInJSON` is the same concern
as the other two but is stranded in a feature file, and all three sit in `handlers.go`
alongside the `DBStore` interface and the `Handler` constructor. Move the three into one
`agentjson.go`. Of the three, only `repairTruncatedJSONArray` carries a comment — and
`repairMathInJSON` is the one that most needs one, plus a scoping fix (F10).

### (e) Error-wrapping and logging idioms

**Error wrapping: not a problem. Do not spend time here.** Counted across all non-test Go:
**96 uses of `%w`** (backend 48, researcher 38, jobs 10) against **4 uses of `: %v", err`**.
That is a convention being followed, not a divergence. The only real wrapping issue is local
and narrow: `rs/tool/tides.go:84, 147, 207, 387` return a bare sentinel (`ErrInvalidDate`)
and discard the underlying `time.Parse` detail entirely.

**Logging: this is where the divergence lives.** Four separate axes.

1. **Key spelling.** 149 sites use `"error"`, 21 use `"err"`. The minority is concentrated:
   `voyages.go` (13 sites — `:40, 76, 109, 137, 178, 250, 284, 361, 369, 438, 475, 481, 561`),
   `stops.go` (4 — `:49, 124, 173, 213`), `recommendation.go` (3 — `:159, 306, 443`),
   `passages.go` (1 — `:118`). In production both emit JSON, so one concept occupies two
   field names in the log index. Worker item 2 said "all four other files use `error`" within
   its own five-file scope and worker item 3 found `"err"` in three further files — both were
   right about what they could see; the repo-wide count above resolves it. 21 one-word edits.
2. **Structured attrs vs interpolated messages.** See F5 — `research.go`, `discovery.go`,
   `recommendation.go` and `guide.go` `fmt.Sprintf` values into the message while
   `lookout.go`, `tracks.go` and `weather.go` pass them as attrs.
3. **Bracketed prefixes baked into messages.** `[lookout]` / `[lookout:bulk]` (12 sites in
   `lookout.go`), `[discovery-mining] [%d/12]`, `[weather]` (14 sites), `[scheduled]` (8),
   `[guide-agent]`, `[navigator-agent]` — against no prefix at all in `tracks.go`,
   `voyages.go`, `stops.go`, `admin.go`, `handlers.go`. Under a JSON handler a prefix inside
   the message is strictly worse than a `slog.With("component", …)` attribute.
4. **slog vs charmbracelet.** `be/server/auth.go:16` imports `github.com/charmbracelet/log`
   and calls package-level `log.Error`/`log.Info` at 13 sites, so an entire authentication
   subsystem is invisible to production JSON logging. Everything else in both modules goes
   through `slog`. Note the two modules reach the same destination by different routes:
   the backend wraps a charmbracelet handler inside `CloudLoggingHandler` for dev
   (`be/main.go:58`) and forces `slog.NewJSONHandler` for prod, while the researcher ships a
   near-copy of that handler at `rs/telemetry/telemetry.go:135-186` — a copy that lost the
   colour-code comments (`be/server/log_handler.go:34-40`). Key naming is also not shared:
   `be/main.go:78-89` uses PascalCase keys where everything else is lowercase, and
   `rs/tool/stop_anchor.go:113-114` invents `requested_lat`/`stop_lat` with no coordinate key
   convention anywhere.

**Recommendation:** the `"err"` → `"error"` sweep and the `auth.go` slog conversion are both
mechanical, cheap, and together fix the two that actually cost you during an incident.

### (f) Naming drift for the same domain concept

Six concepts carry more than one name. The first three are the ones that would confuse a new
reader.

**1. stop / waypoint / passage point.** `model.Stop` (`be/model/models.go:68`) is the single
persisted entity, and its own doc comment concedes the ambiguity: *"Stop represents a specific
stop or waypoint within a voyage"* (`:67`). It carries a `StopType` discriminator of
`"landfall"` or `"passage_point"` (`:61-64`, `:72`). So a passage point **is** a Stop — but
the handler layer names the concept a third way (`InterpolatePassagePoints`,
`researchPassagePoints`, `DeletePassagePoints` at `be/store/stops.go:79`), and `waypoint`
simultaneously means something *different* in `be/gpx/` — a GPX `<wpt>` element
(`be/gpx/types.go:14, 26`), which `parser.go:94-96` may assemble into a route named
`"Waypoints Route"`. `lookout.go`'s `extractWaypointsSummary` uses the handler sense, not the
GPX sense. One word, two incompatible meanings, in one codebase.

**2. leg / passage / segment.** `gpx.LegResult` (`be/gpx/split.go:11`) is produced by
`SplitTrackByStops`; `extractLegNumber` (`tracks.go:476`) parses "Leg N" out of a track name;
but the godoc one function away calls the same thing a passage — *"points to the starting stop
of its passage leg"* (`tracks.go:573`) — and `segment` is taken by the GPX `<trkseg>` element
(`be/gpx/filter.go`, `types.go`). Test fixtures show the confusion has already leaked into
data: `"Passage Leg"`, `"Leg 1 Passage"`, `"Passage Cove to Harbor"`
(`tracks_test.go:303, 623, 643`).

**3. track / route.** In GPX these are distinct elements, `<trk>` and `<rte>`, and `be/gpx/`
correctly keeps them apart. But `ParseGPX` flattens both into `ParsedTrack` with a
`Kind` of planned-or-recorded (`parser.go:33-34, 59`), so above the parser "route" means
"planned track" — except at `parser.go:96`, where waypoints are assembled into something
named a Route and typed as a planned Track. `model.TrackKind` exists to carry this
distinction (`models.go:256`) and is then never used as a type: every call site writes
`string(model.TrackKindPlanned)`.

**4. facility / place.** `handlers.Facility` (`be/server/handlers/research.go:31`) is the
persisted shape inside `Briefing.Facilities`; the researcher calls the same concept
`PlaceResult` (`rs/tool/places.go`) and `PlaceDetail` (`rs/tool/adapters.go`); the agent that
produces them is named `facility_discovery`. Three names across one data flow.

**5. Lon / Lng.** Within the single `be/gpx` package: `Lon` on every XML type
(`types.go:29, 47, 69, 80`; `filter.go:47`) and `Lng` on the domain `Point`. The two haversine
call sites in `filter.go` read `pts[...].Lon` (`:153`) and `points[...].Lng` (`:250`), 97
lines apart.

**6. Result / Response.** In `rs/tool/`, `FilterGPXDataResult` returns a `*FilterGPXResponse`
and `CalculateHeadingResult` returns a `*HeadingResponse`; across the MCP boundary the pairing
is `TideResult`↔`McpTideResponse` and `PlacesResponse`↔`McpPlacesResponse` but
`SailingDirectionsResult`↔`SailingDirectionsResponse` with no prefix. The `Mcp` prefix marks
2 of the 7 MCP types.

**Recommendation:** pick one word per concept and write it down — a short glossary in
AGENTS.md would do. The highest-value single rename is `Lon`/`Lng`, because it is inside one
package and every conversion site currently has to remember which side it is on.

---

## 7. Nitpicks

- `be/server/handlers/handlers.go:283`, `:293` — `start := -1` / `end := -1` overwritten unconditionally by the if/else immediately following. (Worker item 3 cited `:280`/`:287`; the assignments are at `:283`/`:293`.)
- `be/server/handlers/tracks.go:253-256` — `!voyage.IsPublic` re-checked after `GetVoyageByToken`, whose query already has `AND is_public = true` (`be/store/voyages.go:77`).
- `be/server/handlers/tracks.go:447-448` — `plannedRouteMatch.voyageStopID` and `startStopID` hold the same value under two names.
- `be/server/handlers/admin.go:118, 119, 246` — `map[string]interface{}` where the rest of the package uses `map[string]any`.
- `be/server/handlers/admin.go:156-157, 274-275` — literal JSON written with `w.Write` and no `Content-Type`, while `:117` and `:227` set it; `RevokeInvitation` (`:235-243`) returns a bare 200 where its siblings return `{"message": …}`.
- `be/server/handlers/progress.go:61, 74, 81` — three `rc.Flush()` calls, two styles (`_ =` once, bare twice), one function.
- `be/server/handlers/guide.go:54` — `r.ParseMultipartForm(10 << 20)` return discarded, so a malformed body surfaces as the less accurate "Failed to retrieve image" at `:58`.
- `be/server/handlers/weather.go:43` — `daysOut <= forecastWindowDays` also catches negative `daysOut`, routing past stops to the forecast endpoint.
- `be/server/handlers/weather.go:236` vs `:276` — same two counters, two declaration shapes, adjacent functions.
- `be/server/handlers/passages.go:81` defaults passage points to `SearchRadius = 60` while `stops.go:112` defaults stops to `20`; the 3× difference is unexplained.
- `be/server/handlers/passages.go:153` — `isWaterPoint` is an `is…` predicate that performs a blocking Geocoding API call.
- `be/server/handlers/passages.go:185-186` — `case "ZERO_RESULTS": return true` encodes the central heuristic of the file (the Geocoding API returning nothing means open water) with no comment. The single most surprising line in `passages.go`.
- `be/server/handlers/passages.go:111`, `:131` — `waterProbeRadiiNM = {3,6,10,16,24}` and the ±90° perpendicular probe directions, both unexplained.
- `be/server/handlers/passages.go:201-213` — `initialBearing` cites a formula via Greek identifiers (`φ1`, `Δλ`) without naming it; `offsetPoint`'s `distNM / 60.0` and `/ math.Cos(lat)` are standard nautical identities, invisible to a non-navigator.
- `be/server/handlers/discovery.go:157` — `centerDistDeg < 8.0` unexplained, while its sibling `3.5` at `:152` *is* documented (~210 NM).
- `be/server/handlers/discovery.go:262-265`, `:402-408` — local struct types declared inside 100+ line function bodies, far from their use.
- `be/server/handlers/research.go:22` — `AgentOutput` is specifically the harbourmaster stop-briefing payload; there are five agents.
- `be/server/handlers/research.go:23` — `AgentOutput.LocationName` never read.
- `be/server/handlers/discovery.go:81`, `:210` — `DiscoveryMining`/`DiscoveryPruning` are gerund nouns against a `Trigger*` verb convention everywhere else.
- `be/server/handlers/discovery.go:121` — `isSpatialDuplicate` vs `isRegionDuplicate` give no hint which is the sub-predicate.
- `be/server/handlers/recommendation.go:30` — `recommendationCount` computes a *target* to request, not a count.
- `be/server/handlers/lookout.go:730` — `lookoutHaversineNM` is prefixed with its own file's subject.
- `be/server/handlers/handlers.go:192` buffer `10`, `:225`/`:235` the same `10*time.Second` literal twice, `progress.go:19` buffer `30`, `recommendation.go:176` buffer `100`, `scheduled.go:19` `ListStopsInWindow(ctx, 2)` — unexplained magic.
- `be/server/handlers/context.go:18, 23, 32` — three godoc lines restating the signature. (`:37-38` is useful; keep it.)
- `be/server/handlers/tracks.go:77, 91, 705, 811`; `be/gpx/parser.go:33, 59, 157, 163`; `be/gpx/filter.go:66`; `be/gpx/split.go:22`; `be/config/config.go:216` — comments restating the line below them. The numbered strategy comments at `lookout.go:403-445` and `tracks.go:737-783`, and `parser.go:94`, are the opposite and should stay.
- `be/model/models.go:216, 224, 230, 238` — four godoc lines on `MarshalJSON`/`UnmarshalJSON`/`Value`/`Scan` carrying no information beyond the method name.
- `be/main.go:99, 106, 112, 115` — a numbered comment sequence with no step 1 and a step 3.5, each restating the next line.
- `be/server/routes.go:20` — `for _, route := range r` shadows the package type `route` declared at `:12`.
- `be/server/routes.go:49-50` — `/healthz` and `/health` both bound to `s.healthCheck`.
- `be/server/middleware.go:198` — `responseWriter` gives no hint it exists to capture a status code; `statusRecorder` would.
- `be/server/middleware.go` — six of twelve middlewares carry a `Middleware` suffix and six do not, inside a file named `middleware.go`.
- `be/gpx/filter.go:327-334` — six time layouts, two of them (`"2006-01-02T15:04:05Z07:00"` and `time.RFC3339`) identical, so one is unreachable.
- `be/gpx/types.go:22, 23, 33, 40, 51, 74` — five struct fields unmarshalled and never read, in a hierarchy that is never re-marshalled.
- `be/gpx/parser.go:13-17, 270, 346, 362`; `simplify.go:6, 10`; `filter.go:222`; `types.go:103, 110` — the `gpx` export surface is roughly 3× wider than its external consumers need; only `ParseGPX`, `FilterGPX`, `SplitTrackByStops`, `CalculateHaversineNM`, `ParsedTrack` and `LegResult` are used outside the package.
- `be/gpx/split.go:48-49` — the godoc's "or closest approach" clause does not match the code, which requires both best-match **and** `<= 5.0`.
- `be/store/briefings.go:46` — `tolerance` names neither unit nor axis; it is a half-width in decimal degrees.
- `be/store/db.go:16`, `be/config/config.go:47` — `store.New` returns a `*DB`, `config.New` a `*Config`; `main.go` reads as a column of bare `New` calls.
- `be/agent/agent.go:209-217, 257-263` and `be/agent/reasoning_engine.go:91-99` — three copies of "if role is empty or model, append every non-thought part's text".
- `rs/tool/sailing_directions.go:383-389` — hand-rolled all-digits rune walk where `strconv.Atoi` would do.
- `rs/tool/tides.go:18, 20` — `DefaultSearchRadius = 50` and `MaxStationsToCheck = 5` carry no unit and no doc, while their sibling `MaxSearchRadius` (`:19`) does.
- `rs/tool/tides.go:93-95, 519-521, 525-527` — the "no local station" literal written three times.
- `rs/tool/tides.go:189-194, 373-378` — UK and NZ service-coverage bounding boxes as raw magic numbers.
- `rs/tool/tides.go:279` — `ukStationDist.dist` is unit-free; the value is miles (`:292`).
- `rs/tool/tides.go:397, 399` — `NumberOfDays: 3` and `Interval: 10`; the 10-minute sampling exists specifically to feed `detectHighLow`, which is nowhere stated.
- `rs/tool/adapters.go:135, 166, 216, 247, 323, 346` — the `if s.X == nil { return nil, fmt.Errorf("… service unavailable") }` guard repeated six times.
- `rs/tool/gpx_filter.go:104, 108` — `internalPoint.Ele` and `.Name` are write-only.
- `rs/tool/gpx_filter.go:112` — `SourceDoc` holds `"trk"`/`"rte"`/`"wpt"`/`"input"`; those are source *elements*, not documents.
- `rs/tool/safety_feed.go:209` — `io.LimitReader(resp.Body, 5<<20)` with no comment.
- `rs/tool/weather.go:92` — `daysUntil >= -1 && daysUntil <= 16`; the `16` is explained at `:91`, the `-1` is not.
- `rs/tool/weather.go:96-113` — `errgroup.WithContext` used purely as a `WaitGroup`: both closures `return nil` unconditionally and errors are hoisted into captured vars. No data race, just misleading.
- `rs/tool/search.go:97, 131` — `Run` and `Execute` are two public entry points to the same work; neither name says which is the interface method.
- `rs/tool/search.go:149-151` — the error is stringified into the results map as `"Error: " + err.Error()` and handed to the LLM; never logged.
- `rs/tool/places.go:156-157, 170`; `rs/tool/search.go:103, 118` — comments restating the line below.
- `rs/tool/sailing_directions.go:169, 217, 353, 475` — four user-facing strings inlined as literals inside the HTTP path; hoist to consts.
- `rs/mcp/mcp.go:17-176` uses `interface{}` throughout while `rs/server/reasoning_engine.go:18-30` uses `any`, same service, same Go version.
- `rs/config/config.go:1` and `rs/mcp/mcp.go:1` have no package doc, unlike `agent`, `server`, `session` and `telemetry`.
- `rs/server/reasoning_engine.go:59` — `lookupString(fallback string, maps []…)` puts the default ahead of the data it searches; reads backwards at all three call sites.
- `rs/telemetry/telemetry.go:390-428` — `ResolveProjectID` is exported with no caller outside its own file.
- `rs/telemetry/telemetry.go` is three files in 532 lines: slog handler (`:32-186`), SSE broadcaster and tool tracker (`:188-359`), tracing init (`:361-532`).
- `rs/agent/agent.go` — temperatures are unexplained but internally consistent (0.25 research, 0.1 precision, 0.4 search); one comment on the `spec.temperature` field would make the convention legible. `agent.go:28-31` is the bar — it explains the 8192-token truncation incident that motivated `maxOutputTokens`.
- `rs/config/config.go:93, 100`; `rs/main.go:57, 94-97`; `rs/server/server.go:73` — uncommented tuning constants (thinking budget 1024, search timeout 30000, init 30 s, shutdown 5 min prod / 5 s dev, SSE write 600 s). The prod shutdown value is sized for long agent runs and is worth saying out loud.
- `rs/main.go:23-25` — three `godotenv.Load` calls with ignored errors and no comment explaining the walk up to repo root.
- `sync/main.go:21, 32` — `Config` and `NGAStoredPub` exported from `package main`.
- `be/server/server.go:16, 28`; `be/main.go:24`; `be/server/handlers/handlers.go:108, 133` — godoc restating the signature.
- `be/main.go:98` — a single blank line immediately after the `func run(` opening brace.
- Unchecked `json.NewEncoder(w).Encode(...)` at roughly 30 sites across both modules, plus `w.Write` at `rs/server/server.go:67`. Consistently ignored, so not a divergence — noted only because a write failure after `WriteHeader` is currently invisible.
- `.gitignore:49` covers `code/app/backend/.gotmp/*`; the seven stale `go-build*` directories there are untracked and invisible to git. Wasted disk, not a repo-hygiene problem. The `dir/*` form matches each entry where `dir/` would prune the walk, but the sibling entries at `:46-48` use the same form, so it is consistent rather than an oversight.
