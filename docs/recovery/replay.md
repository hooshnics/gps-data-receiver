# Telemetry replay & recovery

Offline tool for validating and reprocessing durable Teltonika raw frames **without affecting live ingestion**.

## Safety invariants

- Does **not** use the live consumer group (`gps-raw-workers`)
- Does **not** `XACK` / `XDel` live entries
- Does **not** `XADD` into `gps:raw_incoming` (refuses enqueue to the live raw stream)
- Device timestamps remain inside the AVL payload; `received_at` metadata is preserved on export/enqueue
- Default reprocess flags skip PiStat, Hooshnics, DLQ, and Postgres

Live path remains: TCP → CRC → Redis `XADD` → ACK.

## Build / run

```bash
go run ./cmd/replay -h
```

## Inputs

| Flag | Description |
|------|-------------|
| `-source redis` | Read `gps:raw_incoming` via `XRANGE` |
| `-source dlq` | Read `gps:dead_letter` |
| `-source file` | Read NDJSON archive (`-file`) |
| `-id` | Exact Redis stream ID |
| `-imei` | IMEI filter |
| `-from` / `-to` | RFC3339 time range (uses `received_at` / stream ID time) |
| `-limit` | Cap number of entries |

## Modes

### Dry-run (default)
```bash
go run ./cmd/replay -source redis -imei 356890080000001 -from 2026-07-23T00:00:00Z -to 2026-07-23T23:59:59Z -v
```
Parses Codec8/8E; prints OK/FAIL; no writes.

### Export (warm archive)
```bash
go run ./cmd/replay -source redis -from 2026-07-23T00:00:00Z -mode export -file /var/archive/raw-2026-07-23.ndjson
# optional: gzip /var/archive/raw-2026-07-23.ndjson
```

### Enqueue to test stream
```bash
go run ./cmd/replay -source file -file raw.ndjson -mode enqueue -target-stream gps:raw_replay
```
Point a **non-production** consumer at `gps:raw_replay` for end-to-end tests.

### Reprocess (parser pipeline)
```bash
# Safe defaults: parse + local processor options only (no external HTTP / PG / DLQ)
go run ./cmd/replay -source file -file raw.ndjson -mode reprocess -force

# Explicit recovery against live destinations (dangerous — use with care)
go run ./cmd/replay -source file -file raw.ndjson -mode reprocess -force \
  -no-external=false -no-postgres=false
```

`-force` bypasses Redis telemetry dedupe so a fixed parser can emit points previously skipped as duplicates.

## Recovery playbooks

### Parser bug shipped
1. Export affected window (or pull from cold archive).
2. Dry-run with new binary until `parse_fail=0`.
3. Reprocess with `-force` into a staging destination first (`-no-external` + inspect), then production if required.

### Destination outage already handled
PiStat/Hooshnics retries + DLQ cover delivery; replay is for **decode/pipeline** recovery, not routine destination blips.

### Redis failover gap
Un-ACKed frames remain on devices; after Sentinel promote, devices retransmit. Use replay only for frames already durable in Redis/archive.

## Related
- ADR 0018, 0020
- [archival.md](./archival.md)
- `docs/performance/load-test-report.md`
