# 0011 — Docker deployment & Redis AOF

## Status
Accepted

## Context
`allkeys-lru` on Redis could **evict stream keys**, silently breaking zero-drop. Durability also requires AOF for crash recovery of XADD before TCP ACK.

## Decision
Redis container configuration:
- `appendonly yes`
- `appendfsync everysec`
- `maxmemory-policy noeviction`
- `maxmemory 512mb`
- persistent volume `redis-data`
- healthcheck + `restart: unless-stopped`

App depends_on healthy Redis/Postgres; exposes `8080` and `5055`.

## Alternatives Considered
- `appendfsync always` — safer, higher ACK latency
- `noeviction` without AOF — RAM durability only

## Consequences
**Positive:** Streams cannot be LRU-evicted; AOF survives process crash.  
**Negative:** Under memory pressure Redis returns OOM errors → TCP withholds ACK (correct). Ops must size `maxmemory` / monitor.

## References
- `docker-compose.yml` redis service
- ADR 0002, 0004
