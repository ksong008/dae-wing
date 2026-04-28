# Three-Layer Refactor

## Target Topology

The runtime topology stays:

`daed -> dae-wing -> dae`

- `daed`: UI only. It should speak REST/OpenAPI and SSE, and stop embedding dae-specific parsing rules.
- `dae-wing`: control plane only. It should own auth, persistence, orchestration, and API transport.
- `dae`: data plane only. It should own config normalization, plan building, runtime stats, health, and eBPF lifecycle.

## Why Keep Three Layers

- It preserves the current product shape and keeps web, orchestration, and proxy concerns separate.
- It lets `dae-wing` evolve from GraphQL to REST/OpenAPI + SSE without forcing a full `dae` rewrite first.
- It avoids moving UI and persistence logic into the proxy engine.

## Current Problem

Today `dae-wing` reaches into `dae` through many scattered package calls:

- config parsing
- routing dependency expansion
- runtime stats
- control plane access
- reload and shutdown channels

That makes protocol migration and memory optimization harder, because the boundary is not explicit.

## Phase 0 Boundary

This branch introduces a new `engine` facade in `dae-wing`:

```text
dae-wing/
├── engine/
│   ├── engine.go
│   └── engine_test.go
├── cmd/
└── dae/              # current implementation detail
```

The intent is:

- `dae-wing/engine`: stable control-plane boundary
- `dae-wing/dae`: legacy implementation adapter for the current global `dae` runtime

## Target `dae-wing` Layout

After migration, `dae-wing` should move toward:

```text
dae-wing/
├── cmd/
├── engine/
│   ├── service.go
│   ├── runtime.go
│   ├── compiler.go
│   └── native/
├── store/
│   ├── sqlite/
│   └── model/
├── orchestrator/
│   ├── config_run.go
│   ├── latency.go
│   └── subscription.go
├── transport/
│   ├── httpapi/
│   └── auth/
└── web/
```

Responsibilities:

- `engine`: the only package allowed to talk to the dae runtime
- `store`: persistence only
- `orchestrator`: resource assembly and run/reload workflows
- `transport/httpapi`: REST, OpenAPI, and SSE control-plane APIs

## Target `dae` Layout

`dae` should keep its current domain split, but expose an instance-style engine instead of package globals:

```text
dae/
├── config/
├── component/
├── control/
├── engine/
│   ├── engine.go
│   ├── runtime.go
│   └── reload.go
└── cmd/
```

The key change is not a directory rename. The key change is replacing global runtime state with an `Engine` instance that `dae-wing` can hold directly.

## Memory Goals

The main expected wins are:

- fewer repeated config parses in request paths
- fewer duplicate config models in memory
- a single orchestration boundary for reload state
- a smaller transport surface after moving to REST/OpenAPI + SSE

The biggest memory spike still comes from eBPF object loading and route/materialization, so transport choice alone is not the main savings source.

## Migration Order

1. Introduce the `engine` facade in `dae-wing` and route new code through it.
2. Move runtime-sensitive transport paths to the facade.
3. Add REST/OpenAPI + SSE transport.
4. Retire GraphQL.
5. Move `dae` from package globals to an instance-style engine.

## Local Validation Strategy

This branch uses a dry-run validation target first:

- boot the engine facade in dry mode
- reload an empty config through the facade
- read runtime overview through the facade
- stop the engine facade cleanly

That proves the new control-plane boundary can own lifecycle calls before touching kernel mode.
