# Redis Sentinel failover drill

## Goal

Prove Zero-Drop behavior during primary Redis loss:

- No false ACK (ACK only after successful `XADD`)
- No silent packet loss (devices retransmit on missing ACK)
- Consumers reconnect via Sentinel to new master
- Pending entries recoverable via consumer groups / `XAUTOCLAIM`

## Architecture under test

```
TCP → CRC → Redis XADD (primary via Sentinel) → ACK → async consumers
```

## Prerequisites

```bash
docker compose up -d redis redis-replica redis-sentinel-1 redis-sentinel-2 redis-sentinel-3 postgres app
# Confirm master:
docker exec gps-redis-sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
```

Moderate load (do not start at 10k devices for first drill):

```bash
go run ./cmd/teltonika-simulator -host 127.0.0.1 -port 5055 \
  -devices 200 -interval 2s -duration 5m -ramp 15s -ack-timeout 45s
```

Watch:

```bash
curl -s localhost:8080/metrics | grep -E 'gps_teltonika_ingest_errors|gps_teltonika_packets_acked|gps_raw_pending'
docker exec gps-redis-sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
```

## Drill steps

1. Note `T0` and current master address.
2. Induce primary failure: `docker restart gps-redis` (or `docker kill gps-redis`).
3. Record time until Sentinel reports new master (`T1`).
4. Observe simulator: ACK timeouts / reconnects are **expected** during the gap.
5. Confirm `gps_teltonika_ingest_errors_total` may rise briefly; ACKed count should resume climbing after failover.
6. Confirm `gps_raw_pending_count` drains (XAUTOCLAIM / consumers).
7. After primary returns as replica, optionally fail over again or leave as-is.

## Expected results

| Check | Expected |
|-------|----------|
| False ACK | None — failed `XADD` must not ACK |
| Lost durable data | None for ACKed packets |
| Device behavior | Retransmit until ACK |
| Consumer | Reconnect; process backlog |
| Failover time | Typically seconds (`down-after` 5s + election) |

## Results log

| Field | Value |
|-------|-------|
| Date | 2026-07-23 |
| Environment | Local Docker Compose (lab) |
| Load | See [load-test-report.md](../performance/load-test-report.md) / drill section |
| Master before | `redis:6379` |
| Failover trigger | `docker restart gps-redis` |
| Time to new master (approx) | _filled after drill execution_ |
| Ingest error Δ during window | _filled after drill_ |
| Recovery (ACK resume) | _filled after drill_ |
| Packet loss after recovery | None expected for ACKed; in-flight retransmit |
| Pass / Fail | _filled after drill_ |

## Rollback / cleanup

- Ensure three Sentinels healthy.
- Ensure one clear master: `SENTINEL get-master-addr-by-name mymaster`.
- Do not `XTRIM` raw stream during catch-up.

## References

- ADR 0017 (Redis HA)
- ADR 0004 (ACK after durable write)
- `docker/redis/sentinel.conf`
