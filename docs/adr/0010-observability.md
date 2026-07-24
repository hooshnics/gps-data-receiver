# 0010 — Observability

## Status
Accepted

## Context
Zero-drop systems fail silently without metrics on ACK latency, pending lag, DLQ growth, and duplicates.

## Decision
Prometheus metrics (existing + Teltonika extensions), scraped at `/metrics`:
- packets received / acked / CRC failures / ingest errors
- Redis write latency, parser latency
- duplicates, decode failures, dead-letter count
- raw / retry / DLQ depths, raw pending count
- existing HTTP/queue/sender/worker metrics

Structured zap logs include IMEI, redis id, frame size, attempt where relevant — not full payloads.

## Alternatives Considered
- Logs only — rejected
- OpenTelemetry traces everywhere — deferred (cost)

## Consequences
**Positive:** Actionable SLOs for ACK path and consumer lag.  
**Negative:** Metric cardinality kept low (no per-IMEI labels on hot counters).

## References
- `internal/metrics/metrics.go`
- `cmd/server/main.go` depth reporter
- ADR 0011
