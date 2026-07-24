# 0009 — PostgreSQL persistence

## Status
Accepted

## Context
Need local audit of delivered/failed/invalid GPS for ops and path APIs, without putting Postgres on the TCP ACK path.

## Decision
- Tables: `gps_records`, `gps_failed_records`, `gps_invalid_records`
- Writes via `AsyncWriter` (channel + batch insert) after dispatch outcomes
- Indexes on `imei`, `created_at`, expression `(parsed_data->>'date_time')`
- `dedupe_key` unique for idempotent success inserts
- Connection pool sized from `WORKER_COUNT`

## Alternatives Considered
- Sync insert before ACK — rejected (latency / availability coupling)
- TimescaleDB — optional future; not required now

## Consequences
**Positive:** Non-blocking audit trail.  
**Negative:** AsyncWriter can lag under extreme load (bounded channel with overflow goroutine).

## References
- `internal/storage/postgres.go`, `async_writer.go`
