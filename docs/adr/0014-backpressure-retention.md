# 0014 — Backpressure & retention

## Status
Accepted

## Context
Streams must not grow unboundedly, but trimming must never delete unprocessed PEL entries carelessly.

## Decision
- After successful process: `XACK` + `XDel` (primary retention for raw/reports)
- `gps:reports` may use approximate `MAXLEN` (`REDIS_MAX_LEN`)
- `gps:raw_incoming` default `REDIS_RAW_MAX_LEN=0` (unlimited) — prefer consumer catch-up over trim
- Retry/DLQ approximate `MAXLEN` ~100k as safety rail
- HTTP ingest backpressure via queue depth limit
- Redis `noeviction` so memory pressure surfaces as errors (withhold ACK) instead of silent key loss

## Alternatives Considered
- Aggressive MAXLEN on raw — rejected (can trim unread under lag)
- Disk spill as primary backpressure — rejected

## Consequences
**Positive:** Explicit failure under pressure beats silent loss.  
**Negative:** Ops must alert on Redis memory and stream depths.

## References
- `internal/queue/durable.go`, `redis_queue.go`
- ADR 0011
