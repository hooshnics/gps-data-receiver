# Alert runbook index

Prometheus rules: `monitoring/prometheus/alerts.yml`  
Loaded via `prometheus.yml` → `rule_files`.

UI: http://localhost:9090/alerts

## Severity
| Label | Meaning |
|-------|---------|
| `critical` | Ingest durability, availability, or growing backlog — page |
| `warning` | Investigate within business hours |

## Alert → runbook map

| Alert | Primary runbook |
|-------|-----------------|
| `GPSReceiverDown` | [deployment.md](./deployment.md) / restart app |
| `RedisUnavailable` / `RedisMemoryHigh` / `TeltonikaIngestErrorsSpike` | [redis-failure.md](./redis-failure.md) |
| `RedisRawStreamGrowth` / `RawConsumerLag` / `PendingMessagesCritical` | [worker-crash.md](./worker-crash.md) |
| `DeadLetterQueueGrowth` / `ParserFailureSpike` | [data-recovery.md](./data-recovery.md) |
| `RetryQueueGrowth` | below |
| `ACKLatencyDegraded` | below |
| `TCPConnectionSpike` | below |
| `DatabaseConnectionExhaustion` / `PostgresDown` | [postgres-failure.md](./postgres-failure.md) |

---

### GPSReceiverDown
**Diagnosis:** `curl -sf http://127.0.0.1:8080/health`; `docker ps`; `docker logs gps-receiver --tail 200`  
**Recovery:** Restart only after Redis healthy. Devices buffer un-ACKed frames (Zero-Drop).  
**Validation:** `/health` 200; `up{job="gps-receiver"}==1`; ACK rate resumes.

### RedisRawStreamGrowth
**Diagnosis:** `docker exec gps-redis redis-cli XLEN gps:raw_incoming`; compare `gps_raw_pending_count`.  
**Recovery:** Scale workers / fix destinations; do **not** trim raw stream while pending > 0.  
**Validation:** Depth declining over 15m.

### RetryQueueGrowth
**Diagnosis:** Check PiStat/Hooshnics HTTP status; `XLEN gps:pistat_retry` / `gps:hooshnics_retry`.  
**Recovery:** Restore destinations; rate limits already isolate ACK path.  
**Validation:** Retry depths falling; sender success rate up.

### ACKLatencyDegraded
**Diagnosis:** `go run ./cmd/ack-latency-bench -mode local`; Redis CPU/AOF; pool exhaustion.  
**Recovery:** Profile first — never ACK before XADD. Increase Redis IOPS/CPU or reduce concurrent sessions temporarily.  
**Validation:** `gps_teltonika_redis_write_seconds` p99 < 0.5s.

### TCPConnectionSpike
**Diagnosis:** `gps_teltonika_active_connections`; reconnect storm after outage.  
**Recovery:** Ensure Redis/app stable so ACKs resume; check LB idle timeouts; raise `ulimit` if needed.  
**Validation:** Active connections return to fleet baseline.

## Silencing
During planned Sentinel failover drills, silence `TeltonikaIngestErrorsSpike` / `ACKLatencyDegraded` briefly; keep `RedisMemoryHigh` armed.
