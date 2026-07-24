# 0005 — Consumer groups & pending recovery

## Status
Accepted

## Context
Workers crash or deploy mid-message. Entries must not stay forever in PEL (Pending Entries List).

## Decision
- `gps:raw_incoming` → group `gps-raw-workers` (`RawConsumer`)
- `gps:reports` → group `gps-workers` (`Consumer`)
- Retry streams → dedicated groups
- Recovery via **XAUTOCLAIM** (idle ≥ 60–90s) on raw, reports, and retry consumers
- Reports path also drops entries exceeding `MAX_RETRY_ATTEMPTS` delivery count (legacy); raw/retry prefer DLQ over silent drop

## Alternatives Considered
- Manual XPENDING+XCLAIM only — more code, same outcome
- At-most-once (ACK before process) — rejected (loss)

## Consequences
**Positive:** Crash-safe at-least-once processing.  
**Negative:** Possible duplicate dispatch → mitigated by idempotency (ADR 0008).

## References
- `internal/queue/raw_consumer.go`, `consumer.go`, `retry_consumer.go`
