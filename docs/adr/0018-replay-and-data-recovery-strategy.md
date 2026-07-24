# 0018 — Replay and data recovery strategy

## Status
Accepted

## Context
Zero-Drop guarantees durable *ingest*, not eternal retention of every raw frame in Redis.
After successful consume, raw stream entries are `XACK` + `XDel` (ADR 0014).
Parser bugs, schema changes, or destination outages may require **reprocessing historical frames** that still exist in Redis (pending/DLQ), an export archive, or a test stream — without disrupting live TCP → XADD → ACK.

## Decision
Provide an offline **replay tool** (`cmd/replay`) that:

| Capability | Behavior |
|------------|----------|
| Input | Redis stream ID, time range, IMEI; or NDJSON archive file; or DLQ |
| Read path | `XRANGE` only — **never** `XACK` / `XDel` / production consumer group |
| Dry-run | CRC + `ParsePacket`; report device timestamps; no side effects |
| Export | NDJSON preserving `imei`, `frame_hex`, `received_at`, `source`, stream `id` |
| Enqueue to test | `XADD` to `gps:raw_replay` (or other non-live stream) only |
| Reprocess | Call `Processor.ProcessRawAVLWithOptions` with safe defaults (`no-external`, `no-postgres`) |
| Force | `--force` skips telemetry dedupe when replaying after a parser fix |

Live ingest path is unchanged: ACK still requires Redis `XADD` of raw AVL.

## Alternatives
- Re-`XADD` into `gps:raw_incoming` — **rejected** (races live consumers, confuses metrics/PEL)
- Replay from Postgres `raw_data` alone — **insufficient** (stores per-record hex fragments, not full AVL frames)
- In-process admin HTTP API — deferred (CLI is safer for ops)

## Consequences
**Positive:** Parser regressions can be validated and recovered without touching TCP ACK.  
**Negative:** Frames already deleted from Redis and never archived cannot be reconstructed as full AVL; retention (ADR 0020) is required for long-horizon recovery.

## Operational Impact
- Runbook: `docs/runbooks/data-recovery.md`
- Docs: `docs/recovery/replay.md`
- After parser deploys, dry-run sample traffic before enabling `-force` reprocess to production destinations

## References
- `cmd/replay/`, `internal/replay/`, `internal/ingest/processor.go`
- ADR 0002, 0004, 0014, 0020
