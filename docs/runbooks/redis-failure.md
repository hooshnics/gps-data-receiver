# Runbook: Redis failure

## Symptoms
- Rising `gps_teltonika_ingest_errors_total` (ACK withheld)
- Devices reconnect / SD buffer growth
- Alerts: `RedisUnavailable`, `RedisMemoryHigh`, `TeltonikaIngestErrorsSpike`
- App logs: raw XADD failed

## Diagnosis
```bash
docker ps --filter name=gps-redis
docker exec gps-redis redis-cli PING
docker exec gps-redis redis-cli INFO memory | findstr used_memory
docker exec gps-redis redis-cli INFO persistence
docker exec gps-redis-sentinel-1 redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
curl -s http://127.0.0.1:8080/metrics | findstr gps_teltonika_ingest
```

## Commands
```bash
# Primary health
docker logs gps-redis --tail 100
docker logs gps-redis-replica --tail 100
docker logs gps-redis-sentinel-1 --tail 50

# Memory / eviction policy (must remain noeviction)
docker exec gps-redis redis-cli CONFIG GET maxmemory-policy
docker exec gps-redis redis-cli CONFIG GET maxmemory
```

## Recovery steps
1. If process down: `docker compose up -d redis redis-replica redis-sentinel-1 redis-sentinel-2 redis-sentinel-3`
2. If primary dead and Sentinel healthy: wait for promote; confirm new master via `SENTINEL get-master-addr-by-name`
3. If memory > 80%: reduce consumer lag (scale workers), export/archive if needed — **do not** switch to `allkeys-lru`
4. Confirm app uses Sentinel (`REDIS_SENTINEL_ADDRS`) so it discovers new primary
5. Do **not** force ACK or disable Redis write on hot path

## Validation
- `PING` PONG on current primary
- Ingest errors rate → 0
- `gps_teltonika_packets_acked_total` increasing
- Devices resume without fleet-wide SD overflow

## Notes
Brief XADD failures during failover are **expected** and preserve Zero-Drop (no ACK without durable write).
