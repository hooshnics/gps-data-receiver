# 0020 — Raw data retention strategy

## Status
Accepted

## Context
`gps:raw_incoming` is **operational** storage: written before TCP ACK, consumed asynchronously, then removed (`XACK`+`XDel`).
Default `REDIS_RAW_MAX_LEN=0` avoids trimming unread entries, but Redis is not a compliance archive.
PostgreSQL `gps_records.raw_data` retains **per-record** fragments after successful delivery — useful for audits of delivered points, not full Codec8/8E frame replay.

## Decision
Adopt a **hybrid** retention strategy:

| Tier | Store | Retention | Role |
|------|-------|-----------|------|
| Hot | Redis Streams | Until consumed (+ PEL) | Zero-Drop ingest |
| Warm | Compressed NDJSON (Option A) | 30–90 days on disk / NFS | Fast ops export via `cmd/replay -mode export` |
| Cold | Object storage (Option B) | 1+ years (policy-driven) | Long-term recovery / compliance |
| Analytical | PostgreSQL parsed + optional Timescale | Product retention | Maps, reports — not primary AVL archive |

**Chosen primary long-term store: Option B (object storage)** for expected volume and cost.
**Option A (compressed files)** is the bootstrap path and staging format before upload.
**Option C (PostgreSQL full-frame archive)** rejected as primary (TOAST growth, backup cost).

Archive capture must **not** block TCP ACK (async / offline export only).

## Alternatives
- Keep Redis forever — rejected (cost, `noeviction` OOM risk)
- Full frames only in Postgres — rejected as primary archive
- Kafka as retention bus — deferred (see scaling strategy); not on ACK path

## Consequences
**Positive:** Recoverability beyond Redis lifetime; cost-aligned cold storage.  
**Negative:** Ops must schedule warm export before consumer `XDel`; gaps if export lags.

## Operational Impact
- Docs: `docs/recovery/archival.md`, `docs/recovery/replay.md`
- Alert on DLQ / stream growth when archival backlog correlates with lag
- Encrypt object buckets; treat IMEI+location as sensitive (ADR 0015)

## References
- ADR 0014, 0018, 0019
- `cmd/replay -mode export`
