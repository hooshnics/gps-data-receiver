# Deployment checklist (Zero-Drop safe)

Use before every production app deploy. Architecture unchanged: **ACK only after Redis XADD**.

## Pre-deploy
- [ ] Redis primary + Sentinel healthy (`PING`, `SENTINEL get-master-addr-by-name mymaster`)
- [ ] Postgres healthy (`pg_isready`) if storage enabled
- [ ] Secret checklist complete ([secret-management-checklist.md](./secret-management-checklist.md))
- [ ] `ENVIRONMENT=production` without `GPS_LAB_MODE`
- [ ] `TELTONIKA_IMEI_WHITELIST` set; Redis + Postgres non-default secrets
- [ ] Alertmanager receivers point at real ops channel ([alertmanager.md](./alertmanager.md))
- [ ] `gps_raw_pending_count` not critically elevated (or plan drain time)
- [ ] Previous image tagged `gps-receiver:previous`
- [ ] Change notes reviewed (parser/codec changes → plan replay)
- [ ] Alerts not already firing critically (or acknowledged)
- [ ] Archive volume mounted if `ARCHIVE_ENABLED=true`
- [ ] Archive volume writable by uid 1000 (entrypoint chown) — see [archive-strategy.md](../recovery/archive-strategy.md)
- [ ] After deploy: `docker exec gps-receiver ls -la /var/lib/gps-archive` shows ownership `appuser` / `1000`

## Deploy
- [ ] `docker compose build app` (or pull known digest)
- [ ] `docker compose up -d --no-deps app` (uses `stop_grace_period: 90s`)
- [ ] Confirm ordered shutdown logs: TCP stop → consumers → HTTP
- [ ] Wait for healthcheck: `wget` `/health` healthy
- [ ] Confirm `/ready` OK

## Post-deploy validation (15–30 min)
- [ ] `up{job="gps-receiver"} == 1`
- [ ] `gps_teltonika_packets_acked_total` increasing
- [ ] `increase(gps_teltonika_ingest_errors_total[5m])` near baseline
- [ ] `gps_raw_pending_count` not monotonically rising
- [ ] ACK latency: `gps_teltonika_redis_write_seconds` p99 acceptable
- [ ] Optional smoke: `go run ./cmd/teltonika-simulator -devices 20 -duration 1m`
- [ ] Optional: `go run ./cmd/ack-latency-bench -mode tcp -sessions 10 -packets 20`

## Rollback trigger
Roll back if any of:
- Health/ready fails > 3 minutes
- Sustained ingest error spike
- Decode/CRC failure spike after parser change
- Pending backlog climbing without recovery

→ [rollback.md](./rollback.md)

## Never during deploy
- [ ] Do not `kill -9` unless process wedged past grace period
- [ ] Do not ACK before Redis
- [ ] Do not set Redis `maxmemory-policy` to LRU
- [ ] Do not `XTRIM` raw stream while consumers are catching up
