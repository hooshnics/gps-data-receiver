# Raw data archival

## Why Redis is not the archive

Redis Streams (`gps:raw_incoming`) provide **durable ingest** for Zero-Drop ACK.
After the raw consumer succeeds, entries are removed (`XACK` + `XDel`).
Keeping all frames forever in Redis conflicts with `maxmemory-policy noeviction` (OOM → withhold ACK → fleet backlog).

## Strategy summary

See ADR 0020 and **ADR 0023** (implemented archive writer). Short version:

1. **Hot:** Redis (operational)
2. **Warm:** automated daily gzip NDJSON via `ARCHIVE_ENABLED` (`internal/archive`) — see [archive-strategy.md](./archive-strategy.md)
3. **Cold:** Object storage (S3/MinIO/compatible) — primary long-term store
4. **Postgres:** parsed telemetry + per-record fragments — product DB, not full-frame archive

Manual export remains available:

```bash
go run ./cmd/replay -source redis -mode export -file /tmp/raw.ndjson
```

## Suggested ops schedule

```bash
# Example daily warm export of yesterday’s UTC window (adjust host/env)
FROM=$(date -u -d 'yesterday' +%Y-%m-%dT00:00:00Z)
TO=$(date -u -d 'yesterday' +%Y-%m-%dT23:59:59Z)
OUT=/var/lib/gps-archive/raw-${FROM:0:10}.ndjson

go run ./cmd/replay -source redis -from "$FROM" -to "$TO" -mode export -file "$OUT"
gzip -9 "$OUT"
# aws s3 cp "$OUT.gz" s3://hooshnics-gps-raw/$(hostname)/
```

> Note: manual export only sees frames **still present** in the stream (pending / not yet deleted).
> Prefer automated archive (`ARCHIVE_ENABLED`, ADR 0023) for complete warm coverage before `XDel`.

## Recovery

1. Fetch `.ndjson.gz` from object storage for the incident window.
2. Follow [replay.md](./replay.md) dry-run → enqueue/reprocess.

## Volume / cost sketch

| Fleet | Interval | Packets/day | ~Frame size | Raw/day |
|-------|----------|-------------|{-------------|---------|
| 1k devices | 5s | ~17.3M | ~200 B | ~3.5 GB |
| 5k devices | 5s | ~86M | ~200 B | ~17 GB |

Gzip typically 2–4× on hex NDJSON; object storage lifecycle to IA/Glacier after 90 days.

## Compliance
- Treat archives as location + identifier data.
- Encrypt at rest; least-privilege IAM; retention + legal hold documented with product/legal.
