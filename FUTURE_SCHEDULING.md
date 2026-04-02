# Future scheduling directions for v5+

The current v5 runtime stops at:

- explicit `Runtime` lifecycle
- `Service` / `Job` split
- pooled short jobs
- bounded job queues
- queue-full policies
- priority-aware local queues
- partitioned job execution

This is the intentional boundary for the current branch.

## Why the current branch stops here

The runtime now gives users three important guarantees:

1. explicit ownership of lifecycle and shutdown
2. predictable local queue semantics inside each partition
3. isolation between job partitions

Adding global fairness or work stealing would weaken those guarantees unless the runtime becomes a more sophisticated scheduler.

## Benchmark signals that would justify a deeper scheduler

The current implementation should only move beyond local partition queues if benchmarks show a real and repeated capacity problem in realistic workloads, such as:

- meaningful throughput loss versus a comparable shared-pool baseline
- much worse queue wait latency for one partition while other partitions stay idle
- repeated evidence of stranded capacity after worker-count and queue-cap tuning

As a practical threshold, only revisit scheduling if realistic workloads still show roughly 15-25%+ throughput loss or 2x+ tail queue delay compared with a simpler shared-pool baseline.

## Recommended next step if deeper scheduling is needed

The smallest safe next step is **limited, opt-in borrowing**:

- an idle worker may borrow a single job from an explicitly shareable partition
- borrowing should be default-off
- the source partition should keep its own priority/FIFO ordering
- lifecycle, fail-fast, and shutdown must remain global

This is safer than general work stealing because it keeps sharing explicit and narrow.

## What should stay deferred

These should remain out of the current v5 line:

- general work stealing across all partitions
- global fairness scheduling
- soft quotas backed by a global scheduler
- dynamic partition rebalancing
- worker stealing that ignores partition-local queue policy

## Risks to watch if borrowing is explored later

- weakening critical-vs-batch isolation silently
- breaking drop-oldest / drop-newest semantics under cross-partition movement
- hidden contention from expanding the current single-runtime lock model
- muddy semantics when partition-local priority meets global fairness

## Recommended benchmark focus before revisiting this document

- submission throughput under balanced and skewed partition loads
- queue wait latency per partition
- p95/p99 job start latency under mixed short and long jobs
- percentage of time a partition has queued work while another partition is idle

If those numbers remain healthy, the runtime should stay with the current local-partition design.
