# openmeteogo: distinguish JSON null from 0.0 in marine fields

> Scratch spec for work in `github.com/tpryan/openmeteogo`. Not a NavalPlan doc —
> move it to that repo or delete it once the change lands.

## Problem

Open-Meteo returns `null`, not an error, for a metric it cannot supply at the
requested position. The marine wave model is a global *ocean* model, so any
point outside its water mask — a marina well up an estuary, for example —
answers HTTP 200 with `null` for every hour.

`openmeteogo` decodes these into `[]float64`. Go unmarshals JSON `null` into
`float64` as **0**. The slice still has full length, so a caller checking
`len(resp.Daily.WaveHeightMax) > 0` sees a populated response and reads zeros as
measurements.

Observed in NavalPlan: Benicia Marina (38.0447, -122.1577), roughly 25 nm up the
Carquinez Strait, stored `wave_height_ft 0`, `wave_period 0`, and 48 hourly
points of zero. It rendered as a dead-flat calm. The same voyage's bay stops
returned real values, so the failure looked like a display bug rather than a
decode bug.

This is a silent correctness problem in a safety-adjacent path: downstream
threshold checks read 0 ft and conclude the sea state is safe.

## Change

Switch the marine and ocean slices from `[]float64` to `[]*float64`, so `nil`
means "no value at this position/hour" and `0` means a real zero reading.

`[]*float64` over a custom `NullFloat` type: no new vocabulary for callers, and
`if v != nil` is the obvious idiom.

### `type Hourly struct` (around line 355)

| Field | Current | New |
|---|---|---|
| `WaveHeight`, `WaveDirection`, `WavePeriod`, `WavePeakPeriod` | `[]float64` | `[]*float64` |
| `WindWaveHeight`, `WindWaveDirection`, `WindWavePeriod`, `WindWavePeakPeriod` | `[]float64` | `[]*float64` |
| `SwellWaveHeight`, `SwellWaveDirection`, `SwellWavePeriod`, `SwellWavePeakPeriod` | `[]float64` | `[]*float64` |
| `SecondarySwellWaveHeight`, `SecondarySwellWaveDirection`, `SecondarySwellWavePeriod` | `[]float64` | `[]*float64` |
| `TertiarySwellWaveHeight`, `TertiarySwellWaveDirection`, `TertiarySwellWavePeriod` | `[]float64` | `[]*float64` |
| `SeaLevelHeight`, `SeaSurfaceTemperature` | `[]float64` | `[]*float64` |
| `OceanCurrentVelocity`, `OceanCurrentDirection` | `[]float64` | `[]*float64` |

### `type Daily struct` (around line 461)

| Field | Current | New |
|---|---|---|
| `WaveHeightMax`, `WaveDirectionDominant`, `WavePeriodMax` | `[]float64` | `[]*float64` |
| `WindWaveHeightMax`, `WindWaveDirectionDominant`, `WindWavePeriodMax`, `WindWavePeakPeriodMax` | `[]float64` | `[]*float64` |
| `SwellWaveHeightMax`, `SwellWaveDirectionDominant`, `SwellWavePeriodMax`, `SwellWavePeakPeriodMax` | `[]float64` | `[]*float64` |

Confirm nothing was missed before starting:

```
grep -n '\[\]float64' openmeteo.go | grep -i 'wave\|swell\|sea_\|ocean'
```

## Scope: marine only

Every numeric metric in the API can come back null, so the bug class is general.
Converting all of them would touch most of the library's surface for a failure
mode that is rare on land — the terrestrial models have global coverage, and a
null there means an outage rather than routine out-of-domain.

Marine is where out-of-domain is **normal and expected**, so that is where the
distinction earns its breaking change. Leave the terrestrial fields alone.

## Helpers to add

Export these so callers do not each reinvent them:

```go
// Value returns the first non-nil entry and whether one existed.
func Value(vals []*float64) (float64, bool)

// HasAny reports whether any entry is non-nil. False across a marine series
// means the position lies outside the wave model's domain.
func HasAny(vals []*float64) bool

// Floats flattens a pointer slice, substituting zero for nil entries. Only safe
// once HasAny is true, where nils are gaps inside a covered series rather than
// a wholesale absence of data.
func Floats(vals []*float64) []float64
```

## Tests

1. All-null marine response → `HasAny` false, every entry nil. Guards the exact
   Benicia case.
2. Mixed response (`[1.2, null, 0.9]`) → length 3, middle entry nil, others set.
3. **Real zero** (`[0.0, 0.0]`) → entries non-nil pointing at 0. This is the
   assertion that distinguishes calm from absent; without it the change is
   pointless.
4. Existing marine decode tests still pass with pointer dereferences.

## Release

Breaking for anyone reading marine fields. Pre-1.0: bump the minor and note it.
At v1+: this is a v2 module path.

Changelog should name the symptom, not just the type change — "zeros were
indistinguishable from missing data in marine responses" is what a user will
search for.

## NavalPlan follow-up

NavalPlan currently works around this with its own marine client at
`code/app/backend/internal/server/handlers/marine.go`, which does the same
pointer decode locally. Once this ships:

1. Bump the dependency, re-vendor.
2. Delete `marine.go`.
3. Point `fetchWeatherForDay` back at `client.Get` with `Marine(true)`, using
   `HasAny` for the coverage check that currently lives in `fetchMarineWaves`.
4. Keep `model.WeatherSummary.WaveDataUnavailable` and the `internal/safety`
   guards as they are — they consume the distinction and do not care where it
   comes from.
