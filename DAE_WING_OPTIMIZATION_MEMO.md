# dae-wing Optimization Memo

Date: 2026-04-19
Branch: `personal/stable`

## Goal

This memo records optimization investigation and changes for `dae-wing`.

Primary objectives:

- stable
- fast
- avoid long-running memory growth

## Why `dae-wing` Is Worth Optimizing

`dae-wing` sits directly between:

- `dae-core`
- `daed`

It is not just a thin API shim. It already owns:

- GraphQL aggregation
- DB-backed query composition
- runtime snapshot adaptation
- in-process caches
- control-plane orchestration and reload glue

That means memory growth or sluggishness observed while “running daed” may originate in:

- frontend (`daed`)
- backend wrapper (`dae-wing`)
- core (`dae`)

`dae-wing` is a realistic middle suspect and therefore deserves a dedicated review.

## Current Dependency Chain

The current intended chain is:

- `daed`
  - submodule `wing`
- `dae-wing`
  - submodule `dae-core`
- `dae`

So compatibility and performance depend not only on code paths, but also on:

- submodule pinning
- whether `dae-wing` is using the expected `dae-core`

## High-Priority Review Areas

### 1. Node latency cache

File:

- `graphql/service/node/latency_cache.go`

Why it matters:

- global process-wide cache
- mixes DB results and runtime snapshots
- has refresh throttling
- may retain stale node entries indefinitely

Current observations:

- cache is append/overwrite oriented
- no explicit eviction path
- no cleanup when nodes disappear
- no upper bound

Main risks:

- stale cache entries
- process memory growth over time
- repeated cloning / copying on query path

### 2. Runtime snapshot adaptation

File:

- `dae/runtime.go`

Why it matters:

- converts `dae-core` runtime snapshots into `dae-wing` GraphQL-facing structures
- this can amplify allocations if queried frequently

Main risks:

- repeated slice copying
- avoidable allocations on polling-heavy dashboards

### 3. Runtime GraphQL resolver hot path

File:

- `graphql/service/general/runtime_resolver.go`

Why it matters:

- directly serves frontend polling traffic
- may be executed frequently in dashboard mode

Main risks:

- request-path allocations
- no memoization between near-identical queries

### 4. Reload / control-plane glue

Files:

- `dae/run.go`
- `cmd/run.go`

Why it matters:

- this is where `dae-wing` manages `dae-core`
- stale references / cached data across reloads can create subtle memory or correctness issues

Main risks:

- stale cached state after reload
- overly coupled logger / channel / callback lifecycle

## Suggested Optimization Order

1. `graphql/service/node/latency_cache.go`
2. `dae/runtime.go`
3. `graphql/service/general/runtime_resolver.go`
4. `dae/run.go`

## Optimization Principles

- prefer explicit cache lifecycle over opportunistic retention
- prefer replacing cache state atomically over growing long-lived maps
- keep GraphQL hot paths cheap and allocation-aware
- keep integration with `dae-core` version-aware and deterministic

## Applied Changes

### A. Initial review baseline

- Verified that `dae-wing` currently pins `dae-core` through local replace/submodule structure.
- Verified that `daed personal/stable` can be updated to include the latest `dae personal/stable` by first bumping `dae-wing`, then bumping `daed`.

### B. `node/latency_cache` tightening

Files:

- `graphql/service/node/latency_cache.go`
- `graphql/service/node/latency_cache_test.go`

Changes:

- Cache refresh now replaces the whole latency cache state atomically instead of incrementally overwriting individual items.
- This prevents stale node entries from accumulating indefinitely after topology changes.
- Query path now snapshots only the requested node IDs instead of cloning the entire latency cache map on every request.
- Runtime latency merge now reuses the already-fetched node list rather than re-querying all matching nodes from DB by link.

Why this helps:

- lower memory retention from obsolete cache entries
- lower per-query allocation cost
- fewer redundant DB operations on the read path

Tests added:

- full cache replacement removes stale entries
- filtered cache snapshot only clones requested node IDs

## Current Conclusion

Short version:

- `dae-wing` is the next best optimization target after recent `dae-core` DNS/UDP stabilization.
- `graphql/service/node/latency_cache.go` is the strongest candidate for meaningful improvement.

### C. Runtime overview hot-path tightening

Files:

- `dae/runtime.go`
- `graphql/service/general/runtime_resolver.go`
- `graphql/service/general/runtime_resolver_test.go`

Changes:

- `dae-wing` now reuses `dae-core`'s runtime snapshot types directly instead of rebuilding the runtime overview sample slice one element at a time.
- `RuntimeOverviewResolver` now builds sample resolvers once up front and keeps them attached to the overview resolver.
- Sample resolvers now point at the existing runtime sample entries instead of copying each sample into resolver structs.

Why this helps:

- removes one full sample-slice copy per runtime overview query
- reduces request-path allocations for polling-heavy dashboard traffic
- keeps the GraphQL hot path thinner without changing external schema shape

Tests added:

- runtime overview resolver caches sample resolvers
- empty runtime overview returns no sample resolvers

### D. Reload path DNS cache retention cleanup

Files:

- `dae/run.go`

Changes:

- Stopped cloning `dae-core` DNS cache during reload in `dae-wing`.
- The current `dae-core` reload path no longer restores that cloned cache back into the new controller/domain-routing state, so retaining it in `dae-wing` provided allocation cost without actual reuse.

Why this helps:

- avoids a deep copy of a potentially large DNS cache map on every reload
- reduces reload-time allocation spikes
- avoids carrying dead reload state that is not actually consumed by the new control plane

### E. Control-plane pointer synchronization

Files:

- `dae/run.go`
- `dae/run_test.go`

Changes:

- Replaced the unsynchronized package-global control-plane pointer access with an atomic pointer wrapper.
- `dae-wing` now stores the freshly swapped control plane atomically during reload.
- Shutdown now clears the exported control-plane pointer before closing the active control plane so new queries do not grab a closing instance.

Why this helps:

- removes a real concurrency race between GraphQL request handlers and reload-time control-plane swaps
- makes runtime queries more stable during reload/shutdown boundaries
- reduces the chance of handing out a stale or closing control-plane pointer to request paths

Tests added:

- control-plane accessor returns `ErrControlPlaneNotInit` when empty
- control-plane accessor returns the atomically stored pointer when present

### F. Reload logger reconfiguration cleanup

Files:

- `dae/run.go`
- `dae/run_test.go`

Changes:

- Removed the reload-time `logrus.New()` replacement and the paired output-reset hack.
- Added an in-place logger reconfiguration helper that updates level/formatter while preserving the existing writer.
- Standard logger reconfiguration is still kept in sync, but without swapping logger instances during reload.

Why this helps:

- avoids swapping logger objects while other goroutines may still hold references
- preserves existing writer state without depending on a manual `SetOutput` reset
- makes reload behavior simpler and less allocation-heavy

Tests added:

- logger reconfiguration preserves the existing output writer and applies the new level

## Next Step

Recommended immediate next step:

- continue auditing `dae/run.go` exit and callback paths

Questions to answer:

- Is reload retaining more state than necessary across control-plane swaps?
- Can logger/channel glue be tightened without affecting reload correctness?
