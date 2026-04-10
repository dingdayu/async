# Roadmap

This document tracks the recommended development order for the current `v5` line.

It intentionally focuses on **stabilizing the runtime model that already exists** rather than expanding the scheduler into a much larger system too early.

For scheduling boundaries and long-term non-goals, see [FUTURE_SCHEDULING.md](FUTURE_SCHEDULING.md).

## Current status

The current branch has already established the main runtime foundation:

- explicit `Runtime` lifecycle (`Start`, `Run`, `Shutdown`, `Wait`)
- explicit `Service` / `Job` split
- pooled job execution with bounded queues and queue-full policies
- partitioned job execution
- priority-aware heap-backed local queues
- middleware / wrapper extension points
- observer and stats-based observability hooks

That means the next phase should emphasize **semantic stability, official extensions, observability completeness, and engineering discipline**.

## Guiding principles

1. Keep the **core runtime layer** small and lifecycle-focused.
2. Keep scheduling and backpressure in the **control layer**.
3. Prefer middleware, observers, and adapters in the **extension layer** over pushing more policy into the scheduler core.
4. Do not expand into a heavier scheduler unless benchmarks and real workloads justify it.

---

## Phase A - Stabilize public semantics

**Priority: highest**

The next important work is to lock down behavior contracts that are now exposed publicly.

### Goals

- make `Stats` field meanings explicit and stable
- document middleware execution order clearly
- document runtime middleware vs task middleware composition clearly
- document observer behavior clearly:
  - sync vs async behavior
  - panic isolation
  - expectation that callbacks remain lightweight
- document queue full policy behavior in terms of events, stats, and drop semantics
- keep English and Chinese docs aligned where public semantics matter

### Deliverables

- README contract clarifications
- migration guide updates if semantics affect upgrading users
- focused tests for newly documented behavior

---

## Phase B - Add official extension building blocks

**Priority: high**

The runtime now has extension hooks. The next step is to provide a small set of official, production-oriented building blocks on top of them.

### Recommended additions

- `Recover` middleware
- `Timeout` middleware
- `Retry` middleware
- structured `Logging` middleware
- `Metrics` adapter/middleware
- `Tracing` adapter/middleware

### Rules

- keep these outside the scheduler hot path as much as possible
- avoid stuffing feature-specific policy into `Runtime`
- prefer small reusable helpers over large framework-style abstractions

---

## Phase C - Complete the observability loop

**Priority: high**

Observers and point-in-time stats exist, but production integrations usually need richer measurement surfaces.

### Recommended additions

- counters for:
  - submitted
  - queued
  - started
  - finished
  - failed
  - dropped
- queue wait duration
- execution duration
- partition saturation / pressure indicators
- metrics-friendly adapters for Prometheus / OpenTelemetry-style exporters

### Design preference

- expose integration points first
- avoid hard-wiring the runtime to one metrics backend

---

## Phase D - Establish engineering quality baselines

**Priority: high**

Now that the project touches runtime hot paths directly, correctness alone is not enough.

### Must-have discipline

- `go test ./...`
- `golangci-lint run`
- `go test ./... -run '^$' -bench 'BenchmarkRuntime' -benchmem`

### Recommended follow-ups

- publish benchmark baselines for key runtime scenarios
- call out regression thresholds for hot paths
- ensure CI protects tests, lint, and benchmark-sensitive refactors

---

## Phase E - Refine and narrow the public API

**Priority: medium**

The next API work should be about **clarity and restraint**, not feature volume.

### Recommended work

- review naming consistency across task, runtime, observer, and stats APIs
- distinguish stable public contract from implementation detail in docs
- reduce ambiguity before introducing any larger control APIs

### Do not rush yet

- task handles
- per-task cancellation APIs
- large public state-machine APIs
- heavy management-plane features

Those features would move the library toward a task management system rather than a focused runtime.

---

## Phase F - Revisit advanced scheduling only with evidence

**Priority: low / evidence-driven**

Advanced scheduling should remain deferred until benchmarks and real workloads clearly justify it.

### Only revisit if evidence shows

- repeated throughput loss relative to simpler baselines
- persistent partition imbalance that tuning cannot solve
- materially worse tail queue delay under realistic workloads

### If revisited, prefer the smallest safe step

- limited, opt-in borrowing between explicitly shareable partitions

See [FUTURE_SCHEDULING.md](FUTURE_SCHEDULING.md) for the current boundary and the risks of going further.

---

## What should not become near-term roadmap items

These should stay out of the active near-term plan unless project requirements change materially:

- runtime reboot / restart semantics
- collapsing `Service` and `Job` into one ambiguous abstraction
- global fairness scheduling
- general work stealing across partitions
- dynamic partition rebalancing
- hiding task failures behind fire-and-forget APIs

## Recommended order of execution

1. **Phase A** - semantic stability
2. **Phase B** - official extension building blocks
3. **Phase C** - richer observability
4. **Phase D** - lint / benchmark / CI discipline
5. **Phase E** - API narrowing and refinement
6. **Phase F** - only if benchmark evidence demands it
