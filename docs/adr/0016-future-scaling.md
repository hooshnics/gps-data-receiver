# 0016 — Future scaling strategy

## Status
Accepted

## Context
Fleet may grow to thousands of concurrent Teltonika sessions.

## Decision (near-term)
- Scale **workers** horizontally on shared Redis consumer groups
- Keep TCP sessions sticky per process (OS accepts fan-out behind LB with `TELTONIKA_TCP` VIP)
- Keep ACK path dependent only on local Redis RTT
- Prefer more Redis memory / replicas before introducing Kafka

## Future options (not implemented)
- Redis Cluster (see scaling-strategy.md — only if single primary saturates)
- Separate ingest and dispatch deployments
- TimescaleDB for long-term telemetry
- Per-IMEI partitioning for strict global order across workers
- Kafka as a multi-consumer enterprise bus (not on ACK path)

## Update
Redis **Sentinel HA** is implemented (ADR 0017). Prefer vertical Redis + more workers + multi-ingest LB before Cluster/Kafka.

## Consequences
**Positive:** Current design scales with Redis + worker count.  
**Negative:** Single-region Redis remains SPOF until HA is added.

## References
- ADR 0001, 0003, 0005
