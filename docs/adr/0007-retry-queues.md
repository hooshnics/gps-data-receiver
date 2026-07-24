# 0007 — Retry queues

## Status
Accepted

## Context
PiStat/Hooshnics outages must not block `gps:raw_incoming` consumption or TCP ingest.

## Decision
- After HTTP retries exhausted → `XADD` to `gps:pistat_retry` or `gps:hooshnics_retry`
- Dedicated `RetryConsumers` drain those streams
- Attempt counter on each entry; after max (~20) → DLQ
- XAUTOCLAIM recovers abandoned retry PEL entries
- Hooshnics forwarder parks on Redis retry instead of disk spill (disk spill remains last-resort fallback)

## Alternatives Considered
- Block raw consumer until PiStat recovers — rejected (backpressure into TCP ACK path)
- Infinite redelivery on raw stream — rejected (blocks parse of healthy devices)

## Consequences
**Positive:** Ingest continues during destination outages.  
**Negative:** Extra streams/ops; eventual delivery not real-time.

## References
- `internal/queue/retry_consumer.go`
- `internal/hooshnics/forwarder.go` `SetRetrySink`
- ADR 0013
