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

## Current Conclusion

Short version:

- `dae-wing` is the next best optimization target after recent `dae-core` DNS/UDP stabilization.
- `graphql/service/node/latency_cache.go` is the strongest candidate for meaningful improvement.

## Next Step

Recommended immediate next step:

- audit and tighten `graphql/service/node/latency_cache.go`

Questions to answer:

- Does the cache retain obsolete node entries?
- Can the cache be rebuilt atomically instead of patched incrementally?
- Can runtime results and persisted results be merged with less copying?
