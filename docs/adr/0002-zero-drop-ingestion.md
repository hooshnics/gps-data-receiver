# 0002 — Zero-drop ingestion

## Status
Accepted

## Context
Under load, early ACK + in-memory channels caused ACKed-but-lost frames (map path jumps). Teltonika devices only retain undelivered records if they do **not** receive a positive ACK.

## Decision
Zero-drop hot path for TCP:

```
readFrame → CountAVLRecords (CRC) → Redis XADD (gps:raw_incoming) → TCP ACK
```

If XADD fails: **withhold ACK** and close the session so the device retransmits from SD.

## Alternatives Considered
- ACK then async channel — rejected (loss on crash/full buffer)
- ACK then Redis — rejected (same loss window)
- Local disk WAL before ACK — deferred (Redis AOF sufficient for current scale)

## Consequences
**Positive:** Durability before device confirmation.  
**Negative:** ACK latency includes Redis RTT; Redis outages pause device delivery (by design).  
**Perf:** Hot path stays CPU-light (CRC/count only); full parse is async.

## References
- `internal/teltonika/tcp/session.go`
- ADR 0004, 0003, 0011
