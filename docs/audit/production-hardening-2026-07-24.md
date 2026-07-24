# Production hardening report — 2026-07-24

## Architecture status

Unchanged Zero-Drop path:

`TCP → CRC → Redis XADD → ACK → Raw consumer → Parser → PG / PiStat / Hooshnics / Retry / DLQ`

## Files changed

| Area | Files | Reason |
|------|-------|--------|
| Archive perms | `Dockerfile`, `docker/entrypoint.sh`, `internal/archive/writer.go` | Volume owned by root → chown uid 1000 then drop to `appuser`; `EnsureWritable` fail-fast |
| TCP stability | `internal/teltonika/tcp/session.go`, `server.go`, `listen_*.go`, `session_test.go` | Keep session on XADD failure; retry XADD 3×; longer timeouts; less reconnect storm |
| Redis client | `internal/queue/redis_queue.go` | Read/Write/Pool timeouts 5s/10s under load |
| Forwarder/retry | `internal/queue/retry_consumer.go`, `durable.go`, `internal/hooshnics/forwarder.go`, `internal/metrics/metrics.go` | Exp backoff before requeue; soft-cap → DLQ; circuit + retry metrics |
| Compose/docs | `docker-compose.yml`, `docker-compose.prod.yml`, `docs/production-readiness.md`, archive/runbook docs | Production config + checklist |

## Benchmark before / after

Test: **200 devices**, interval **2s**, duration **120s**, ramp **20s** (target ~100 pkt/s steady).

| Metric | Before | After |
|--------|--------|-------|
| Throughput (avg over run) | 82 ACK/s | **85.7 ACK/s** (ramp dilutes; mid-run ~90–92) |
| Failed | 1737 | **0** |
| Reconnects | 1737 | **0** |
| ACK p50 | 2.5 ms | **2.2 ms** |
| ACK p95 | 85 ms | **23.1 ms** |
| ACK p99 | 603 ms | **414 ms** |
| `gps:raw_incoming` pending | 0 | **0** |
| Stream lag | 0 | **0** |

Primary win: **connection stability** (reconnect storm eliminated). Throughput average still slightly under 100 pkt/s because of ramp-up; late-window progress lines approached ~92 ACK/s.

## Verification

- `go test` (focused packages): PASS
- Docker image rebuild + app restart: healthy; process as `appuser`; archive writer ready
- Load retest: 12000/12000 ACKed

## Remaining risks

1. ACK p99 still hundreds of ms under Redis AOF/`everysec` + Windows Docker — monitor on bare-metal Linux.
2. Alertmanager webhook still placeholder until ops wiring.
3. Production must set IMEI whitelist + Redis/Postgres secrets (`ENVIRONMENT=production`).
4. External PiStat/Hooshnics outages inflate retry/DLQ — expected; does not block ACK.
5. Scenario B/C (5k/10k) not re-run in this pass.

## Next ops step

```bash
cd "GPS Simulator"
$env:PYTHONIOENCODING='utf-8'
python teltonika_tcp_tester.py load --host 127.0.0.1 --devices 200 --interval 2 --duration 120 --ramp 20
```

Checklist: [`docs/production-readiness.md`](../production-readiness.md)
