# 0004 — ACK only after durable Redis XADD

## Status
Accepted

## Context
Teltonika protocol semantics: positive 4-byte ACK means the server accepted the records; the device may delete them from its buffer.

## Decision
Write the 4-byte big-endian ACK **only after** successful `EnqueueRawAVL` (`XADD` to `gps:raw_incoming`). CRC failures still get `ACK=0`. Redis errors get **no ACK** and session close.

## Alternatives Considered
- ACK before Redis — rejected (zero-drop violation)
- Buffer to memory then ACK — rejected

## Consequences
**Positive:** Device SD card is the fallback store when Redis is down.  
**Negative:** Slow Redis increases device timeouts (monitor `gps_teltonika_redis_write_seconds`).

## References
- `internal/teltonika/tcp/session.go`
- ADR 0002, 0007
