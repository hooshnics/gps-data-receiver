# 0008 — Idempotency

## Status
Accepted

## Context
At-least-once delivery + device retransmission after lost ACK can produce duplicates.

## Decision
Two layers:
1. **Frame hash** (`SHA-256` of raw AVL) via Redis `SET NX` — skips exact resent packets before re-dispatch.
2. **Telemetry key** `IMEI|date_time|lat|lon|status` via Redis `SET NX` after decode — skips logical duplicates.
3. **PostgreSQL** `dedupe_key` column + `UNIQUE` index + `ON CONFLICT DO NOTHING` on insert.

TTL for Redis keys: `REDIS_DEDUPE_TTL` (default 24h).

## Alternatives Considered
- Frame hash only — insufficient if payload encoding differs for same point
- DB-only uniqueness — slower hot path; kept as final safety net

## Consequences
**Positive:** No duplicate map points / DB rows from retransmission.  
**Negative:** True identical samples within TTL (same second + coords + status) are collapsed — acceptable for Teltonika sample rates.

## References
- `internal/queue/durable.go` `ClaimFirstSighting`, `ClaimTelemetrySighting`
- `internal/storage/postgres.go`
- `internal/ingest/processor.go`
