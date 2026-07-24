# Runbook: Data recovery

## Symptoms
- Parser regression after deploy
- DLQ growth / decode failures
- Need to reprocess historical frames

## Diagnosis
```bash
docker exec gps-redis redis-cli XLEN gps:dead_letter
curl -s http://127.0.0.1:8080/metrics | findstr "dead_letter decode"
go run ./cmd/replay -source dlq -limit 20 -mode dry-run -v
```

## Commands
```bash
# Dry-run a time window from live stream (read-only)
go run ./cmd/replay -source redis -from 2026-07-23T00:00:00Z -to 2026-07-23T12:00:00Z -v

# Export warm archive
go run ./cmd/replay -source redis -from ... -to ... -mode export -file /tmp/raw.ndjson

# Safe reprocess (no external HTTP / PG by default)
go run ./cmd/replay -source file -file /tmp/raw.ndjson -mode reprocess -force

# Test env copy (never writes gps:raw_incoming)
go run ./cmd/replay -source file -file /tmp/raw.ndjson -mode enqueue -target-stream gps:raw_replay
```

## Recovery steps
1. Identify window + IMEI filters
2. Dry-run until `parse_fail=0` on fixed binary
3. Prefer staging (`enqueue` → test consumers) before production reprocess with `-no-external=false`
4. Use `-force` when telemetry dedupe would skip corrected parses
5. Consult [../recovery/replay.md](../recovery/replay.md) and archival docs for cold storage

## Validation
- Dry-run OK counts match expected
- Downstream maps/reports show recovered points
- DLQ growth stops

## Notes
Postgres `raw_data` is per-record fragments — not sufficient for full AVL replay. Keep warm/cold archives (ADR 0020).
