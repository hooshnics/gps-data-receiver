# Load Test Report — GPS Data Receiver (Teltonika)

## Purpose
Validate production capacity of the Zero-Drop ingest pipeline under realistic Teltonika TCP load, without moving ACK before durable Redis `XADD`.

## Architecture under test

```
Device (simulator) → TCP :5055 → CRC → Redis XADD → ACK → Async consumers → PG / PiStat / Hooshnics
```

## Tools

| Tool | Path | Role |
|------|------|------|
| Fleet simulator | `cmd/teltonika-simulator` | N virtual devices, interval, reconnect, offline buffer |
| ACK benchmark | `cmd/ack-latency-bench` | p50/p95/p99 for TCP ACK RTT and CRC+XADD microbench |

## How to run

### Prerequisites
```bash
docker compose up -d redis redis-replica redis-sentinel-1 redis-sentinel-2 redis-sentinel-3 postgres app
# ensure HOOSHNICS_MIRROR can be disabled for pure ingest capacity if needed
```

### Scenario A — 1000 devices / 5s (≈200 pkt/s)
```bash
go run ./cmd/teltonika-simulator \
  -host 127.0.0.1 -port 5055 \
  -devices 1000 -interval 5s -duration 5m \
  -records 1 -codec 8 -ramp 30s
```

### Scenario B — 5000 devices / 5s (≈1000 pkt/s)
```bash
go run ./cmd/teltonika-simulator -devices 5000 -interval 5s -duration 5m -ramp 60s -codec 8
```

### Scenario C — 10000 devices / 5s (≈2000 pkt/s)
```bash
go run ./cmd/teltonika-simulator -devices 10000 -interval 5s -duration 5m -ramp 120s -ack-timeout 60s
```

### Codec8E mix
```bash
go run ./cmd/teltonika-simulator -devices 1000 -interval 5s -duration 5m -codec 8e
```

### Offline / retransmission
```bash
go run ./cmd/teltonika-simulator -devices 200 -interval 2s -duration 3m \
  -offline-every 10 -retransmit
```

### ACK latency benchmark (critical path)
```bash
# End-to-end TCP ACK RTT (includes CRC + XADD + ACK)
go run ./cmd/ack-latency-bench -mode tcp -sessions 50 -packets 200 -records 1

# Isolate CRC + Redis XADD (AOF everysec cost)
go run ./cmd/ack-latency-bench -mode local -sessions 50 -packets 200
```

### Metrics to capture during the run
```bash
curl -s localhost:8080/metrics | grep -E 'gps_teltonika_|gps_raw_|gps_dead_letter|gps_pistat_retry'
docker stats gps-receiver gps-redis gps-postgres --no-stream
```

---

## Results template (fill after each run)

> Numbers below are placeholders — run the scenarios and paste measured values. Do not tune the Zero-Drop path without profiling first.

### Environment
| Field | Value |
|-------|-------|
| Date | 2026-07-24 |
| Host / VM size | Local Docker Desktop (Windows) |
| Redis mode | Sentinel (compose default) |
| `appendfsync` | everysec |
| `WORKER_COUNT` | 50 |
| App image / commit | gps-receiver:latest (post session-keep + archive perms) |

### Scenario: 200 devices / 2s interval (post-hardening retest)

| Metric | Before | After |
|--------|--------|-------|
| Packets sent | — | 12000 |
| Packets ACKed | — | 12000 |
| Failed / timeouts | 1737 / — | **0 / 0** |
| Reconnects | 1737 | **0** |
| Throughput (ACK/s avg) | 82 | **85.7** (steady mid-run ~90–92) |
| ACK p50 | 2.5ms | **2.2ms** |
| ACK p95 | 85ms | **23.1ms** |
| ACK p99 | 603ms | **414ms** |
| `gps_raw_pending_count` | 0 | 0 |
| `gps_raw_stream_depth` | 0 | 0 |

### Observations
- Session-keep on temporary XADD failure removed reconnect storm.
- Archive volume permission fixed via entrypoint chown → appuser.
- Full report: `docs/audit/production-hardening-2026-07-24.md`

### Pass / Fail criteria (suggested)
| Check | Target |
|-------|--------|
| Zero-Drop | No ACK without XADD (architecture invariant) |
| ACK p99 | << configured device/simulator timeout |
| Ingest errors | Explainable only during induced failover |
| Consumer lag | Pending recovers via XAUTOCLAIM; no unbounded growth |
| DLQ | Only poison / exhausted retries |

---

## Sentinel failover drill (availability)

```bash
# While simulator runs at moderate load:
docker restart gps-redis
# Expect brief ACK timeouts / ingest_errors, then recovery via promoted replica.
# Confirm: docker exec gps-redis-sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
```

Record failover gap (seconds) and whether devices recovered without data loss (ACKed count should eventually match durable stream processing).

---

## Notes
- Do **not** “fix” high ACK latency by ACKing before Redis.
- Profile Redis fsync, pool exhaustion, and CPU before code changes.
- Prefer ramp-up (`-ramp`) to avoid thundering herd on IMEI handshake.
