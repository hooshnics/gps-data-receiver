# 0022 — Post-ACK processing safety (dedupe claim release)

## Status
Accepted

## Context
Audit found that claiming Redis frame-hash / telemetry dedupe keys **before** successful processing, then failing the handler, caused XAUTOCLAIM reclaim to treat the message as a duplicate and ACK it — a **silent post-ACK drop** of downstream work (device already had TCP ACK after XADD).

## Decision
1. On raw handler failure after `ClaimFirstSighting`, call `ReleaseFirstSighting` before leaving the message pending.
2. On PiStat park failure after telemetry claims, `ReleaseTelemetrySighting` for claimed keys.
3. Never ACK poison/decode paths unless DLQ `XADD` succeeds (else leave pending).

TCP ACK path unchanged: CRC → Redis XADD → ACK only.

## Alternatives
- Claim dedupe only after success — also valid; release-on-failure preserves existing “skip duplicate stream entries” behavior for retransmits while fixing reclaim.

## Consequences
**Positive:** Failed processing remains retryable via PEL.  
**Negative:** Brief window where crash between claim and release can still skip; mitigated by release on error path and telemetry/PG second layer for successful partial work.

## Operational Impact
- Monitor `gps_raw_pending_count` after deploy
- Replay dry-run still preferred for parser incidents

## References
- `internal/queue/raw_consumer.go`, `durable.go`
- `internal/ingest/processor.go`
