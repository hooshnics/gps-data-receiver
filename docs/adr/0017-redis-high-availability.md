# 0017 — Redis High Availability (Sentinel)

## Status
Accepted

## Context
Zero-Drop ACK depends on a successful Redis `XADD`. A single Redis container is a **Single Point of Failure**: process crash, host reboot, or disk issues pause ingest (by design we withhold ACK), but without a replica/Sentinel the outage lasts until manual recovery and risk of longer device SD backlog.

Redis Cluster was evaluated and rejected for this workload: Streams + consumer groups are simpler and more reliable on a classic primary/replica topology; Cluster adds hash-slot complexity without benefit at current fleet scale.

## Decision
Adopt **Redis Sentinel + one replica** (Option A):

| Service | Role |
|---------|------|
| `redis` | Primary (AOF `everysec`, `noeviction`) |
| `redis-replica` | Hot standby replica |
| `redis-sentinel-1/2/3` | Quorum 2 for automatic failover |

Application client:
- If `REDIS_SENTINEL_ADDRS` is set → `redis.NewFailoverClient` (discovers current primary)
- Else → standalone `REDIS_HOST:REDIS_PORT` (dev / emergency)

Master name: `mymaster` (`REDIS_SENTINEL_MASTER`).

### Failover behavior
1. Sentinels detect primary down (`down-after-milliseconds` 5s).
2. Quorum elects and promotes replica.
3. FailoverClient reconnects to new primary.
4. During the window: TCP `XADD` may fail → **ACK withheld** → devices retransmit (Zero-Drop preserved).
5. After failover: streams continue from AOF/replica data (replica must have caught up).

## Alternatives
- **Redis Cluster** — unnecessary operational complexity for Streams-centric ingest
- **Manual primary only** — rejected (no automatic recovery)
- **`appendfsync always` on single node** — improves crash durability but not availability

## Consequences
**Positive:** Automatic promotion; app survives primary loss without config change.  
**Negative:** More containers; failover still causes transient XADD errors (correct for Zero-Drop).  
**Limitations:** Brief ingest pause; async replication sub-second risk covered by un-ACKed device buffers; three Sentinels required for quorum.

## Operational Impact
- Alert on ingest errors + Redis memory (`monitoring/prometheus/alerts.yml`)
- Runbook: `docs/runbooks/redis-failure.md`
- Drill: restart primary under load; confirm Sentinel promote + device recovery
- Keep AOF `everysec` + `noeviction` on primary and replica

## References
- `docker-compose.yml`, `docker/redis/*.conf`
- `internal/queue/redis_queue.go`, `internal/config/config.go`
- ADR 0002, 0004, 0011
