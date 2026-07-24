# Production readiness — Teltonika GPS Data Receiver

Architecture (unchanged):

```
Teltonika Device → TCP Codec8/8E → CRC → Redis XADD → ACK
                                              ↓
                                    Raw consumer → Parser → PG / PiStat / Hooshnics
                                              ↓
                                    Retry streams → DLQ
```

Zero-Drop rule: **never ACK before successful Redis `XADD`.**

---

## 1. Required environment variables

| Variable | Production requirement |
|----------|------------------------|
| `ENVIRONMENT` | `production` |
| `TELTONIKA_TCP_ENABLED` | `true` |
| `TELTONIKA_IMEI_WHITELIST` | Non-empty comma-separated IMEIs |
| `POSTGRES_PASSWORD` | Non-default secret (not `gps`/`password`/`postgres`) |
| `REDIS_PASSWORD` | Non-empty (see `docker/redis/PRODUCTION-AUTH.md`) |
| `DESTINATION_SERVERS` | PiStat (or lab sink) URLs |
| `REDIS_SENTINEL_ADDRS` | Three Sentinels (recommended) |
| `ARCHIVE_ENABLED` / `ARCHIVE_DIR` | `true` / `/var/lib/gps-archive` |

Lab escape flags (`GPS_LAB_MODE`, `TELTONIKA_ALLOW_ANY_IMEI`, …) must **not** be set on public production.

Template: `env.production.example`

---

## 2. Docker deployment steps

```bash
cd gps-data-receiver

# Fill secrets (never commit)
cp env.production.example .env.production
# edit .env.production

docker compose -f docker-compose.yml -f docker-compose.prod.yml \
  --env-file .env.production up -d --build

docker compose ps
docker logs gps-receiver --tail 100
```

Health:

```bash
curl -sf http://127.0.0.1:8080/health
curl -sf http://127.0.0.1:8080/ready
```

---

## 3. Volume permissions (archive)

Symptom: `mkdir /var/lib/gps-archive/2026: permission denied`

Fix (built-in):

- Image entrypoint (`docker/entrypoint.sh`) starts as root briefly, `chown -R 1000:1000` on `ARCHIVE_DIR`, then `su-exec appuser`.
- Process runs as **appuser (uid 1000)**.
- `archive.EnsureWritable` fails fast at startup if not writable.

Do **not** set `user: "1000:1000"` on the app service unless the volume is pre-chowned.

Manual repair:

```bash
docker compose run --rm --user root --entrypoint sh app -c \
  "chown -R 1000:1000 /var/lib/gps-archive && chmod 0750 /var/lib/gps-archive"
```

Details: `docs/recovery/archive-strategy.md`

---

## 4. Redis requirements

| Setting | Value |
|---------|-------|
| Persistence | AOF `appendfsync everysec` |
| Memory policy | `noeviction` |
| HA | Primary + replica + 3 Sentinels |
| Auth | `requirepass` + `masterauth` + Sentinel `auth-pass` in production |
| Network | Not publicly exposed |
| Raw stream | `REDIS_RAW_MAX_LEN=0` (no trim of unread) |
| Pool | `REDIS_POOL_SIZE` ≥ concurrent devices + workers (e.g. 300–500) |

Host sysctl (Linux) for connection bursts:

```text
net.core.somaxconn = 4096
net.ipv4.tcp_max_syn_backlog = 4096
```

---

## 5. Monitoring commands

```bash
# App metrics
curl -s http://127.0.0.1:8080/metrics | grep -E 'gps_teltonika_|gps_raw_|gps_hooshnics|gps_pistat|gps_dead_letter'

# Redis
docker exec gps-redis redis-cli INFO memory
docker exec gps-redis redis-cli XLEN gps:raw_incoming
docker exec gps-redis redis-cli XPENDING gps:raw_incoming gps-raw-workers

# Sentinel master
docker exec gps-redis-sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster

# Alertmanager
curl -s http://127.0.0.1:9093/-/healthy
```

Key alerts: Redis unavailable, ACK latency, pending growth, DLQ growth, TCP ingest stopped — see `docs/runbooks/alertmanager.md`.

---

## 6. Backup strategy

| Data | Strategy |
|------|----------|
| Raw AVL warm | `ARCHIVE_DIR` daily `.ndjson.gz` → object storage |
| Redis | AOF/RDB volume snapshots; Sentinel promotes replica |
| PostgreSQL | Logical dumps / volume snapshots |
| Secrets | Vault / orchestrator secrets — not in git |

Recovery: `docs/recovery/archive-strategy.md`, `docs/recovery/replay.md`, `cmd/replay` (dry-run default).

---

## 7. Rollback procedure

1. Tag previous image: `gps-receiver:previous`
2. `docker compose up -d --no-deps app` with previous digest
3. Confirm `/health`, ACK rate, no ingest error spike
4. Do **not** `XTRIM` raw stream during rollback catch-up
5. Full steps: `docs/runbooks/rollback.md`

---

## 8. Load / soak validation (pre-cutover)

```bash
cd "../GPS Simulator"
python teltonika_tcp_tester.py ack-bench --host 127.0.0.1 --sessions 50 --packets 100
python teltonika_tcp_tester.py load --host 127.0.0.1 --devices 200 --interval 2 --duration 120 --ramp 20 --ack-timeout 45
```

Targets (lab hardware dependent):

| Metric | Guidance |
|--------|----------|
| Throughput | ≥ 95 ACK/s for 200 devices @ 2s (~100 pkt/s) |
| ACK p50 | low ms |
| ACK p99 | << device ACK timeout |
| `gps_raw_pending_count` | recovers to ~0 |
| reconnects / failed | should stay low after session-keep fix |

---

## 9. Checklists

- [ ] [deployment-checklist.md](./runbooks/deployment-checklist.md)
- [ ] [secret-management-checklist.md](./runbooks/secret-management-checklist.md)
- [ ] Alertmanager real notification channel
- [ ] Firewall on `:5055` (device APN/VPN only)
- [ ] Redis/Postgres not public
- [ ] Archive volume writable (uid 1000)
- [ ] Failover drill recorded (`docs/runbooks/redis-failover-drill.md`)
