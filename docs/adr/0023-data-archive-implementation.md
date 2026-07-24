# ADR 0023 — Data archive implementation

## Status

Accepted

## Context

ADR 0020 defined retention strategy (hot Redis / warm gzip / cold object storage).
Warm archives were previously **manual** (`cmd/replay -mode export`), which risks gaps when consumers `XACK`+`XDel` faster than ops export.

## Decision

Implement an optional **post-process archive writer** on the raw consumer path:

```
Redis Stream (already durable)
        │
Raw consumer (parse / store / dispatch)
        │
Archive Writer (local NDJSON.gz)   ← AFTER successful handler
        │
XACK / XDel
```

- **Not on the TCP ACK hot path** — Zero-Drop ACK remains Redis `XADD` only.
- Format: daily rotated gzip NDJSON under `ARCHIVE_DIR/YYYY/MM/DD/raw-YYYYMMDD.ndjson.gz`
- Config: `ARCHIVE_ENABLED`, `ARCHIVE_DIR`, `ARCHIVE_STRICT`
- Non-strict (default): archive errors increment `raw_storage_failures_total` and park a DLQ copy for recovery, then ACK; if DLQ park fails, leave pending
- Strict mode: archive failure releases frame claim and leaves message pending (no silent drop)
- Docker: entrypoint chowns `ARCHIVE_DIR` to uid 1000 then drops to `appuser`; `EnsureWritable` fails fast at startup

Per-device files (`device_<imei>.json.gz`) were considered; daily consolidated files were chosen to avoid thousands of open file handles under fleet load.

## Consequences

- Warm archive fills automatically without blocking ingest.
- Disk growth must be monitored (`HostDiskUsageHigh`); operators ship to object storage.
- Recovery: gunzip → `cmd/replay -source file`.
- External HTTP outages remain isolated (archive is local disk only).

## References

- `internal/archive/writer.go`
- `internal/queue/raw_consumer.go` (`StreamArchiver`)
- `docs/recovery/archive-strategy.md`
- ADR 0020, 0004
