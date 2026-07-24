# 0006 — Dead-letter queue

## Status
Accepted

## Context
Some frames pass CRC but fail full Codec decode (corrupt IO blocks, unsupported edge cases). Retrying forever blocks consumer progress.

## Decision
Route poison payloads to `gps:dead_letter` with metadata (`imei`, `frame_hex`, `error`, `received_at`, `source`) and **ACK** the source stream entry. Also used when PiStat/Hooshnics retry attempts exceed the max.

## Alternatives Considered
- Leave in PEL forever — rejected (starvation)
- Drop silently — rejected (ops blindness)
- Disk-only spill — rejected for primary path (Redis preferred)

## Consequences
**Positive:** Workers stay healthy; ops can inspect DLQ.  
**Negative:** DLQ needs monitoring (`gps_dead_letter_depth`, `gps_teltonika_dead_letter_total`).

## References
- `internal/queue/durable.go` `EnqueueDeadLetter`
- `internal/ingest/processor.go`
