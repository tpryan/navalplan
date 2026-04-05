# Researcher Service Go Audit — 2026-04-04

## Scope

Full audit of `code/services/researcher/` — the AI agent orchestration microservice.

- `main.go` — server setup, agent wiring, telemetry SSE, middleware (591 lines)
- `tools/weather.go` — Open-Meteo weather forecast tool (233 lines)
- `tools/tides.go` — NOAA tide prediction tool (156 lines)
- `tools/places.go` — Google Maps Places search tool (176 lines)
- `tools/sunrise.go` — Sunrise/sunset + timezone tool (111 lines)
- `config/config.go`, `logging/` — configuration and logging utilities

Approximately 1,200 lines of production code reviewed.

---

## Summary

The service is well-structured with clean interface-based tool providers and good separation between tool logic and agent wiring. The main concerns are: a context propagation bug that silently discards cancellation in `sunrise.go`, a data field that is never populated in `tides.go`, no graceful shutdown handling for Cloud Run's SIGTERM signal, and an unauthenticated internal telemetry endpoint.

---

## Issues by Severity

### High — Correctness

**1. `context.Background()` used instead of tool context in `sunrise.go:86`**

`GetSunriseSunset` receives a `tool.Context` (which embeds `context.Context`) but calls the Google Maps Timezone API with a fresh `context.Background()`:

```go
tzResult, err := sp.client.Timezone(context.Background(), tzReq)
```

This means if the ADK framework cancels the tool invocation (timeout, client disconnect), the timezone HTTP call continues running. It also loses any trace IDs injected upstream.

Fix:
```go
tzResult, err := sp.client.Timezone(ctx, tzReq)
```

---

**2. `TideResult.DistanceMiles` never populated — `tides.go`**

`TideResult` declares a `DistanceMiles float64` field, but `GetTides` never sets it:

```go
return TideResult{
    StationName: s.Name,
    StationID:   s.ID,
    Tides:       tides,
    // DistanceMiles always 0.0
}, nil
```

The NOAA client returns a station with a `DistanceMiles` field. The agent always receives `distance_miles: 0.0`, which removes useful context about how far the tide data is from the requested location.

Fix: populate from the station object when constructing the result.

---

### Medium — Reliability

**3. No graceful shutdown on SIGTERM — `main.go`**

`http.ListenAndServe` runs indefinitely with no signal handler:

```go
return http.ListenAndServe(":"+s.config.Port, ...)
```

Cloud Run sends SIGTERM before a 10-second drain window. In-flight LLM requests (which regularly take 20–60 seconds) will be hard-killed mid-response, producing incomplete or error responses to the backend.

Fix: use `http.Server` with `Shutdown()`, listen for `os.Signal` on SIGTERM/SIGINT, and call `Shutdown()` with a generous drain timeout (e.g. 5 minutes to allow active research to complete).

---

**4. `errgroup` context discarded in `weather.go:90`**

```go
g, _ := errgroup.WithContext(ctx)
```

The derived context (which would be cancelled on goroutine failure) is discarded. The goroutines close over the outer `ctx` instead. `WeatherClient.Get` doesn't accept a context today, so this isn't a current correctness issue — but it means future changes that add context-aware cancellation won't work, and it's misleading.

Fix: capture the derived context: `g, gCtx := errgroup.WithContext(ctx)` and use `gCtx` in goroutines (even if the current client doesn't use it, it's ready for when it does).

---

**5. `context.Background()` in `NewPlacesTool` client creation — `places.go:66`**

```go
c, err := places.NewClient(ctx, clientOpts...)  // ctx is context.Background() here
```

`NewPlacesTool` is called from `setupTools()` which doesn't receive a context. The gRPC client initialization uses an unbound `context.Background()`. If the Google API is unresponsive during startup, this call hangs indefinitely (the 30-second `initCtx` added to `run()` does not cover `setupTools()`).

Fix: pass `initCtx` through `setupTools()` to `NewPlacesTool`, or add a dedicated timeout inside `NewPlacesTool`.

---

### Low — Maintenance / Security

**6. `/telemetry` SSE endpoint is unauthenticated — `main.go`**

The `/telemetry` endpoint streams real-time internal tool execution events (tool names, durations, session IDs) with no authentication check. In production on Cloud Run this is mitigated by IAM, but any authenticated Cloud Run caller can monitor all agent activity.

Fix: add a shared-secret check (`Authorization: Bearer <NAVALPLAN_SYSTEM_KEY>`) consistent with other internal endpoints in the backend, or document that IAM is the intended gate.

---

**7. `main_test.go` — agent creation test has no timeout**

`TestCreateResearcherAgent` uses `context.Background()` with no deadline. If the Gemini API hangs, the test hangs indefinitely with no indication to CI.

Fix: use a 30-second context timeout.

---

**8. `ErrAPIUnavailable` defined but never used — `tools/errors.go`**

`ErrAPIUnavailable` is exported but no tool wraps it. API failures are returned as ad-hoc wrapped errors. This prevents callers from using `errors.Is(err, ErrAPIUnavailable)` to distinguish unavailability from invalid input.

Fix: either use it consistently in `weather.go`, `tides.go`, and `places.go` when external API calls fail, or remove it to avoid misleading documentation.

---

**9. Embedded prompt files not validated at startup — `main.go`**

The `//go:embed` directives embed prompt Markdown files. If a file is accidentally empty (truncated, wrong path), the agent starts with an empty instruction string and no error is logged.

Fix: add a startup check that each prompt string has non-zero length, failing fast rather than running a prompt-less agent.

---

**10. Tide station search radius upper bound is large — `tides.go:95`**

The station search expands from 50 to 500 miles (10× `DefaultSearchRadius`). A tide station 400+ miles away provides essentially useless local tides data. The fallback is silent: the agent receives results without knowing the station is 400 miles from the requested point.

Fix: cap the search at ~150 miles and return `ErrNotFound` beyond that, so the agent can explicitly tell the user no nearby station was found rather than returning distant data.

---

## What Was NOT Found

- No race conditions in the `timings` map or telemetry `clients` map — both are correctly protected by their respective mutexes
- No SQL injection risks (no database in this service)
- No hardcoded API keys or secrets — all read from environment
- The graceful degradation in `weather.go` (marine data is optional, weather data is required) is correct
- The `findNearbyStations` expanding-radius retry logic is reasonable
- Interface-based tool providers (`WeatherClient`, `TideClient`, etc.) are well-designed and make unit testing straightforward

---

## Recommended Priority Order

1. **High:** Fix `context.Background()` in `sunrise.go` — use tool context
2. **High:** Populate `TideResult.DistanceMiles` from station data
3. **Medium:** Add graceful SIGTERM shutdown
4. **Medium:** Thread `initCtx` through `setupTools()` to `NewPlacesTool`
5. **Medium:** Capture errgroup context in `weather.go`
6. **Low:** Validate embedded prompt strings at startup
7. **Low:** Add test timeout to `main_test.go`
8. **Low:** Decide on `ErrAPIUnavailable` — use or remove
9. **Low:** Cap tide station search radius at ~150 miles
10. **Low:** Document or enforce authentication on `/telemetry`

---

**Total issues found:** 10 (2 high, 3 medium, 5 low)
**Files reviewed:** 10 Go source files
