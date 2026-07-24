# 0003 — Redis Streams

## Status
Accepted

## Context
Need a durable, multi-consumer buffer between TCP ingest and parse/dispatch, with crash recovery and horizontal worker scaling.

## Decision
Use Redis Streams as the backbone:

| Stream | Role |
|--------|------|
| `gps:raw_incoming` | Raw Teltonika AVL (pre-ACK durable write) |
| `gps:reports` | HTTP / legacy parsed path |
| `gps:dead_letter` | Poison / exhausted retries |
| `gps:pistat_retry` | PiStat delivery retries |
| `gps:hooshnics_retry` | Hooshnics delivery retries |

## Alternatives Considered
- Kafka — overkill for current fleet size
- NATS JetStream — not in stack
- PostgreSQL LISTEN/NOTIFY — poor for binary fan-out latency

## Consequences
**Positive:** Persistence + consumer groups + pending reclaim.  
**Negative:** Redis is a critical dependency for ACK.  
**Retention:** Processed entries are `XACK`+`XDel`; retry/DLQ use approximate `MAXLEN`.

## References
- `internal/queue/redis_queue.go`, `durable.go`
- ADR 0005, 0014
